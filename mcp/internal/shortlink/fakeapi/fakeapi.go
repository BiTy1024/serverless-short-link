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

// API implements http.Handler for /api/links and /api/stats.
type API struct {
	// ViewerOnly makes every write return 403, as for a 'viewer' user.
	ViewerOnly bool

	mu       sync.Mutex
	links    map[string]shortlink.Link
	clicks   []click
	rejected map[string]bool
	lastAuth string
	lastQry  string
}

type click struct {
	path, target, ts string
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

// AddClick records a click; path includes the leading slash.
func (a *API) AddClick(path, target, ts string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clicks = append(a.clicks, click{path, target, ts})
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
			a.list(w)
		case http.MethodPost:
			a.write(w, func() { a.create(w, r) })
		default:
			notFound(w)
		}
	case strings.HasPrefix(path, "/api/links/"):
		slug := strings.TrimPrefix(path, "/api/links/")
		switch r.Method {
		case http.MethodPut:
			a.write(w, func() { a.update(w, r, slug) })
		case http.MethodDelete:
			a.write(w, func() { a.delete(w, slug) })
		default:
			notFound(w)
		}
	case path == "/api/stats" && r.Method == http.MethodGet:
		a.overview(w, r)
	case strings.HasPrefix(path, "/api/stats/") && r.Method == http.MethodGet:
		a.detail(w, r, "/"+strings.TrimPrefix(path, "/api/stats/"))
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

func (a *API) list(w http.ResponseWriter) {
	links := make([]shortlink.Link, 0, len(a.links))
	for _, l := range a.links {
		links = append(links, l)
	}
	sort.Slice(links, func(i, j int) bool { return links[i].ShortPath < links[j].ShortPath })
	writeJSON(w, http.StatusOK, map[string]any{"links": links, "count": len(links)})
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ShortPath string `json:"short_path"`
		TargetURL string `json:"target_url"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON body"})
		return
	}
	if err := shortlink.ValidateSlug(body.ShortPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := shortlink.ValidateTargetURL(body.TargetURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if _, exists := a.links[body.ShortPath]; exists {
		writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf("Path '%s' already exists", body.ShortPath)})
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	l := shortlink.Link{ShortPath: body.ShortPath, TargetURL: body.TargetURL, CreatedAt: now, UpdatedAt: now}
	a.links[l.ShortPath] = l
	writeJSON(w, http.StatusCreated, l)
}

func (a *API) update(w http.ResponseWriter, r *http.Request, slug string) {
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
	l, ok := a.links[slug]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("Path '%s' not found", slug)})
		return
	}
	l.TargetURL = body.TargetURL
	l.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	a.links[slug] = l
	writeJSON(w, http.StatusOK, l)
}

func (a *API) delete(w http.ResponseWriter, slug string) {
	if _, ok := a.links[slug]; !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("Path '%s' not found", slug)})
		return
	}
	delete(a.links, slug)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	linkedOnly := strings.EqualFold(r.URL.Query().Get("linked_only"), "true")
	byPath := map[string]*shortlink.LinkStatsSummary{}
	total := 0
	for _, c := range a.clicks {
		if _, ok := a.links[strings.TrimPrefix(c.path, "/")]; linkedOnly && !ok {
			continue
		}
		s := byPath[c.path]
		if s == nil {
			s = &shortlink.LinkStatsSummary{Path: c.path}
			byPath[c.path] = s
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
	stats := make([]shortlink.LinkStatsSummary, 0, len(byPath))
	for _, s := range byPath {
		stats = append(stats, *s)
	}
	sort.Slice(stats, func(i, j int) bool { return stats[i].Clicks > stats[j].Clicks })
	writeJSON(w, http.StatusOK, map[string]any{"stats": stats, "total_clicks": total})
}

func (a *API) detail(w http.ResponseWriter, r *http.Request, path string) {
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
		if c.path != path || (start != "" && c.ts < start) || (end != "" && c.ts > end) {
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
	writeJSON(w, http.StatusOK, shortlink.LinkStats{Path: path, Clicks: len(matched), TargetURL: target, RecentClicks: recent})
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
