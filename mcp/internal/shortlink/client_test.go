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

	created, err := c.CreateLink(ctx, "Go.Example.com", "docs", "https://example.com/docs")
	if err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	if created.Domain != "go.example.com" || created.Slug != "docs" || created.CreatedAt == "" {
		t.Fatalf("created = %+v", created)
	}

	if _, err := c.CreateLink(ctx, "go.example.com", "docs", "https://example.com/other"); !isStatus(err, http.StatusConflict) {
		t.Fatalf("duplicate create err = %v, want 409", err)
	}

	updated, err := c.UpdateLink(ctx, "go.example.com", "docs", "https://example.com/v2")
	if err != nil || updated.TargetURL != "https://example.com/v2" {
		t.Fatalf("UpdateLink = %+v, %v", updated, err)
	}

	got, err := c.GetLink(ctx, "go.example.com", "docs")
	if err != nil || got.TargetURL != "https://example.com/v2" {
		t.Fatalf("GetLink = %+v, %v", got, err)
	}

	links, err := c.ListLinks(ctx, "")
	if err != nil || len(links) != 1 {
		t.Fatalf("ListLinks = %+v, %v", links, err)
	}

	if err := c.DeleteLink(ctx, "go.example.com", "docs"); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}
	if _, err := c.GetLink(ctx, "go.example.com", "docs"); !isStatus(err, http.StatusNotFound) {
		t.Fatalf("GetLink after delete err = %v, want 404", err)
	}
	if err := c.DeleteLink(ctx, "go.example.com", "docs"); !isStatus(err, http.StatusNotFound) {
		t.Fatalf("second delete err = %v, want 404", err)
	}
}

