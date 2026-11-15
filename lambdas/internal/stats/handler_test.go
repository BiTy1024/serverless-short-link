package stats

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store/storetest"
)

var fixedNow = time.Date(2026, 3, 10, 12, 0, 0, 500000000, time.UTC)

func newHandler(s Store) *Handler {
	return &Handler{Store: s, Now: func() time.Time { return fixedNow }}
}

func ptr(s string) *string { return &s }

const (
	domA = "a.example"
	domB = "b.example"
)

// click builds a click on domA; at builds one on another domain.
func click(slug, ts, target string) store.Click {
	return at(domA, slug, ts, target)
}

func at(domain, slug, ts, target string) store.Click {
	var t *string
	if target != "" {
		t = ptr(target)
	}
	return store.NewClick(domain, slug, ts, t)
}

// request builds a request; path is "<domain>/<slug>" for the detail route, or "".
func request(routeKey, path string, query map[string]string) events.APIGatewayV2HTTPRequest {
	req := events.APIGatewayV2HTTPRequest{RouteKey: routeKey, QueryStringParameters: query}
	if path != "" {
		d, slug, _ := strings.Cut(path, "/")
		req.PathParameters = map[string]string{"domain": d, "path": slug}
	}
	return req
}

func call(t *testing.T, s Store, req events.APIGatewayV2HTTPRequest) events.APIGatewayV2HTTPResponse {
	t.Helper()
	resp, err := newHandler(s).Handle(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q", resp.Headers["Content-Type"])
	}
	return resp
}

func wantBody(t *testing.T, resp events.APIGatewayV2HTTPResponse, status int, body string) {
	t.Helper()
	if resp.StatusCode != status || resp.Body != body {
		t.Errorf("got %d %s\nwant %d %s", resp.StatusCode, resp.Body, status, body)
	}
}

func TestUnknownRoute(t *testing.T) {
	// The single-domain detail route no longer exists.
	req := events.APIGatewayV2HTTPRequest{RouteKey: "GET /api/stats/{path}", PathParameters: map[string]string{"path": "promo"}}
	resp := call(t, storetest.NewFake(), req)
	wantBody(t, resp, 404, `{"error":"Route not found"}`)
}

func overviewFixture() *storetest.Fake {
	fake := storetest.NewFake(store.Link{Domain: domA, Slug: "b"}, store.Link{Domain: domA, Slug: "c"})
	fake.Clicks = []store.Click{
		click("a", "2026-03-02T00:00:00+00:00", "https://a.example/1"),
		click("b", "2026-03-05T00:00:00+00:00", "https://b.example"),
		click("c", "2026-03-01T00:00:00+00:00", "https://c.example"),
		click("a", "2026-03-01T00:00:00+00:00", "https://a.example/2"),
		click("c", "2026-03-03T00:00:00+00:00", ""),
		click("b", "2026-03-04T00:00:00+00:00", "https://b.example"),
		click("b", "2026-03-06T00:00:00+00:00", "https://b.example"),
	}
	return fake
}

func TestOverview(t *testing.T) {
	resp := call(t, overviewFixture(), request("GET /api/stats", "", nil))
	// Sorted by clicks; /a and /c tie and keep first-seen order. target_url
	// is the one of the last click seen, null when that click has none.
	wantBody(t, resp, 200, `{"stats":[`+
		`{"domain":"a.example","slug":"b","clicks":3,"target_url":"https://b.example","first_click":"2026-03-04T00:00:00+00:00","last_click":"2026-03-06T00:00:00+00:00"},`+
		`{"domain":"a.example","slug":"a","clicks":2,"target_url":"https://a.example/2","first_click":"2026-03-01T00:00:00+00:00","last_click":"2026-03-02T00:00:00+00:00"},`+
		`{"domain":"a.example","slug":"c","clicks":2,"target_url":null,"first_click":"2026-03-01T00:00:00+00:00","last_click":"2026-03-03T00:00:00+00:00"}`+
		`],"total_clicks":7}`)
}

