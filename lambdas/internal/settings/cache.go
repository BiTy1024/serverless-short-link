package settings

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Lister reads every stored domain.
type Lister interface {
	List(ctx context.Context) ([]Domain, error)
}

// Defaults for Fallbacks.
const (
	DefaultTTL = 60 * time.Second
	// DefaultRetry is how long Fallbacks waits before it asks SSM again after
	// a failed read when it has no cached settings.
	DefaultRetry = 5 * time.Second
)

// Fallbacks answers the redirect's question "is this domain served, and
// where does it send unknown slugs" from the stored settings, cached in
// memory. Only active domains are served; an active domain without a
// fallback URL falls back to DefaultURL.
//
// The settings are read again once TTL has passed. When a read fails, the
// last good settings stay in use until the next TTL has passed. Without any
// good read yet, every domain is served with DefaultURL as its fallback, and
// SSM is asked again after Retry.
type Fallbacks struct {
	Store      Lister
	DefaultURL string
	TTL        time.Duration
	Retry      time.Duration
	Now        func() time.Time

	mu      sync.Mutex
	active  map[string]string // domain -> fallback URL
	loaded  bool
	nextTry time.Time
}

// Fallback returns the fallback URL of domain; known is false when the
// domain is not stored or not active.
func (f *Fallbacks) Fallback(ctx context.Context, domain string) (url string, known bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.Now()
	if !now.Before(f.nextTry) {
		f.refresh(ctx, now)
	}
	if !f.loaded {
		return f.DefaultURL, true
	}
	url, known = f.active[domain]
	if known && url == "" {
		url = f.DefaultURL
	}
	return url, known
}

// refresh reads the settings; the caller holds f.mu.
func (f *Fallbacks) refresh(ctx context.Context, now time.Time) {
	domains, err := f.Store.List(ctx)
	if err != nil {
		if f.loaded {
			slog.Error("Reading domain settings failed, using cached settings", "error", err.Error())
			f.nextTry = now.Add(f.TTL)
		} else {
			slog.Error("Reading domain settings failed, serving every domain with the default fallback", "error", err.Error())
			f.nextTry = now.Add(f.Retry)
		}
		return
	}
	active := make(map[string]string, len(domains))
	for _, d := range domains {
		if d.Status == StatusActive {
			active[d.Domain] = d.FallbackURL
		}
	}
	f.active, f.loaded, f.nextTry = active, true, now.Add(f.TTL)
}
