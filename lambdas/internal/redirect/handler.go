// Package redirect resolves (domain, slug) to its target URL, records the
// click and answers with a 301. The domain is the host the request was sent to.
package redirect

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/domain"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

// Store is the data access the redirect handler needs.
type Store interface {
	GetLink(ctx context.Context, domain, slug string) (store.Link, bool, error)
	PutClick(ctx context.Context, click store.Click) error
}

// DomainConfig holds the redirect settings of the domains this stack serves.
type DomainConfig interface {
	// Fallback returns the target for "/" and for unknown slugs on domain;
	// known is false when the domain is not served by this stack.
	Fallback(domain string) (url string, known bool)
}

// Handler serves every non-API route of the HTTP API.
type Handler struct {
	Store   Store
	Domains DomainConfig
	// DefaultRedirectURL is the target for requests that carry no host, which
	// therefore have no domain to ask Domains about.
	DefaultRedirectURL string
	// AdminOrigin is the only origin allowed by the CORS headers on /api paths.
	AdminOrigin string
	Now         func() time.Time
}

// Handle answers one request. It never returns an error: failures become a
// 502 response.
func (h *Handler) Handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (resp events.APIGatewayV2HTTPResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Error processing request", "error", fmt.Sprint(r))
			resp, err = unavailable(), nil
		}
	}()

	path := req.RawPath
	if path == "" {
		path = "/"
	}
	host := requestDomain(req)
	slog.Info("Request received", "domain", host, "path", path)

	if strings.HasPrefix(path, "/api") {
		return h.apiFallback(req), nil
	}

	if host == "" {
		slog.Info("Request without host, using default", "target_url", h.DefaultRedirectURL)
		return movedPermanently(h.DefaultRedirectURL), nil
	}
	fallback, served := h.Domains.Fallback(host)
	if !served {
		slog.Info("Domain not served", "domain", host)
		return notFound(), nil
	}

	slug := strings.Trim(path, "/")
	target, hit, err := h.resolve(ctx, host, slug, fallback)
	if err != nil {
		slog.Error("Error processing request", "error", err.Error())
		return unavailable(), nil
	}
	if hit {
		h.track(ctx, host, slug, target)
	}
	return movedPermanently(target), nil
}

func movedPermanently(target string) events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{
		StatusCode: 301,
		Headers:    map[string]string{"Location": target},
	}
}

// apiFallback answers /api paths that no API route matched: CORS preflight
// gets a 200, everything else a JSON 404.
func (h *Handler) apiFallback(req events.APIGatewayV2HTTPRequest) events.APIGatewayV2HTTPResponse {
	cors := map[string]string{
		"Access-Control-Allow-Origin":  h.AdminOrigin,
		"Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
		"Access-Control-Allow-Headers": "Content-Type, Authorization",
		"Access-Control-Max-Age":       "3600",
	}
	if req.RequestContext.HTTP.Method == "OPTIONS" {
		return events.APIGatewayV2HTTPResponse{StatusCode: 200, Headers: cors}
	}
	headers := maps.Clone(cors)
	headers["Content-Type"] = "application/json"
	return events.APIGatewayV2HTTPResponse{
		StatusCode: 404,
		Headers:    headers,
		Body:       `{"error": "Not found"}`,
	}
}

// requestDomain returns the domain the request was sent to: the API Gateway
// domain name, or the Host header when that is empty, normalised by
// domain.FromHost. It is "" when neither is set.
func requestDomain(req events.APIGatewayV2HTTPRequest) string {
	if d := domain.FromHost(req.RequestContext.DomainName); d != "" {
		return d
	}
	for k, v := range req.Headers {
		if strings.EqualFold(k, "host") {
			return domain.FromHost(v)
		}
	}
	return ""
}

// resolve returns the target for slug on host; hit is false when fallback is
// used. An empty slug is never looked up.
func (h *Handler) resolve(ctx context.Context, host, slug, fallback string) (target string, hit bool, err error) {
	if slug == "" {
		return fallback, false, nil
	}
	link, ok, err := h.Store.GetLink(ctx, host, slug)
	if err != nil {
		return "", false, err
	}
	if ok {
		slog.Info("Redirect hit", "domain", host, "slug", slug, "target_url", link.TargetURL)
		return link.TargetURL, true, nil
	}
	slog.Info("Redirect miss, using fallback", "domain", host, "slug", slug, "target_url", fallback)
	return fallback, false, nil
}

// track records a click. A failure is logged and never fails the redirect.
func (h *Handler) track(ctx context.Context, host, slug, target string) {
	ts := store.Timestamp(h.Now())
	if err := h.Store.PutClick(ctx, store.NewClick(host, slug, ts, &target)); err != nil {
		slog.Error("Failed to track redirect", "error", err.Error())
		return
	}
	slog.Info("Tracked click", "domain", host, "slug", slug, "timestamp", ts)
}

func notFound() events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{
		StatusCode: 404,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       "Not found",
	}
}

func unavailable() events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{
		StatusCode: 502,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       "Service temporarily unavailable",
	}
}
