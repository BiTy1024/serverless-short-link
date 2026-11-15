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

func click(path, ts, target string) store.Click {
	c := store.Click{RedirectPath: path, Timestamp: ts}
	if target != "" {
		c.TargetURL = ptr(target)
	}
	return c
}

func request(routeKey, path string, query map[string]string) events.APIGatewayV2HTTPRequest {
	req := events.APIGatewayV2HTTPRequest{RouteKey: routeKey, QueryStringParameters: query}
	if path != "" {
		req.PathParameters = map[string]string{"path": path}
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
	resp := call(t, storetest.NewFake(), request("GET /api/stats/{path}/more", "", nil))
	wantBody(t, resp, 404, `{"error":"Route not found"}`)
}

func overviewFixture() *storetest.Fake {
	fake := storetest.NewFake(store.Link{ShortPath: "b"}, store.Link{ShortPath: "c"})
	fake.Clicks = []store.Click{
		click("/a", "2026-03-02T00:00:00+00:00", "https://a.example/1"),
		click("/b", "2026-03-05T00:00:00+00:00", "https://b.example"),
		click("/c", "2026-03-01T00:00:00+00:00", "https://c.example"),
		click("/a", "2026-03-01T00:00:00+00:00", "https://a.example/2"),
		click("/c", "2026-03-03T00:00:00+00:00", ""),
		click("/b", "2026-03-04T00:00:00+00:00", "https://b.example"),
		click("/b", "2026-03-06T00:00:00+00:00", "https://b.example"),
	}
	return fake
}

func TestOverview(t *testing.T) {
	resp := call(t, overviewFixture(), request("GET /api/stats", "", nil))
	// Sorted by clicks; /a and /c tie and keep first-seen order. target_url
	// is the one of the last click seen, null when that click has none.
	wantBody(t, resp, 200, `{"stats":[`+
		`{"path":"/b","clicks":3,"target_url":"https://b.example","first_click":"2026-03-04T00:00:00+00:00","last_click":"2026-03-06T00:00:00+00:00"},`+
		`{"path":"/a","clicks":2,"target_url":"https://a.example/2","first_click":"2026-03-01T00:00:00+00:00","last_click":"2026-03-02T00:00:00+00:00"},`+
		`{"path":"/c","clicks":2,"target_url":null,"first_click":"2026-03-01T00:00:00+00:00","last_click":"2026-03-03T00:00:00+00:00"}`+
		`],"total_clicks":7}`)
}

func TestOverviewLinkedOnly(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "True"} {
		resp := call(t, overviewFixture(), request("GET /api/stats", "", map[string]string{"linked_only": v}))
		wantBody(t, resp, 200, `{"stats":[`+
			`{"path":"/b","clicks":3,"target_url":"https://b.example","first_click":"2026-03-04T00:00:00+00:00","last_click":"2026-03-06T00:00:00+00:00"},`+
			`{"path":"/c","clicks":2,"target_url":null,"first_click":"2026-03-01T00:00:00+00:00","last_click":"2026-03-03T00:00:00+00:00"}`+
			`],"total_clicks":5}`)
	}
	resp := call(t, overviewFixture(), request("GET /api/stats", "", map[string]string{"linked_only": "yes"}))
	if resp.StatusCode != 200 || !strings.Contains(resp.Body, `"path":"/a"`) {
		t.Errorf("linked_only=yes filtered: %s", resp.Body)
	}
}

func TestOverviewEmpty(t *testing.T) {
	resp := call(t, storetest.NewFake(), request("GET /api/stats", "", nil))
	wantBody(t, resp, 200, `{"stats":[],"total_clicks":0}`)
}

func TestOverviewStoreFailure(t *testing.T) {
	fake := overviewFixture()
	fake.Errors = map[string]error{"ListLinkPaths": errors.New("boom")}
	resp := call(t, fake, request("GET /api/stats", "", map[string]string{"linked_only": "true"}))
	wantBody(t, resp, 500, `{"error":"Internal server error"}`)
}

func TestDetail(t *testing.T) {
	fake := storetest.NewFake()
	for i := range 25 {
		fake.Clicks = append(fake.Clicks, click("/promo", fmt.Sprintf("2026-03-%02dT00:00:00+00:00", i+1), fmt.Sprintf("https://t.example/%d", i+1)))
	}
	fake.Clicks = append(fake.Clicks, click("/other", "2026-03-09T00:00:00+00:00", "https://o.example"))

	resp := call(t, fake, request("GET /api/stats/{path}", "promo", nil))
	recent := ""
	for day := 25; day > 5; day-- {
		if recent != "" {
			recent += ","
		}
		recent += fmt.Sprintf(`{"timestamp":"2026-03-%02dT00:00:00+00:00"}`, day)
	}
	// target_url comes from the oldest click in range.
	wantBody(t, resp, 200, `{"path":"/promo","clicks":25,"target_url":"https://t.example/1","recent_clicks":[`+recent+`]}`)
	if fake.LastRange != (store.TimeRange{}) {
		t.Errorf("range = %+v, want open", fake.LastRange)
	}
}

func TestDetailNoClicks(t *testing.T) {
	resp := call(t, storetest.NewFake(), request("GET /api/stats/{path}", "promo", nil))
	wantBody(t, resp, 200, `{"path":"/promo","clicks":0,"target_url":null,"recent_clicks":[]}`)
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
			resp := call(t, fake, request("GET /api/stats/{path}", "promo", tt.query))
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
		click("/promo", "2026-02-28T23:59:59+00:00", "https://t.example/old"),
		click("/promo", "2026-03-01T00:00:00+00:00", "https://t.example/in"),
		click("/promo", "2026-03-02T10:00:00+00:00", "https://t.example/in2"),
	}
	resp := call(t, fake, request("GET /api/stats/{path}", "promo", map[string]string{"from": "2026-03-01", "to": "2026-03-02"}))
	wantBody(t, resp, 200, `{"path":"/promo","clicks":2,"target_url":"https://t.example/in","recent_clicks":[`+
		`{"timestamp":"2026-03-02T10:00:00+00:00"},{"timestamp":"2026-03-01T00:00:00+00:00"}]}`)
}

func TestDetailInvalidDays(t *testing.T) {
	const msg = `{"error":"Invalid time filter. Use days=N or from=YYYY-MM-DD&to=YYYY-MM-DD"}`
	for _, days := range []string{"x", "1.5", "7d", "_1", "1_", "1__0", "+-1", "--1", "+"} {
		resp := call(t, storetest.NewFake(), request("GET /api/stats/{path}", "promo", map[string]string{"days": days}))
		if resp.StatusCode != 400 || resp.Body != msg {
			t.Errorf("days=%q: got %d %s", days, resp.StatusCode, resp.Body)
		}
	}
}

func TestDetailDaysOutOfRange(t *testing.T) {
	for _, days := range []string{"1000000000", "99999999999999999999", "800000"} {
		resp := call(t, storetest.NewFake(), request("GET /api/stats/{path}", "promo", map[string]string{"days": days}))
		wantBody(t, resp, 500, `{"error":"Internal server error"}`)
	}
}

func TestDetailStoreFailure(t *testing.T) {
	fake := storetest.NewFake()
	fake.Errors = map[string]error{"QueryClicks": errors.New("boom")}
	resp := call(t, fake, request("GET /api/stats/{path}", "promo", nil))
	wantBody(t, resp, 500, `{"error":"Internal server error"}`)
}
