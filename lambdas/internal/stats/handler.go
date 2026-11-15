// Package stats serves click statistics under /api/stats, across all domains
// or for one domain.
package stats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/apigw"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/domain"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

// Store is the data access the stats handler needs.
type Store interface {
	ScanClicks(ctx context.Context, r store.TimeRange) ([]store.Click, error)
	QueryDomainClicks(ctx context.Context, domain string, r store.TimeRange) ([]store.Click, error)
	QueryLinkClicks(ctx context.Context, domain, slug string, r store.TimeRange) ([]store.Click, error)
	ListLinkKeys(ctx context.Context, domain string) ([]string, error)
}

// Handler serves the /api/stats routes.
type Handler struct {
	Store Store
	Now   func() time.Time
}

const (
	routeOverview = "GET /api/stats"
	routeDetail   = "GET /api/stats/{domain}/{path}"

	recentClicksLimit = 20
	// maxDays is the largest day count a time filter accepts before the
	// request fails as out of range.
	maxDays = 999_999_999
)

type routeFunc func(h *Handler, ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)

var routes = map[string]routeFunc{
	routeOverview: (*Handler).overview,
	routeDetail:   (*Handler).detail,
}

// Handle routes one request. Unknown routes get a 404 and any failure inside
// a route a 500.
func (h *Handler) Handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	slog.Info("Request", "route_key", req.RouteKey)

	route, ok := routes[req.RouteKey]
	if !ok {
		return apigw.Error(404, "Route not found"), nil
	}
	return h.run(ctx, req, route), nil
}

func (h *Handler) run(ctx context.Context, req events.APIGatewayV2HTTPRequest, route routeFunc) (resp events.APIGatewayV2HTTPResponse) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Error handling route", "route_key", req.RouteKey, "error", fmt.Sprint(r))
			resp = apigw.Error(500, "Internal server error")
		}
	}()
	resp, err := route(h, ctx, req)
	if err != nil {
		slog.Error("Error handling route", "route_key", req.RouteKey, "error", err.Error())
		return apigw.Error(500, "Internal server error")
	}
	return resp
}

type linkStats struct {
	Domain     string  `json:"domain"`
	Slug       string  `json:"slug"`
	Clicks     int     `json:"clicks"`
	TargetURL  *string `json:"target_url"`
	FirstClick string  `json:"first_click"`
	LastClick  string  `json:"last_click"`
}

type overviewResponse struct {
	Stats       []*linkStats `json:"stats"`
	TotalClicks int          `json:"total_clicks"`
}

const msgInvalidFilter = "Invalid time filter. Use days=N or from=YYYY-MM-DD&to=YYYY-MM-DD"