func TestOverviewLinkedOnly(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "True"} {
		resp := call(t, overviewFixture(), request("GET /api/stats", "", map[string]string{"linked_only": v}))
		wantBody(t, resp, 200, `{"stats":[`+
			`{"domain":"a.example","slug":"b","clicks":3,"target_url":"https://b.example","first_click":"2026-03-04T00:00:00+00:00","last_click":"2026-03-06T00:00:00+00:00"},`+
			`{"domain":"a.example","slug":"c","clicks":2,"target_url":null,"first_click":"2026-03-01T00:00:00+00:00","last_click":"2026-03-03T00:00:00+00:00"}`+
			`],"total_clicks":5}`)
	}
	resp := call(t, overviewFixture(), request("GET /api/stats", "", map[string]string{"linked_only": "yes"}))
	if resp.StatusCode != 200 || !strings.Contains(resp.Body, `"slug":"a"`) {
		t.Errorf("linked_only=yes filtered: %s", resp.Body)
	}
}

func TestOverviewEmpty(t *testing.T) {
	resp := call(t, storetest.NewFake(), request("GET /api/stats", "", nil))
	wantBody(t, resp, 200, `{"stats":[],"total_clicks":0}`)
}

func TestOverviewStoreFailure(t *testing.T) {
	fake := overviewFixture()
	fake.Errors = map[string]error{"ListLinkKeys": errors.New("boom")}
	resp := call(t, fake, request("GET /api/stats", "", map[string]string{"linked_only": "true"}))
	wantBody(t, resp, 500, `{"error":"Internal server error"}`)
}

func TestDetail(t *testing.T) {
	fake := storetest.NewFake()
	for i := range 25 {
		fake.Clicks = append(fake.Clicks, click("promo", fmt.Sprintf("2026-03-%02dT00:00:00+00:00", i+1), fmt.Sprintf("https://t.example/%d", i+1)))
	}
	fake.Clicks = append(fake.Clicks, click("other", "2026-03-09T00:00:00+00:00", "https://o.example"))

	resp := call(t, fake, request(routeDetail, "a.example/promo", nil))
	recent := ""
	for day := 25; day > 5; day-- {
		if recent != "" {
			recent += ","
		}
		recent += fmt.Sprintf(`{"timestamp":"2026-03-%02dT00:00:00+00:00"}`, day)
	}
	// target_url comes from the oldest click in range.
	wantBody(t, resp, 200, `{"domain":"a.example","slug":"promo","clicks":25,"target_url":"https://t.example/1","recent_clicks":[`+recent+`]}`)
	if fake.LastRange != (store.TimeRange{}) {
		t.Errorf("range = %+v, want open", fake.LastRange)
	}
}

func TestDetailNoClicks(t *testing.T) {
	resp := call(t, storetest.NewFake(), request(routeDetail, "a.example/promo", nil))
	wantBody(t, resp, 200, `{"domain":"a.example","slug":"promo","clicks":0,"target_url":null,"recent_clicks":[]}`)
}

func TestDetailTimeFilters(t *testing.T) {
	tests := []struct {
		name  string
		query map[string]string
		want  store.TimeRange
	}{
		{"from and to", map[string]string{"from": "2026-03-01", "to": "2026-03-05"},
			store.TimeRange{Start: "2026-03-01T00:00:00+00:00", End: "2026-03-05T23:59:59+00:00"}},
		{"from only", map[string]string{"from": "2026-03-01"}, store.TimeRange{Start: "2026-03-01T00:00:00+00:00"}},
		{"to only", map[string]string{"to": "2026-03-05"}, store.TimeRange{End: "2026-03-05T23:59:59+00:00"}},
		{"from wins over days", map[string]string{"from": "2026-03-01", "days": "x"}, store.TimeRange{Start: "2026-03-01T00:00:00+00:00"}},
		{"days", map[string]string{"days": "7"}, store.TimeRange{Start: "2026-03-03T12:00:00.500000+00:00"}},
		{"days with spaces and sign", map[string]string{"days": " +7 "}, store.TimeRange{Start: "2026-03-03T12:00:00.500000+00:00"}},
		{"days with underscore", map[string]string{"days": "1_0"}, store.TimeRange{Start: "2026-02-28T12:00:00.500000+00:00"}},
		{"negative days", map[string]string{"days": "-1"}, store.TimeRange{Start: "2026-03-11T12:00:00.500000+00:00"}},
		{"empty values are ignored", map[string]string{"from": "", "to": "", "days": ""}, store.TimeRange{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := storetest.NewFake()
			resp := call(t, fake, request(routeDetail, "a.example/promo", tt.query))
			if resp.StatusCode != 200 {
				t.Fatalf("status %d, body %s", resp.StatusCode, resp.Body)
			}
			if fake.LastRange != tt.want {
				t.Errorf("range = %+v, want %+v", fake.LastRange, tt.want)
			}
		})
	}
}

