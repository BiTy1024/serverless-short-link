// Package fakeapi is an in-memory stand-in for the short-link management API
// and a token provider, for tests. It reproduces the routes, status codes and
// response bodies of the links and stats Lambdas.
package fakeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/shortlink"
)

// API implements http.Handler for /api/links, /api/stats and GET /api/domains.
type API struct {
	// ViewerOnly makes every write return 403, as for a 'viewer' user.
	ViewerOnly bool

	mu       sync.Mutex
	links    map[string]shortlink.Link // by key(domain, slug)
	domains  []shortlink.Domain
	clicks   []click
	rejected map[string]bool
	lastAuth string
	lastQry  string
}

type click struct {
	domain, slug, target, ts string
}

func key(domain, slug string) string { return domain + "#" + slug }

func errorJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// domainParam normalises and validates a domain from a request and answers
// 400 when it is invalid.
func domainParam(w http.ResponseWriter, raw string) (string, bool) {
	d := shortlink.NormalizeDomain(raw)
	if d == "" {
		errorJSON(w, http.StatusBadRequest, "Domain is required")
		return "", false
	}
	if shortlink.ValidateDomain(d) != nil {
		errorJSON(w, http.StatusBadRequest, fmt.Sprintf("Domain '%s' is not a valid hostname", d))
		return "", false
	}
	return d, true
}

// optionalDomain is domainParam for the ?domain= filter: absent means all.
func optionalDomain(w http.ResponseWriter, r *http.Request) (string, bool) {
	if strings.TrimSpace(r.URL.Query().Get("domain")) == "" {
		return "", true
	}
	return domainParam(w, r.URL.Query().Get("domain"))
}

// splitLink splits "<domain>/<slug>" from a route; ok is false for any other shape.
func splitLink(rest string) (domain, slug string, ok bool) {
	domain, slug, ok = strings.Cut(rest, "/")
	return domain, slug, ok && slug != "" && !strings.Contains(slug, "/")
}

// New returns an empty API.
func New() *API {
	return &API{links: map[string]shortlink.Link{}, rejected: map[string]bool{}}
}

// RejectToken makes requests bearing token answer 401.
func (a *API) RejectToken(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.rejected[token] = true
}

// AddClick records a click on slug at domain.
func (a *API) AddClick(domain, slug, target, ts string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clicks = append(a.clicks, click{domain, slug, target, ts})
}

// AddDomain adds a domain setting to the GET /api/domains response.
func (a *API) AddDomain(d shortlink.Domain) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.domains = append(a.domains, d)
}

// LastAuthorization returns the Authorization header of the last request.
func (a *API) LastAuthorization() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastAuth
}

// LastQuery returns the raw query string of the last request.
func (a *API) LastQuery() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastQry
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.lastAuth = r.Header.Get("Authorization")
	a.lastQry = r.URL.RawQuery
	token, ok := strings.CutPrefix(a.lastAuth, "Bearer ")
	if !ok || token == "" || a.rejected[token] {
		// API Gateway's JWT authorizer answers with "message", not "error".
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Unauthorized"})
		return
	}

	path := r.URL.Path
	switch {
	case path == "/api/links":
		switch r.Method {
		case http.MethodGet:
			a.list(w, r)
		case http.MethodPost:
			a.write(w, func() { a.create(w, r) })
		default:
			notFound(w)
		}
	case strings.HasPrefix(path, "/api/links/"):
		d, slug, ok := splitLink(strings.TrimPrefix(path, "/api/links/"))
		switch {
		case !ok:
			notFound(w)
		case r.Method == http.MethodPut:
			a.write(w, func() { a.update(w, r, d, slug) })
		case r.Method == http.MethodDelete:
			a.write(w, func() { a.delete(w, d, slug) })
		default:
			notFound(w)
		}
	case path == "/api/domains" && r.Method == http.MethodGet:
		domains := append([]shortlink.Domain{}, a.domains...)
		writeJSON(w, http.StatusOK, map[string]any{"domains": domains, "count": len(domains), "route53_zone_ids": []string{}})
	case path == "/api/stats" && r.Method == http.MethodGet:
		a.overview(w, r)
	case strings.HasPrefix(path, "/api/stats/") && r.Method == http.MethodGet:
		d, slug, ok := splitLink(strings.TrimPrefix(path, "/api/stats/"))
		if !ok {
			notFound(w)
			return
		}
		a.detail(w, r, d, slug)
	default:
		notFound(w)
	}
}