// overview aggregates the clicks in range per link, most clicked first.
// Links with equal counts keep the order in which they were first seen.
// With ?domain= it reads one domain through the by-domain index; without it
// it scans the clicks of every domain. With linked_only=true, clicks of
// links that no longer exist are left out.
func (h *Handler) overview(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	q := req.QueryStringParameters
	linkedOnly := strings.ToLower(q["linked_only"]) == "true"

	name, err := domain.ParseOptional(q["domain"])
	if err != nil {
		return apigw.Error(400, err.Error()), nil
	}
	r, err := h.timeRange(q)
	if errors.Is(err, errInvalidFilter) {
		return apigw.Error(400, msgInvalidFilter), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	var clicks []store.Click
	if name != "" {
		clicks, err = h.Store.QueryDomainClicks(ctx, name, r)
	} else {
		clicks, err = h.Store.ScanClicks(ctx, r)
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	var known map[string]bool
	if linkedOnly {
		keys, err := h.Store.ListLinkKeys(ctx, name)
		if err != nil {
			return events.APIGatewayV2HTTPResponse{}, err
		}
		known = make(map[string]bool, len(keys))
		for _, k := range keys {
			known[k] = true
		}
	}

	byKey := map[string]*linkStats{}
	result := []*linkStats{}
	for _, c := range clicks {
		if known != nil && !known[c.LinkKey] {
			continue
		}
		s, ok := byKey[c.LinkKey]
		if !ok {
			s = &linkStats{Domain: c.Domain, Slug: c.Slug, FirstClick: c.Timestamp, LastClick: c.Timestamp}
			byKey[c.LinkKey] = s
			result = append(result, s)
		}
		s.Clicks++
		s.TargetURL = c.TargetURL
		if c.Timestamp < s.FirstClick {
			s.FirstClick = c.Timestamp
		}
		if c.Timestamp > s.LastClick {
			s.LastClick = c.Timestamp
		}
	}

	slices.SortStableFunc(result, func(a, b *linkStats) int { return b.Clicks - a.Clicks })
	total := 0
	for _, s := range result {
		total += s.Clicks
	}
	return apigw.JSON(200, overviewResponse{Stats: result, TotalClicks: total}), nil
}

type recentClick struct {
	Timestamp string `json:"timestamp"`
}

type detailResponse struct {
	Domain       string        `json:"domain"`
	Slug         string        `json:"slug"`
	Clicks       int           `json:"clicks"`
	TargetURL    *string       `json:"target_url"`
	RecentClicks []recentClick `json:"recent_clicks"`
}

// detail returns the click count of one link, its 20 most recent clicks and
// the target URL recorded on the oldest click in range.
func (h *Handler) detail(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	name, err := domain.Parse(req.PathParameters["domain"])
	if err != nil {
		return apigw.Error(400, err.Error()), nil
	}
	slug := req.PathParameters["path"]

	r, err := h.timeRange(req.QueryStringParameters)
	if errors.Is(err, errInvalidFilter) {
		return apigw.Error(400, msgInvalidFilter), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	clicks, err := h.Store.QueryLinkClicks(ctx, name, slug, r)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	var target *string
	if len(clicks) > 0 {
		target = clicks[0].TargetURL
	}

	sorted := slices.Clone(clicks)
	slices.SortStableFunc(sorted, func(a, b store.Click) int { return strings.Compare(b.Timestamp, a.Timestamp) })
	recent := []recentClick{}
	for _, c := range sorted[:min(len(sorted), recentClicksLimit)] {
		recent = append(recent, recentClick{Timestamp: c.Timestamp})
	}

	return apigw.JSON(200, detailResponse{
		Domain:       name,
		Slug:         slug,
		Clicks:       len(clicks),
		TargetURL:    target,
		RecentClicks: recent,
	}), nil
}

var errInvalidFilter = errors.New("invalid time filter")

// timeRange turns the query parameters into a time range. from and to
// (YYYY-MM-DD, not validated) take precedence over days; days=N starts N
// days before now and leaves the end open. Without any of them the range is
// open on both ends.
func (h *Handler) timeRange(q map[string]string) (store.TimeRange, error) {
	from, to, days := q["from"], q["to"], q["days"]

	if from != "" || to != "" {
		var r store.TimeRange
		if from != "" {
			r.Start = from + "T00:00:00+00:00"
		}
		if to != "" {
			r.End = to + "T23:59:59+00:00"
		}
		return r, nil
	}

	if days != "" {
		n, err := parseDays(days)
		if err != nil {
			return store.TimeRange{}, err
		}
		if n > maxDays || n < -maxDays {
			return store.TimeRange{}, fmt.Errorf("days=%d is out of range", n)
		}
		start := h.Now().UTC().AddDate(0, 0, -int(n))
		if start.Year() < 1 || start.Year() > 9999 {
			return store.TimeRange{}, fmt.Errorf("days=%d is out of range", n)
		}
		return store.TimeRange{Start: store.Timestamp(start)}, nil
	}

	return store.TimeRange{}, nil
}

// parseDays parses a decimal integer: surrounding whitespace, a leading sign
// and single underscores between digits are allowed. Malformed input is
// errInvalidFilter; a well-formed number beyond int64 is a range error.
func parseDays(s string) (int64, error) {
	s = apigw.TrimSpace(s)
	digits := strings.TrimLeft(s, "+-")
	if len(s)-len(digits) > 1 || digits == "" {
		return 0, errInvalidFilter
	}
	prevUnderscore := true
	for _, r := range digits {
		switch {
		case r >= '0' && r <= '9':
			prevUnderscore = false
		case r == '_' && !prevUnderscore:
			prevUnderscore = true
		default:
			return 0, errInvalidFilter
		}
	}
	if prevUnderscore {
		return 0, errInvalidFilter
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("days=%s: %w", s, err)
	}
	return n, nil
}