func TestDetailFiltersByRange(t *testing.T) {
	fake := storetest.NewFake()
	fake.Clicks = []store.Click{
		click("promo", "2026-02-28T23:59:59+00:00", "https://t.example/old"),
		click("promo", "2026-03-01T00:00:00+00:00", "https://t.example/in"),
		click("promo", "2026-03-02T10:00:00+00:00", "https://t.example/in2"),
	}
	resp := call(t, fake, request(routeDetail, "a.example/promo", map[string]string{"from": "2026-03-01", "to": "2026-03-02"}))
	wantBody(t, resp, 200, `{"domain":"a.example","slug":"promo","clicks":2,"target_url":"https://t.example/in","recent_clicks":[`+
		`{"timestamp":"2026-03-02T10:00:00+00:00"},{"timestamp":"2026-03-01T00:00:00+00:00"}]}`)
}

func TestDetailInvalidDays(t *testing.T) {
	const msg = `{"error":"Invalid time filter. Use days=N or from=YYYY-MM-DD&to=YYYY-MM-DD"}`
	for _, days := range []string{"x", "1.5", "7d", "_1", "1_", "1__0", "+-1", "--1", "+"} {
		resp := call(t, storetest.NewFake(), request(routeDetail, "a.example/promo", map[string]string{"days": days}))
		if resp.StatusCode != 400 || resp.Body != msg {
			t.Errorf("days=%q: got %d %s", days, resp.StatusCode, resp.Body)
		}
	}
}

func TestDetailDaysOutOfRange(t *testing.T) {
	for _, days := range []string{"1000000000", "99999999999999999999", "800000"} {
		resp := call(t, storetest.NewFake(), request(routeDetail, "a.example/promo", map[string]string{"days": days}))
		wantBody(t, resp, 500, `{"error":"Internal server error"}`)
	}
}

func TestDetailStoreFailure(t *testing.T) {
	fake := storetest.NewFake()
	fake.Errors = map[string]error{"QueryLinkClicks": errors.New("boom")}
	resp := call(t, fake, request(routeDetail, "a.example/promo", nil))
	wantBody(t, resp, 500, `{"error":"Internal server error"}`)
}

func twoDomainFixture() *storetest.Fake {
	fake := storetest.NewFake(store.Link{Domain: domA, Slug: "promo"}, store.Link{Domain: domB, Slug: "promo"})
	fake.Clicks = []store.Click{
		at(domA, "promo", "2026-03-01T00:00:00+00:00", "https://a.example/t"),
		at(domB, "promo", "2026-03-02T00:00:00+00:00", "https://b.example/t"),
		at(domB, "promo", "2026-03-08T00:00:00+00:00", "https://b.example/t"),
		at(domB, "gone", "2026-03-09T00:00:00+00:00", "https://b.example/g"),
	}
	return fake
}

func TestOverviewAllDomainsScans(t *testing.T) {
	fake := twoDomainFixture()
	resp := call(t, fake, request("GET /api/stats", "", nil))
	// The same slug on two domains is counted per domain.
	wantBody(t, resp, 200, `{"stats":[`+
		`{"domain":"b.example","slug":"promo","clicks":2,"target_url":"https://b.example/t","first_click":"2026-03-02T00:00:00+00:00","last_click":"2026-03-08T00:00:00+00:00"},`+
		`{"domain":"a.example","slug":"promo","clicks":1,"target_url":"https://a.example/t","first_click":"2026-03-01T00:00:00+00:00","last_click":"2026-03-01T00:00:00+00:00"},`+
		`{"domain":"b.example","slug":"gone","clicks":1,"target_url":"https://b.example/g","first_click":"2026-03-09T00:00:00+00:00","last_click":"2026-03-09T00:00:00+00:00"}`+
		`],"total_clicks":4}`)
	if fake.Calls["ScanClicks"] != 1 || fake.Calls["QueryDomainClicks"] != 0 {
		t.Errorf("calls = %v", fake.Calls)
	}
}

