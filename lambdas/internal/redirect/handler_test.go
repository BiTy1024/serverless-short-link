package redirect

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/domain"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store/storetest"
)

const (
	defaultURL  = "https://example.com/home"
	adminOrigin = "https://admin.short.example"
	host        = "go.example.com"
)

var fixedNow = time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)

func newHandler(s Store) *Handler {
	return &Handler{
		Store:       s,
		Domains:     domain.Static{FallbackURL: defaultURL},
		AdminOrigin: adminOrigin,
		Now:         func() time.Time { return fixedNow },
	}
}

// request builds a request to host, given as the API Gateway domain name.
func request(method, path string) events.APIGatewayV2HTTPRequest {
	var req events.APIGatewayV2HTTPRequest
	req.RawPath = path
	req.RequestContext.HTTP.Method = method
	req.RequestContext.DomainName = host
	return req
}

func promo(domain string) store.Link {
	return store.Link{Domain: domain, Slug: "promo", TargetURL: "https://" + domain + "/target"}
}

func TestRedirectHit(t *testing.T) {
	for _, path := range []string{"/promo", "/promo/", "//promo//"} {
		t.Run(path, func(t *testing.T) {
			fake := storetest.NewFake(promo(host))
			resp, err := newHandler(fake).Handle(context.Background(), request("GET", path))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 301 {
				t.Fatalf("status = %d, want 301", resp.StatusCode)
			}
			if got := resp.Headers["Location"]; got != "https://go.example.com/target" {
				t.Errorf("Location = %q", got)
			}
			if len(resp.Headers) != 1 || resp.Body != "" {
				t.Errorf("unexpected headers %v or body %q", resp.Headers, resp.Body)
			}
			if len(fake.Clicks) != 1 {
				t.Fatalf("clicks = %d, want 1", len(fake.Clicks))
			}
			c := fake.Clicks[0]
			if c.LinkKey != "go.example.com#promo" || c.Domain != host || c.Slug != "promo" ||
				c.Timestamp != "2026-03-04T05:06:07.123456+00:00" ||
				c.TargetURL == nil || *c.TargetURL != "https://go.example.com/target" {
				t.Errorf("click = %+v", c)
			}
		})
	}
}

func TestRedirectMissUsesDefaultWithoutTracking(t *testing.T) {
	fake := storetest.NewFake()
	resp, _ := newHandler(fake).Handle(context.Background(), request("GET", "/unknown"))
	if resp.StatusCode != 301 || resp.Headers["Location"] != defaultURL {
		t.Fatalf("got %d %v", resp.StatusCode, resp.Headers)
	}
	if len(fake.Clicks) != 0 {
		t.Errorf("miss was tracked: %+v", fake.Clicks)
	}
}

func TestRedirectRootUsesDefaultWithoutLookup(t *testing.T) {
	for _, path := range []string{"/", "", "///"} {
		fake := storetest.NewFake()
		resp, _ := newHandler(fake).Handle(context.Background(), request("GET", path))
		if resp.StatusCode != 301 || resp.Headers["Location"] != defaultURL {
			t.Errorf("%q: got %d %v", path, resp.StatusCode, resp.Headers)
		}
		if fake.Calls["GetLink"] != 0 || len(fake.Clicks) != 0 {
			t.Errorf("%q: lookups=%d clicks=%d", path, fake.Calls["GetLink"], len(fake.Clicks))
		}
	}
}

func TestRedirectTrackingFailureStillRedirects(t *testing.T) {
	fake := storetest.NewFake(promo(host))
	fake.Errors = map[string]error{"PutClick": errors.New("boom")}
	resp, _ := newHandler(fake).Handle(context.Background(), request("GET", "/promo"))
	if resp.StatusCode != 301 || resp.Headers["Location"] != "https://go.example.com/target" {
		t.Fatalf("got %d %v", resp.StatusCode, resp.Headers)
	}
}

func TestRedirectLookupFailureIs502(t *testing.T) {
	fake := storetest.NewFake()
	fake.Errors = map[string]error{"GetLink": errors.New("boom")}
	resp, _ := newHandler(fake).Handle(context.Background(), request("GET", "/promo"))
	if resp.StatusCode != 502 || resp.Body != "Service temporarily unavailable" ||
		resp.Headers["Content-Type"] != "text/plain" {
		t.Fatalf("got %d %v %q", resp.StatusCode, resp.Headers, resp.Body)
	}
}

func TestAPIPaths(t *testing.T) {
	cors := map[string]string{
		"Access-Control-Allow-Origin":  adminOrigin,
		"Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
		"Access-Control-Allow-Headers": "Content-Type, Authorization",
		"Access-Control-Max-Age":       "3600",
	}

	t.Run("preflight", func(t *testing.T) {
		fake := storetest.NewFake()
		resp, _ := newHandler(fake).Handle(context.Background(), request("OPTIONS", "/api/unknown"))
		if resp.StatusCode != 200 || resp.Body != "" {
			t.Fatalf("got %d %q", resp.StatusCode, resp.Body)
		}
		assertHeaders(t, resp.Headers, cors)
		if fake.Calls["GetLink"] != 0 {
			t.Error("api path was looked up")
		}
	})

	t.Run("not found", func(t *testing.T) {
		resp, _ := newHandler(storetest.NewFake()).Handle(context.Background(), request("GET", "/apistuff"))
		if resp.StatusCode != 404 || resp.Body != `{"error": "Not found"}` {
			t.Fatalf("got %d %q", resp.StatusCode, resp.Body)
		}
		want := map[string]string{"Content-Type": "application/json"}
		for k, v := range cors {
			want[k] = v
		}
		assertHeaders(t, resp.Headers, want)
	})
}