func TestBearerTokenSent(t *testing.T) {
	api := fakeapi.New()
	tokens := fakeapi.NewTokens()
	if _, err := newClient(api, tokens).ListLinks(context.Background(), ""); err != nil {
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
	if _, err := newClient(api, tokens).ListLinks(context.Background(), ""); err != nil {
		t.Fatalf("ListLinks: %v", err)
	}
	if tokens.Invalidations() != 1 || api.LastAuthorization() != "Bearer token-2" {
		t.Fatalf("invalidations=%d auth=%q", tokens.Invalidations(), api.LastAuthorization())
	}
}

func TestViewerCannotWrite(t *testing.T) {
	api := fakeapi.New()
	api.ViewerOnly = true
	_, err := newClient(api, fakeapi.NewTokens()).CreateLink(context.Background(), "go.example.com", "docs", "https://example.com")
	if !isStatus(err, http.StatusForbidden) || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenErrorIsReported(t *testing.T) {
	tokens := fakeapi.NewTokens()
	tokens.Err = errors.New("boom")
	_, err := newClient(fakeapi.New(), tokens).ListLinks(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "authentication failed: boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestStats(t *testing.T) {
	ctx := context.Background()
	api := fakeapi.New()
	c := newClient(api, fakeapi.NewTokens())
	if _, err := c.CreateLink(ctx, "go.example.com", "docs", "https://example.com/docs"); err != nil {
		t.Fatal(err)
	}
	api.AddClick("go.example.com", "docs", "https://example.com/docs", "2026-11-01T10:00:00+00:00")
	api.AddClick("go.example.com", "docs", "https://example.com/docs", "2026-11-03T10:00:00+00:00")
	api.AddClick("go.example.com", "gone", "https://example.com/gone", "2026-11-02T10:00:00+00:00")
	api.AddClick("other.example", "docs", "https://example.com/o", "2026-11-02T10:00:00+00:00")

	all, err := c.StatsOverview(ctx, "", false)
	if err != nil || all.TotalClicks != 4 || len(all.Stats) != 3 || all.Stats[0].Slug != "docs" || all.Stats[0].Domain != "go.example.com" {
		t.Fatalf("overview = %+v, %v", all, err)
	}
	linked, err := c.StatsOverview(ctx, "", true)
	if err != nil || linked.TotalClicks != 2 || len(linked.Stats) != 1 {
		t.Fatalf("linked overview = %+v, %v", linked, err)
	}
	other, err := c.StatsOverview(ctx, "other.example", false)
	if err != nil || other.TotalClicks != 1 || len(other.Stats) != 1 || other.Stats[0].Domain != "other.example" {
		t.Fatalf("other.example overview = %+v, %v", other, err)
	}
	if q := api.LastQuery(); q != "domain=other.example&linked_only=false" {
		t.Fatalf("query = %q", q)
	}

	detail, err := c.LinkStats(ctx, "go.example.com", "docs", shortlink.StatsFilter{From: "2026-11-02", To: "2026-11-30"})
	if err != nil || detail.Clicks != 1 || detail.Domain != "go.example.com" || detail.Slug != "docs" || len(detail.RecentClicks) != 1 {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	if q := api.LastQuery(); q != "from=2026-11-02&to=2026-11-30" {
		t.Fatalf("query = %q", q)
	}

	if _, err := c.LinkStats(ctx, "go.example.com", "docs", shortlink.StatsFilter{Days: 7}); err != nil {
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

func TestSameSlugOnTwoDomains(t *testing.T) {
	ctx := context.Background()
	api := fakeapi.New()
	c := newClient(api, fakeapi.NewTokens())
	for _, d := range []string{"a.example", "b.example"} {
		if _, err := c.CreateLink(ctx, d, "docs", "https://"+d+"/docs"); err != nil {
			t.Fatalf("CreateLink on %s: %v", d, err)
		}
	}
	if q := api.LastQuery(); q != "" {
		t.Fatalf("create query = %q", q)
	}
	b, err := c.GetLink(ctx, "b.example", "docs")
	if err != nil || b.TargetURL != "https://b.example/docs" {
		t.Fatalf("GetLink b = %+v, %v", b, err)
	}
	if q := api.LastQuery(); q != "domain=b.example" {
		t.Fatalf("list query = %q", q)
	}
	if err := c.DeleteLink(ctx, "a.example", "docs"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetLink(ctx, "b.example", "docs"); err != nil {
		t.Fatalf("b.example/docs gone after deleting a.example/docs: %v", err)
	}
	all, err := c.ListLinks(ctx, "")
	if err != nil || len(all) != 1 || all[0].Domain != "b.example" {
		t.Fatalf("ListLinks = %+v, %v", all, err)
	}
}

func TestInvalidDomainIsRejectedByAPI(t *testing.T) {
	_, err := newClient(fakeapi.New(), fakeapi.NewTokens()).CreateLink(context.Background(), "localhost", "docs", "https://example.com")
	if !isStatus(err, http.StatusBadRequest) || !strings.Contains(err.Error(), "not a valid hostname") {
		t.Fatalf("err = %v", err)
	}
}

func TestValidateDomain(t *testing.T) {
	valid := []string{"example.com", "go.example.com", "a.co", "xn--bcher-kva.example", "123.example", strings.Repeat("a", 63) + ".com"}
	invalid := []string{"", "localhost", "Example.com", "example.com.", ".example.com", "a..b", "-a.example", "a-.example",
		"example.com:443", "https://example.com", "a_b.example", "127.0.0.1", strings.Repeat("a", 64) + ".com", strings.Repeat("a.", 126) + "com"}
	for _, d := range valid {
		if err := shortlink.ValidateDomain(d); err != nil {
			t.Errorf("ValidateDomain(%q) = %v, want nil", d, err)
		}
	}
	for _, d := range invalid {
		if err := shortlink.ValidateDomain(d); err == nil {
			t.Errorf("ValidateDomain(%q) = nil, want error", d)
		}
	}
	if got := shortlink.NormalizeDomain("  Go.Example.COM "); got != "go.example.com" {
		t.Errorf("NormalizeDomain = %q", got)
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