func TestOverviewOneDomainQueriesIndex(t *testing.T) {
	for _, d := range []string{"b.example", " B.Example "} {
		fake := twoDomainFixture()
		resp := call(t, fake, request("GET /api/stats", "", map[string]string{"domain": d, "linked_only": "true"}))
		wantBody(t, resp, 200, `{"stats":[`+
			`{"domain":"b.example","slug":"promo","clicks":2,"target_url":"https://b.example/t","first_click":"2026-03-02T00:00:00+00:00","last_click":"2026-03-08T00:00:00+00:00"}`+
			`],"total_clicks":2}`)
		if fake.Calls["QueryDomainClicks"] != 1 || fake.Calls["ScanClicks"] != 0 {
			t.Errorf("calls = %v", fake.Calls)
		}
		if fake.LastDomain != domB {
			t.Errorf("domain = %q", fake.LastDomain)
		}
	}
}

func TestOverviewTimeFilter(t *testing.T) {
	fake := twoDomainFixture()
	resp := call(t, fake, request("GET /api/stats", "", map[string]string{"domain": domB, "days": "3"}))
	wantBody(t, resp, 200, `{"stats":[`+
		`{"domain":"b.example","slug":"promo","clicks":1,"target_url":"https://b.example/t","first_click":"2026-03-08T00:00:00+00:00","last_click":"2026-03-08T00:00:00+00:00"},`+
		`{"domain":"b.example","slug":"gone","clicks":1,"target_url":"https://b.example/g","first_click":"2026-03-09T00:00:00+00:00","last_click":"2026-03-09T00:00:00+00:00"}`+
		`],"total_clicks":2}`)
	if want := (store.TimeRange{Start: "2026-03-07T12:00:00.500000+00:00"}); fake.LastRange != want {
		t.Errorf("range = %+v, want %+v", fake.LastRange, want)
	}

	fake = twoDomainFixture()
	call(t, fake, request("GET /api/stats", "", map[string]string{"from": "2026-03-02", "to": "2026-03-08"}))
	if want := (store.TimeRange{Start: "2026-03-02T00:00:00+00:00", End: "2026-03-08T23:59:59+00:00"}); fake.LastRange != want {
		t.Errorf("scan range = %+v, want %+v", fake.LastRange, want)
	}

	resp = call(t, twoDomainFixture(), request("GET /api/stats", "", map[string]string{"days": "x"}))
	wantBody(t, resp, 400, `{"error":"Invalid time filter. Use days=N or from=YYYY-MM-DD&to=YYYY-MM-DD"}`)
}

func TestOverviewInvalidDomain(t *testing.T) {
	for _, d := range []string{"localhost", "b.example:443", "bad_domain.example"} {
		fake := twoDomainFixture()
		resp := call(t, fake, request("GET /api/stats", "", map[string]string{"domain": d}))
		if resp.StatusCode != 400 || !strings.Contains(resp.Body, "is not a valid hostname") {
			t.Errorf("%q: got %d %s", d, resp.StatusCode, resp.Body)
		}
		if len(fake.Calls) != 0 {
			t.Errorf("%q: store was called: %v", d, fake.Calls)
		}
	}
}

func TestDetailSameSlugOnTwoDomains(t *testing.T) {
	resp := call(t, twoDomainFixture(), request(routeDetail, "a.example/promo", nil))
	wantBody(t, resp, 200, `{"domain":"a.example","slug":"promo","clicks":1,"target_url":"https://a.example/t","recent_clicks":[`+
		`{"timestamp":"2026-03-01T00:00:00+00:00"}]}`)
	resp = call(t, twoDomainFixture(), request(routeDetail, "B.EXAMPLE/promo", nil))
	wantBody(t, resp, 200, `{"domain":"b.example","slug":"promo","clicks":2,"target_url":"https://b.example/t","recent_clicks":[`+
		`{"timestamp":"2026-03-08T00:00:00+00:00"},{"timestamp":"2026-03-02T00:00:00+00:00"}]}`)
}

func TestDetailInvalidDomain(t *testing.T) {
	fake := twoDomainFixture()
	resp := call(t, fake, request(routeDetail, "/promo", nil))
	wantBody(t, resp, 400, `{"error":"Domain is required"}`)
	resp = call(t, fake, request(routeDetail, "a.example:8443/promo", nil))
	wantBody(t, resp, 400, `{"error":"Domain 'a.example:8443' is not a valid hostname"}`)
	if len(fake.Calls) != 0 {
		t.Errorf("store was called: %v", fake.Calls)
	}
}