func assertHeaders(t *testing.T, got, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("headers = %v, want %v", got, want)
		return
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("header %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestRedirectResolvesDomainFromHost(t *testing.T) {
	tests := []struct {
		name       string
		domainName string
		headers    map[string]string
		want       string
	}{
		{"domain name", "go.example.com", nil, "go.example.com"},
		{"domain name uppercase", "GO.Example.COM", nil, "go.example.com"},
		{"domain name wins over host", "go.example.com", map[string]string{"host": "other.example"}, "go.example.com"},
		{"host header", "", map[string]string{"host": "go.example.com"}, "go.example.com"},
		{"host header with port", "", map[string]string{"host": "go.example.com:443"}, "go.example.com"},
		{"host header uppercase with port", "", map[string]string{"Host": "GO.EXAMPLE.COM:8443"}, "go.example.com"},
		{"host header trailing dot", "", map[string]string{"host": "go.example.com."}, "go.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := storetest.NewFake(promo("go.example.com"), promo("other.example"))
			req := request("GET", "/promo")
			req.RequestContext.DomainName = tt.domainName
			req.Headers = tt.headers
			resp, _ := newHandler(fake).Handle(context.Background(), req)
			if want := "https://" + tt.want + "/target"; resp.StatusCode != 301 || resp.Headers["Location"] != want {
				t.Fatalf("got %d %v, want 301 to %s", resp.StatusCode, resp.Headers, want)
			}
			if len(fake.Clicks) != 1 || fake.Clicks[0].Domain != tt.want {
				t.Errorf("clicks = %+v", fake.Clicks)
			}
		})
	}
}

func TestRedirectMissingHostUsesFallbackWithoutLookup(t *testing.T) {
	fake := storetest.NewFake(promo(host))
	req := request("GET", "/promo")
	req.RequestContext.DomainName = ""
	req.Headers = map[string]string{"host": "  "}
	resp, _ := newHandler(fake).Handle(context.Background(), req)
	if resp.StatusCode != 301 || resp.Headers["Location"] != defaultURL {
		t.Fatalf("got %d %v", resp.StatusCode, resp.Headers)
	}
	if fake.Calls["GetLink"] != 0 || len(fake.Clicks) != 0 {
		t.Errorf("lookups=%d clicks=%d", fake.Calls["GetLink"], len(fake.Clicks))
	}
}

func TestRedirectSameSlugOnTwoDomains(t *testing.T) {
	fake := storetest.NewFake(promo("a.example"), promo("b.example"))
	for _, d := range []string{"a.example", "b.example"} {
		req := request("GET", "/promo")
		req.RequestContext.DomainName = d
		resp, _ := newHandler(fake).Handle(context.Background(), req)
		if want := "https://" + d + "/target"; resp.Headers["Location"] != want {
			t.Errorf("%s: Location = %q, want %q", d, resp.Headers["Location"], want)
		}
	}
	if len(fake.Clicks) != 2 || fake.Clicks[0].LinkKey != "a.example#promo" || fake.Clicks[1].LinkKey != "b.example#promo" {
		t.Errorf("clicks = %+v", fake.Clicks)
	}

	// A slug that exists only on another domain is a miss.
	req := request("GET", "/promo")
	req.RequestContext.DomainName = "c.example"
	resp, _ := newHandler(fake).Handle(context.Background(), req)
	if resp.Headers["Location"] != defaultURL || len(fake.Clicks) != 2 {
		t.Errorf("c.example: Location = %q, clicks = %d", resp.Headers["Location"], len(fake.Clicks))
	}
}

// onlyDomain serves one domain and reports every other as unknown.
type onlyDomain struct{ name, url string }

func (o onlyDomain) Fallback(d string) (string, bool) {
	if d == o.name {
		return o.url, true
	}
	return "", false
}

func TestRedirectUsesFallbackOfTheDomain(t *testing.T) {
	h := newHandler(storetest.NewFake())
	h.Domains = onlyDomain{name: host, url: "https://go.example.com/landing"}
	for _, path := range []string{"/", "/unknown"} {
		resp, _ := h.Handle(context.Background(), request("GET", path))
		if resp.StatusCode != 301 || resp.Headers["Location"] != "https://go.example.com/landing" {
			t.Errorf("%s: got %d %v", path, resp.StatusCode, resp.Headers)
		}
	}
}

func TestRedirectUnknownDomainIs404WithoutLookup(t *testing.T) {
	fake := storetest.NewFake(promo("other.example"))
	h := newHandler(fake)
	h.Domains = onlyDomain{name: host, url: defaultURL}
	req := request("GET", "/promo")
	req.RequestContext.DomainName = "other.example"
	resp, _ := h.Handle(context.Background(), req)
	if resp.StatusCode != 404 || resp.Body != "Not found" || resp.Headers["Content-Type"] != "text/plain" {
		t.Fatalf("got %d %v %q", resp.StatusCode, resp.Headers, resp.Body)
	}
	if fake.Calls["GetLink"] != 0 || len(fake.Clicks) != 0 {
		t.Errorf("lookups=%d clicks=%d", fake.Calls["GetLink"], len(fake.Clicks))
	}
}
