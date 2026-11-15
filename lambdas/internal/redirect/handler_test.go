package redirect

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store/storetest"
)

const (
	defaultURL  = "https://example.com/home"
	adminOrigin = "https://admin.short.example"
)

var fixedNow = time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)

func newHandler(s Store) *Handler {
	return &Handler{
		Store:              s,
		DefaultRedirectURL: defaultURL,
		AdminOrigin:        adminOrigin,
		Now:                func() time.Time { return fixedNow },
	}
}

func request(method, path string) events.APIGatewayV2HTTPRequest {
	var req events.APIGatewayV2HTTPRequest
	req.RawPath = path
	req.RequestContext.HTTP.Method = method
	return req
}

func TestRedirectHit(t *testing.T) {
	for _, path := range []string{"/promo", "/promo/", "//promo//"} {
		t.Run(path, func(t *testing.T) {
			fake := storetest.NewFake(store.Link{ShortPath: "promo", TargetURL: "https://example.com/promo"})
			resp, err := newHandler(fake).Handle(context.Background(), request("GET", path))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 301 {
				t.Fatalf("status = %d, want 301", resp.StatusCode)
			}
			if got := resp.Headers["Location"]; got != "https://example.com/promo" {
				t.Errorf("Location = %q", got)
			}
			if len(resp.Headers) != 1 || resp.Body != "" {
				t.Errorf("unexpected headers %v or body %q", resp.Headers, resp.Body)
			}
			if len(fake.Clicks) != 1 {
				t.Fatalf("clicks = %d, want 1", len(fake.Clicks))
			}
			c := fake.Clicks[0]
			if c.RedirectPath != "/promo" || c.Timestamp != "2026-03-04T05:06:07.123456+00:00" ||
				c.TargetURL == nil || *c.TargetURL != "https://example.com/promo" {
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
		if fake.GetLinkCalls != 0 || len(fake.Clicks) != 0 {
			t.Errorf("%q: lookups=%d clicks=%d", path, fake.GetLinkCalls, len(fake.Clicks))
		}
	}
}

func TestRedirectTrackingFailureStillRedirects(t *testing.T) {
	fake := storetest.NewFake(store.Link{ShortPath: "promo", TargetURL: "https://example.com/promo"})
	fake.Errors = map[string]error{"PutClick": errors.New("boom")}
	resp, _ := newHandler(fake).Handle(context.Background(), request("GET", "/promo"))
	if resp.StatusCode != 301 || resp.Headers["Location"] != "https://example.com/promo" {
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
		if fake.GetLinkCalls != 0 {
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
