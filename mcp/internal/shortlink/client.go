// Package shortlink is an HTTP client for the short-link management API
// (/api/links and /api/stats). Every request carries a Cognito access token
// as a bearer token; the token comes from a TokenProvider.
//
// All knowledge of API paths and request/response shapes lives in this file.
package shortlink

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Slug rules mirror the validation in the links Lambda.
const (
	// SlugPattern is the regular expression a short path must match.
	SlugPattern = `^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`
	// MaxSlugLength is the maximum length of a short path.
	MaxSlugLength = 64
)

// ReservedSlugs cannot be used as short paths.
var ReservedSlugs = []string{"api", "admin", "health", "status"}

var slugRE = regexp.MustCompile(SlugPattern)

// ValidateSlug applies the same rules as the API and returns the API's
// wording on failure, so a bad slug is rejected before any request is sent.
func ValidateSlug(slug string) error {
	if slug == "" {
		return errors.New("path is required")
	}
	for _, r := range ReservedSlugs {
		if slug == r {
			return fmt.Errorf("path %q is reserved and cannot be used as a short link", slug)
		}
	}
	if len(slug) > MaxSlugLength {
		return fmt.Errorf("path must be at most %d characters", MaxSlugLength)
	}
	if strings.Contains(slug, "--") {
		return errors.New("path must not contain consecutive hyphens")
	}
	if !slugRE.MatchString(slug) {
		return errors.New("path must contain only lowercase letters, numbers, and hyphens, and must start and end with a letter or number")
	}
	return nil
}

// ValidateTargetURL applies the same rules as the API.
func ValidateTargetURL(u string) error {
	if u == "" {
		return errors.New("target URL is required")
	}
	if !strings.HasPrefix(u, "https://") {
		return errors.New("target URL must start with https://")
	}
	if len(u) < len("https://x.xx") {
		return errors.New("target URL must be a valid URL with a domain")
	}
	return nil
}

// TokenProvider returns a valid access token. Invalidate drops a cached token
// so the next Token call fetches a fresh one.
type TokenProvider interface {
	Token(ctx context.Context) (string, error)
	Invalidate()
}

// Link is a short link as stored by the API.
type Link struct {
	ShortPath string `json:"short_path" jsonschema:"the short path (slug), without leading slash"`
	TargetURL string `json:"target_url" jsonschema:"the https URL the short path redirects to"`
	CreatedAt string `json:"created_at" jsonschema:"creation time, ISO 8601 UTC"`
	UpdatedAt string `json:"updated_at" jsonschema:"last update time, ISO 8601 UTC"`
}

// LinkStatsSummary is one row of the stats overview.
type LinkStatsSummary struct {
	Path       string  `json:"path" jsonschema:"the redirect path including the leading slash, e.g. /docs"`
	Clicks     int     `json:"clicks" jsonschema:"total number of recorded clicks"`
	TargetURL  *string `json:"target_url" jsonschema:"target URL recorded with the most recent click seen"`
	FirstClick *string `json:"first_click" jsonschema:"timestamp of the first recorded click"`
	LastClick  *string `json:"last_click" jsonschema:"timestamp of the last recorded click"`
}

// StatsOverview is the response of GET /api/stats.
type StatsOverview struct {
	Stats       []LinkStatsSummary `json:"stats"`
	TotalClicks int                `json:"total_clicks"`
}

// Click is a single recorded redirect.
type Click struct {
	Timestamp string `json:"timestamp" jsonschema:"click time, ISO 8601 UTC"`
}

// LinkStats is the response of GET /api/stats/{path}.
type LinkStats struct {
	Path         string  `json:"path" jsonschema:"the redirect path including the leading slash"`
	Clicks       int     `json:"clicks" jsonschema:"number of clicks in the requested time range"`
	TargetURL    *string `json:"target_url" jsonschema:"target URL recorded with the clicks, null if there were none"`
	RecentClicks []Click `json:"recent_clicks" jsonschema:"up to 20 most recent clicks in the range, newest first"`
}

// StatsFilter narrows GET /api/stats/{path} to a time range. Days and
// From/To are alternatives; the API gives From/To precedence.
type StatsFilter struct {
	Days int    // last N days; 0 means unset
	From string // YYYY-MM-DD, inclusive
	To   string // YYYY-MM-DD, inclusive
}

// APIError is a non-2xx response from the API.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	switch e.Status {
	case http.StatusUnauthorized:
		return fmt.Sprintf("API rejected the credentials (HTTP 401): %s", e.Message)
	case http.StatusForbidden:
		return fmt.Sprintf("permission denied (HTTP 403): %s; write operations require a user in the Cognito group 'admin'", e.Message)
	default:
		return fmt.Sprintf("API error (HTTP %d): %s", e.Status, e.Message)
	}
}