func (a *API) write(w http.ResponseWriter, f func()) {
	if a.ViewerOnly {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Admin access required"})
		return
	}
	f()
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	d, ok := optionalDomain(w, r)
	if !ok {
		return
	}
	links := make([]shortlink.Link, 0, len(a.links))
	for _, l := range a.links {
		if d == "" || l.Domain == d {
			links = append(links, l)
		}
	}
	sort.Slice(links, func(i, j int) bool { return key(links[i].Domain, links[i].Slug) < key(links[j].Domain, links[j].Slug) })
	writeJSON(w, http.StatusOK, map[string]any{"links": links, "count": len(links)})
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Domain    string `json:"domain"`
		Slug      string `json:"slug"`
		TargetURL string `json:"target_url"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}
	d, ok := domainParam(w, body.Domain)
	if !ok {
		return
	}
	if err := shortlink.ValidateSlug(body.Slug); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := shortlink.ValidateTargetURL(body.TargetURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if _, exists := a.links[key(d, body.Slug)]; exists {
		errorJSON(w, http.StatusConflict, fmt.Sprintf("Slug '%s' already exists on %s", body.Slug, d))
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	l := shortlink.Link{Domain: d, Slug: body.Slug, TargetURL: body.TargetURL, CreatedAt: now, UpdatedAt: now}
	a.links[key(d, l.Slug)] = l
	writeJSON(w, http.StatusCreated, l)
}

func (a *API) update(w http.ResponseWriter, r *http.Request, rawDomain, slug string) {
	d, ok := domainParam(w, rawDomain)
	if !ok {
		return
	}
	var body struct {
		TargetURL string `json:"target_url"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}
	if err := shortlink.ValidateTargetURL(body.TargetURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	l, ok := a.links[key(d, slug)]
	if !ok {
		linkNotFound(w, d, slug)
		return
	}
	l.TargetURL = body.TargetURL
	l.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	a.links[key(d, slug)] = l
	writeJSON(w, http.StatusOK, l)
}

func linkNotFound(w http.ResponseWriter, domain, slug string) {
	errorJSON(w, http.StatusNotFound, fmt.Sprintf("Slug '%s' not found on %s", slug, domain))
}

func (a *API) delete(w http.ResponseWriter, rawDomain, slug string) {
	d, ok := domainParam(w, rawDomain)
	if !ok {
		return
	}
	if _, ok := a.links[key(d, slug)]; !ok {
		linkNotFound(w, d, slug)
		return
	}
	delete(a.links, key(d, slug))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	d, ok := optionalDomain(w, r)
	if !ok {
		return
	}
	linkedOnly := strings.EqualFold(r.URL.Query().Get("linked_only"), "true")
	byKey := map[string]*shortlink.LinkStatsSummary{}
	total := 0
	for _, c := range a.clicks {
		if d != "" && c.domain != d {
			continue
		}
		k := key(c.domain, c.slug)
		if _, ok := a.links[k]; linkedOnly && !ok {
			continue
		}
		s := byKey[k]
		if s == nil {
			s = &shortlink.LinkStatsSummary{Domain: c.domain, Slug: c.slug}
			byKey[k] = s
		}
		s.Clicks++
		total++
		target, ts := c.target, c.ts
		s.TargetURL = &target
		if s.FirstClick == nil || ts < *s.FirstClick {
			s.FirstClick = &ts
		}
		if s.LastClick == nil || ts > *s.LastClick {
			s.LastClick = &ts
		}
	}
	stats := make([]shortlink.LinkStatsSummary, 0, len(byKey))
	for _, s := range byKey {
		stats = append(stats, *s)
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Clicks > stats[j].Clicks })
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats, "total_clicks": total})
}

func (a *API) detail(w http.ResponseWriter, r *http.Request, rawDomain, slug string) {
	d, ok := domainParam(w, rawDomain)
	if !ok {
		return
	}
	q := r.URL.Query()
	var start, end string
	switch {
	case q.Get("from") != "" || q.Get("to") != "":
		if f := q.Get("from"); f != "" {
			start = f + "T00:00:00+00:00"
		}
		if t := q.Get("to"); t != "" {
			end = t + "T23:59:59+00:00"
		}
	case q.Get("days") != "":
		n, err := strconv.Atoi(q.Get("days"))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid time filter. Use days=N or from=YYYY-MM-DD&to=YYYY-MM-DD"})
			return
		}
		start = time.Now().UTC().AddDate(0, 0, -n).Format("2006-01-02T15:04:05+00:00")
	}
	var matched []click
	for _, c := range a.clicks {
		if c.domain != d || c.slug != slug || (start != "" && c.ts < start) || (end != "" && c.ts > end) {
			continue
		}
		matched = append(matched, c)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ts > matched[j].ts })
	recent := []shortlink.Click{}
	for i, c := range matched {
		if i == 20 {
			break
		}
		recent = append(recent, shortlink.Click{Timestamp: c.ts})
	}
	var target *string
	if len(matched) > 0 {
		target = &matched[0].target
	}
	writeJSON(w, http.StatusOK, shortlink.LinkStats{Domain: d, Slug: slug, Clicks: len(matched), TargetURL: target, RecentClicks: recent})
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "Route not found"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Tokens is a shortlink.TokenProvider that hands out token-1, token-2, ...
// and moves to the next token after each Invalidate.
type Tokens struct {
	// Err, if set, is returned by Token.
	Err error

	mu            sync.Mutex
	n             int
	invalidations int
}

// NewTokens returns a provider whose first token is token-1.
func NewTokens() *Tokens { return &Tokens{n: 1} }

// Token implements shortlink.TokenProvider.
func (t *Tokens) Token(context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.Err != nil {
		return "", t.Err
	}
	return fmt.Sprintf("token-%d", t.n), nil
}

// Invalidate implements shortlink.TokenProvider.
func (t *Tokens) Invalidate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.n++
	t.invalidations++
}

// Invalidations reports how often Invalidate was called.
func (t *Tokens) Invalidations() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.invalidations
}
