package settings_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings/settingstest"
)

const defaultURL = "https://example.com/default"

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock                   { return &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)} }
func fallbacks(fake *settingstest.SSM, c *clock) *settings.Fallbacks {
	return &settings.Fallbacks{
		Store:      fake.Store(path),
		DefaultURL: defaultURL,
		TTL:        settings.DefaultTTL,
		Retry:      settings.DefaultRetry,
		Now:        c.now,
	}
}

func lookup(t *testing.T, f *settings.Fallbacks, domain, wantURL string, wantKnown bool) {
	t.Helper()
	url, known := f.Fallback(context.Background(), domain)
	if url != wantURL || known != wantKnown {
		t.Errorf("Fallback(%s) = %q, %v; want %q, %v", domain, url, known, wantURL, wantKnown)
	}
}

func seed(fake *settingstest.SSM) {
	fake.Put(path, settings.Domain{Domain: "short.example.com", Role: settings.RolePrimary, Status: settings.StatusActive, FallbackURL: "https://example.com/primary"})
	fake.Put(path, settings.Domain{Domain: "go.example.com", Role: settings.RoleSecondary, Status: settings.StatusActive, FallbackURL: "https://example.com/go"})
	fake.Put(path, settings.Domain{Domain: "new.example.com", Role: settings.RoleSecondary, Status: settings.StatusPendingDNS, FallbackURL: "https://example.com/new"})
	fake.Put(path, settings.Domain{Domain: "bare.example.com", Role: settings.RoleSecondary, Status: settings.StatusActive})
}

func TestFallbackServesOnlyActiveDomains(t *testing.T) {
	fake := settingstest.New()
	seed(fake)
	f := fallbacks(fake, newClock())

	lookup(t, f, "short.example.com", "https://example.com/primary", true)
	lookup(t, f, "go.example.com", "https://example.com/go", true)
	lookup(t, f, "bare.example.com", defaultURL, true)
	lookup(t, f, "new.example.com", "", false)
	lookup(t, f, "unknown.example.com", "", false)
	if n := fake.Count("GetParametersByPath"); n != 1 {
		t.Errorf("SSM reads = %d, want 1 (cached)", n)
	}
}

func TestFallbackRefreshesAfterTTL(t *testing.T) {
	fake := settingstest.New()
	seed(fake)
	c := newClock()
	f := fallbacks(fake, c)
	lookup(t, f, "new.example.com", "", false)

	fake.Put(path, settings.Domain{Domain: "new.example.com", Status: settings.StatusActive, FallbackURL: "https://example.com/new"})
	c.advance(59 * time.Second)
	lookup(t, f, "new.example.com", "", false)
	c.advance(time.Second)
	lookup(t, f, "new.example.com", "https://example.com/new", true)
	if n := fake.Count("GetParametersByPath"); n != 2 {
		t.Errorf("SSM reads = %d, want 2", n)
	}
}

func TestFallbackKeepsLastGoodSettingsOnFailure(t *testing.T) {
	fake := settingstest.New()
	seed(fake)
	c := newClock()
	f := fallbacks(fake, c)
	lookup(t, f, "go.example.com", "https://example.com/go", true)

	fake.SetErr(errors.New("throttled"))
	c.advance(settings.DefaultTTL)
	lookup(t, f, "go.example.com", "https://example.com/go", true)
	lookup(t, f, "unknown.example.com", "", false)
	if n := fake.Count("GetParametersByPath"); n != 2 {
		t.Errorf("SSM reads = %d, want 2 (one failed read, then cached until the next TTL)", n)
	}

	fake.SetErr(nil)
	fake.Put(path, settings.Domain{Domain: "go.example.com", Status: settings.StatusActive, FallbackURL: "https://example.com/changed"})
	c.advance(settings.DefaultTTL)
	lookup(t, f, "go.example.com", "https://example.com/changed", true)
}

func TestFallbackWithoutCacheServesDefault(t *testing.T) {
	fake := settingstest.New()
	seed(fake)
	fake.SetErr(errors.New("access denied"))
	c := newClock()
	f := fallbacks(fake, c)

	lookup(t, f, "anything.example.com", defaultURL, true)
	lookup(t, f, "new.example.com", defaultURL, true)
	if n := fake.Count("GetParametersByPath"); n != 1 {
		t.Errorf("SSM reads = %d, want 1 within the retry interval", n)
	}

	fake.SetErr(nil)
	c.advance(settings.DefaultRetry)
	lookup(t, f, "new.example.com", "", false)
	lookup(t, f, "go.example.com", "https://example.com/go", true)
}
