package shortlink_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/shortlink"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/shortlink/fakeapi"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/testhttp"
)

func newClient(api *fakeapi.API, tokens *fakeapi.Tokens) *shortlink.Client {
	return shortlink.NewClient("https://short.test/", tokens, testhttp.Client(api))
}

func TestLinkLifecycle(t *testing.T) {
	ctx := context.Background()
	api := fakeapi.New()
	c := newClient(api, fakeapi.NewTokens())

	created, err := c.CreateLink(ctx, "docs", "https://example.com/docs")
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if created.ShortPath != "docs" || created.CreatedAt == "" {
		t.Fatalf("created = %+v", created)
	}

	if _, err := c.CreateLink(ctx, "docs", "https://example.com/other"); !isStatus(err, http.StatusConflict) {
		t.Fatalf("duplicate create err = %v, want 409", err)
	}

	updated, err := c.UpdateLink(ctx, "docs", "https://example.com/v2")
	if err != nil || updated.TargetURL != "https://example.com/v2" {
		t.Fatalf("UpdateLink = %+v, %v", updated, err)
	}

	got, err := c.GetLink(ctx, "docs")
	if err != nil || got.TargetURL != "https://example.com/v2" {
		t.Fatalf("GetLink = %+v, %v", got, err)
	}

	links, err := c.ListLinks(ctx)
	if err != nil || len(links) != 1 {
		t.Fatalf("ListLinks = %+v, %v", links, err)
	}

	if err := c.DeleteLink(ctx, "docs"); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	if _, err := c.GetLink(ctx, "docs"); !isStatus(err, http.StatusNotFound) {
		t.Fatalf("GetLink after delete err = %v, want 404", err)
	}
	if err := c.DeleteLink(ctx, "docs"); !isStatus(err, http.StatusNotFound) {
		t.Fatalf("second delete err = %v, want 404", err)
	}
}

func TestBearerTokenSent(t *testing.T) {
	api := fakeapi.New()
	tokens := fakeapi.NewTokens()
	if _, err := newClient(api, tokens).ListLinks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := api.LastAuthorization(); got != "Bearer token-1" {
		t.Fatalf("Authorization = %q", got)
	}
}

func TestUnauthorizedRetriesOnceWithFreshToken(t *testing.T) {
	api := fakeapi.New()
	api.RejectToken("token-1")
	tokens := fakeapi.NewTokens()
	if _, err := newClient(api, tokens).ListLinks(context.Background()); err != nil {
		t.Fatalf("ListLinks: %v", err)
	}
	if tokens.Invalidations() != 1 || api.LastAuthorization() != "Bearer token-2" {
		t.Fatalf("invalidations=%d auth=%q", tokens.Invalidations(), api.LastAuthorization())
	}
}

func TestViewerCannotWrite(t *testing.T) {
	api := fakeapi.New()
	api.ViewerOnly = true
	_, err := newClient(api, fakeapi.NewTokens()).CreateLink(context.Background(), "docs", "https://example.com")
	if !isStatus(err, http.StatusForbidden) || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenErrorIsReported(t *testing.T) {
	tokens := fakeapi.NewTokens()
	tokens.Err = errors.New("boom")
	_, err := newClient(fakeapi.New(), tokens).ListLinks(context.Background())
	if err == nil || !strings.Contains(err.Error(), "authentication failed: boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	api := fakeapi.New()
	c := newClient(api, fakeapi.NewTokens())
	if _, err := c.CreateLink(ctx, "docs", "https://example.com/docs"); err != nil {
		t.Fatal(err)
	}
	api.AddClick("/docs", "https://example.com/docs", "2026-11-01T10:00:00+00:00")
	api.AddClick("/docs", "https://example.com/docs", "2026-11-03T10:00:00+00:00")
	api.AddClick("/gone", "https://example.com/gone", "2026-11-02T10:00:00+00:00")

	all, err := c.StatsOverview(ctx, false)
	if err != nil || all.TotalClicks != 3 || len(all.Stats) != 2 || all.Stats[0].Path != "/docs" {
		t.Fatalf("overview = %+v, %v", all, err)
	}
	linked, err := c.StatsOverview(ctx, true)
	if err != nil || linked.TotalClicks != 2 || len(linked.Stats) != 1 {
		t.Fatalf("linked overview = %+v, %v", linked, err)
	}

	detail, err := c.LinkStats(ctx, "docs", shortlink.StatsFilter{From: "2026-11-02", To: "2026-11-30"})
	if err != nil || detail.Clicks != 1 || detail.Path != "/docs" || len(detail.RecentClicks) != 1 {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	if q := api.LastQuery(); q != "from=2026-11-02&to=2026-11-30" {
		t.Fatalf("query = %q", q)
	}

	if _, err := c.LinkStats(ctx, "docs", shortlink.StatsFilter{Days: 7}); err != nil {
		t.Fatal(err)
	}
	if q := api.LastQuery(); q != "days=7" {
		t.Fatalf("query = %q", q)
	}
}

func TestValidateSlug(t *testing.T) {
	valid := []string{"a", "docs", "a-b", "x1", strings.Repeat("a", 64)}
	invalid := []string{"", "api", "admin", "health", "status", "-a", "a-", "a--b", "A", "a_b", "a/b", strings.Repeat("a", 65)}
	for _, s := range valid {
		if err := shortlink.ValidateSlug(s); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range invalid {
		if err := shortlink.ValidateSlug(s); err == nil {
			t.Errorf("ValidateSlug(%q) = nil, want error", s)
		}
	}
}

func TestValidateTargetURL(t *testing.T) {
	if err := shortlink.ValidateTargetURL("https://example.com"); err != nil {
		t.Error(err)
	}
	for _, s := range []string{"", "http://example.com", "https://x"} {
		if shortlink.ValidateTargetURL(s) == nil {
			t.Errorf("ValidateTargetURL(%q) = nil, want error", s)
		}
	}
}

func isStatus(err error, status int) bool {
	var apiErr *shortlink.APIError
	return errors.As(err, &apiErr) && apiErr.Status == status
}