// Client talks to the management API.
type Client struct {
	base   string
	tokens TokenProvider
	http   *http.Client
}

// NewClient returns a client for the API at baseURL, e.g.
// https://go.example.com. A nil httpClient uses a client with a 30s timeout.
func NewClient(baseURL string, tokens TokenProvider, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), tokens: tokens, http: httpClient}
}

// ListLinks returns all links.
func (c *Client) ListLinks(ctx context.Context) ([]Link, error) {
	var out struct {
		Links []Link `json:"links"`
		Count int    `json:"count"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/links", nil, nil, &out); err != nil {
		return nil, err
	}
	if out.Links == nil {
		out.Links = []Link{}
	}
	return out.Links, nil
}

// GetLink returns the link with the given slug. The API has no single-link
// endpoint, so this lists all links and picks the match.
func (c *Client) GetLink(ctx context.Context, slug string) (*Link, error) {
	links, err := c.ListLinks(ctx)
	if err != nil {
		return nil, err
	}
	for i := range links {
		if links[i].ShortPath == slug {
			return &links[i], nil
		}
	}
	return nil, &APIError{Status: http.StatusNotFound, Message: fmt.Sprintf("Path '%s' not found", slug)}
}

// CreateLink creates a new link. The API returns 409 if the slug exists.
func (c *Client) CreateLink(ctx context.Context, slug, targetURL string) (*Link, error) {
	body := map[string]string{"short_path": slug, "target_url": targetURL}
	var out Link
	if err := c.do(ctx, http.MethodPost, "/api/links", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateLink changes the target URL of an existing link.
func (c *Client) UpdateLink(ctx context.Context, slug, targetURL string) (*Link, error) {
	body := map[string]string{"target_url": targetURL}
	var out Link
	if err := c.do(ctx, http.MethodPut, "/api/links/"+url.PathEscape(slug), nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteLink deletes a link. Recorded clicks are kept by the API.
func (c *Client) DeleteLink(ctx context.Context, slug string) error {
	return c.do(ctx, http.MethodDelete, "/api/links/"+url.PathEscape(slug), nil, nil, nil)
}

// StatsOverview returns click totals per redirect path, sorted by clicks
// descending. With linkedOnly, paths without a current link are left out.
func (c *Client) StatsOverview(ctx context.Context, linkedOnly bool) (*StatsOverview, error) {
	q := url.Values{"linked_only": {strconv.FormatBool(linkedOnly)}}
	var out StatsOverview
	if err := c.do(ctx, http.MethodGet, "/api/stats", q, nil, &out); err != nil {
		return nil, err
	}
	if out.Stats == nil {
		out.Stats = []LinkStatsSummary{}
	}
	return &out, nil
}

// LinkStats returns click statistics for one slug.
func (c *Client) LinkStats(ctx context.Context, slug string, f StatsFilter) (*LinkStats, error) {
	q := url.Values{}
	if f.Days > 0 {
		q.Set("days", strconv.Itoa(f.Days))
	}
	if f.From != "" {
		q.Set("from", f.From)
	}
	if f.To != "" {
		q.Set("to", f.To)
	}
	var out LinkStats
	if err := c.do(ctx, http.MethodGet, "/api/stats/"+url.PathEscape(slug), q, nil, &out); err != nil {
		return nil, err
	}
	if out.RecentClicks == nil {
		out.RecentClicks = []Click{}
	}
	return &out, nil
}

// do sends one request. On 401 it invalidates the token and retries once,
// which covers a token revoked or expired between cache check and use.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encoding request: %w", err)
		}
	}
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	for attempt := 0; ; attempt++ {
		status, respBody, err := c.send(ctx, method, u, payload)
		if err != nil {
			return err
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			c.tokens.Invalidate()
			continue
		}
		if status < 200 || status > 299 {
			return &APIError{Status: status, Message: errorMessage(respBody)}
		}
		if out == nil || status == http.StatusNoContent || len(respBody) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decoding response from %s %s: %w", method, path, err)
		}
		return nil
	}
}

func (c *Client) send(ctx context.Context, method, u string, payload []byte) (int, []byte, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("authentication failed: %w", err)
	}
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request to short-link API failed: %w", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("reading response: %w", err)
	}
	return resp.StatusCode, b, nil
}

// errorMessage extracts the message from the API's {"error": "..."} body,
// or API Gateway's {"message": "..."} body for authorizer rejections.
func errorMessage(body []byte) string {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil {
		if e.Error != "" {
			return e.Error
		}
		if e.Message != "" {
			return e.Message
		}
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	if s == "" {
		return "no response body"
	}
	return s
}
