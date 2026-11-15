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

// Slug and domain rules mirror the validation in the links Lambda.
const (
	// SlugPattern is the regular expression a slug must match.
	SlugPattern = `^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`
	// MaxSlugLength is the maximum length of a slug.
	MaxSlugLength = 64
	// DomainPattern is a coarse shape check for a domain in either case; it
	// fits JSON Schema, which has no case-insensitive flag. ValidateDomain
	// applies the exact rules.
	DomainPattern = `^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$`
	// MaxDomainLength is the maximum length of a domain.
	MaxDomainLength = 253
)

// ReservedSlugs cannot be used as slugs.
var ReservedSlugs = []string{"api", "admin", "health", "status"}

var (
	slugRE  = regexp.MustCompile(SlugPattern)
	labelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// NormalizeDomain returns domain in the form the API stores it: trimmed and
// lowercased.
func NormalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

// ValidateDomain applies the API's hostname rules to a normalised domain:
// at least two dot-separated labels of letters, digits and inner hyphens, at
// most 63 characters per label and 253 in total, and a last label that
// contains a letter (so IP addresses are rejected).
func ValidateDomain(domain string) error {
	if domain == "" {
		return errors.New("domain is required")
	}
	invalid := fmt.Errorf("domain %q is not a valid hostname, e.g. go.example.com", domain)
	if len(domain) > MaxDomainLength {
		return invalid
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return invalid
	}
	for _, l := range labels {
		if !labelRE.MatchString(l) {
			return invalid
		}
	}
	if !strings.ContainsAny(labels[len(labels)-1], "abcdefghijklmnopqrstuvwxyz") {
		return invalid
	}
	return nil
}

// ValidateSlug applies the same rules as the API and returns the API's
// wording on failure, so a bad slug is rejected before any request is sent.
func ValidateSlug(slug string) error {
	if slug == "" {
		return errors.New("slug is required")
	}
	for _, r := range ReservedSlugs {
		if slug == r {
			return fmt.Errorf("slug %q is reserved and cannot be used as a short link", slug)
		}
	}
	if len(slug) > MaxSlugLength {
		return fmt.Errorf("slug must be at most %d characters", MaxSlugLength)
	}
	if strings.Contains(slug, "--") {
		return errors.New("slug must not contain consecutive hyphens")
	}
	if !slugRE.MatchString(slug) {
		return errors.New("slug must contain only lowercase letters, numbers, and hyphens, and must start and end with a letter or number")
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

// Link is a short link as stored by the API. It is served at
// https://<domain>/<slug>; slugs are unique per domain.
type Link struct {
	Domain    string `json:"domain" jsonschema:"the domain the link is served on, e.g. go.example.com"`
	Slug      string `json:"slug" jsonschema:"the slug, without leading slash, unique per domain"`
	TargetURL string `json:"target_url" jsonschema:"the https URL the short link redirects to"`
	CreatedAt string `json:"created_at" jsonschema:"creation time, ISO 8601 UTC"`
	UpdatedAt string `json:"updated_at" jsonschema:"last update time, ISO 8601 UTC"`
}

// LinkStatsSummary is one row of the stats overview.
type LinkStatsSummary struct {
	Domain     string  `json:"domain" jsonschema:"the domain the clicks were recorded on"`
	Slug       string  `json:"slug" jsonschema:"the slug the clicks were recorded on, without leading slash"`
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

// LinkStats is the response of GET /api/stats/{domain}/{slug}.
type LinkStats struct {
	Domain       string  `json:"domain" jsonschema:"the domain of the link"`
	Slug         string  `json:"slug" jsonschema:"the slug of the link"`
	Clicks       int     `json:"clicks" jsonschema:"number of clicks in the requested time range"`
	TargetURL    *string `json:"target_url" jsonschema:"target URL recorded with the clicks, null if there were none"`
	RecentClicks []Click `json:"recent_clicks" jsonschema:"up to 20 most recent clicks in the range, newest first"`
}

// StatsFilter narrows GET /api/stats/{domain}/{slug} to a time range. Days and
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

// ListLinks returns the links of domain, or of every domain when domain is "".
func (c *Client) ListLinks(ctx context.Context, domain string) ([]Link, error) {
	var out struct {
		Links []Link `json:"links"`
		Count int    `json:"count"`
	}
	var q url.Values
	if domain != "" {
		q = url.Values{"domain": {domain}}
	}
	if err := c.do(ctx, http.MethodGet, "/api/links", q, nil, &out); err != nil {
		return nil, err
	}
	if out.Links == nil {
		out.Links = []Link{}
	}
	return out.Links, nil
}

// GetLink returns the link with the given slug on domain. The API has no
// single-link endpoint, so this lists the links of the domain and picks the
// match.
func (c *Client) GetLink(ctx context.Context, domain, slug string) (*Link, error) {
	links, err := c.ListLinks(ctx, domain)
	if err != nil {
		return nil, err
	}
	for i := range links {
		if links[i].Slug == slug {
			return &links[i], nil
		}
	}
	return nil, &APIError{Status: http.StatusNotFound, Message: fmt.Sprintf("Slug '%s' not found on %s", slug, domain)}
}

// CreateLink creates a new link. The API returns 409 if the slug exists on
// the domain.
func (c *Client) CreateLink(ctx context.Context, domain, slug, targetURL string) (*Link, error) {
	body := map[string]string{"domain": domain, "slug": slug, "target_url": targetURL}
	var out Link
	if err := c.do(ctx, http.MethodPost, "/api/links", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateLink changes the target URL of an existing link.
func (c *Client) UpdateLink(ctx context.Context, domain, slug, targetURL string) (*Link, error) {
	body := map[string]string{"target_url": targetURL}
	var out Link
	if err := c.do(ctx, http.MethodPut, linkPath("/api/links/", domain, slug), nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteLink deletes a link. Recorded clicks are kept by the API.
func (c *Client) DeleteLink(ctx context.Context, domain, slug string) error {
	return c.do(ctx, http.MethodDelete, linkPath("/api/links/", domain, slug), nil, nil, nil)
}

// linkPath appends /<domain>/<slug> to prefix, each segment escaped.
func linkPath(prefix, domain, slug string) string {
	return prefix + url.PathEscape(domain) + "/" + url.PathEscape(slug)
}

// StatsOverview returns click totals per link, sorted by clicks descending,
// for domain or for every domain when domain is "". With linkedOnly, clicks
// on links that no longer exist are left out.
func (c *Client) StatsOverview(ctx context.Context, domain string, linkedOnly bool) (*StatsOverview, error) {
	q := url.Values{"linked_only": {strconv.FormatBool(linkedOnly)}}
	if domain != "" {
		q.Set("domain", domain)
	}
	var out StatsOverview
	if err := c.do(ctx, http.MethodGet, "/api/stats", q, nil, &out); err != nil {
		return nil, err
	}
	if out.Stats == nil {
		out.Stats = []LinkStatsSummary{}
	}
	return &out, nil
}

// LinkStats returns click statistics for one link.
func (c *Client) LinkStats(ctx context.Context, domain, slug string, f StatsFilter) (*LinkStats, error) {
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
	if err := c.do(ctx, http.MethodGet, linkPath("/api/stats/", domain, slug), q, nil, &out); err != nil {
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
