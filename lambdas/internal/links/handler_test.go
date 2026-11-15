package links

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store/storetest"
)

var fixedNow = time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)

const nowStamp = "2026-03-04T05:06:07.123456+00:00"

var existing = store.Link{
	ShortPath: "promo",
	TargetURL: "https://example.com/promo",
	CreatedAt: "2026-01-01T00:00:00.000001+00:00",
	UpdatedAt: "2026-01-01T00:00:00.000001+00:00",
}

func newHandler(s Store) *Handler {
	return &Handler{Store: s, Now: func() time.Time { return fixedNow }}
}

func request(routeKey, path, body, groups string) events.APIGatewayV2HTTPRequest {
	req := events.APIGatewayV2HTTPRequest{RouteKey: routeKey, Body: body}
	if path != "" {
		req.PathParameters = map[string]string{"path": path}
	}
	if groups != "" {
		req.RequestContext.Authorizer = &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
			JWT: &events.APIGatewayV2HTTPRequestContextAuthorizerJWTDescription{
				Claims: map[string]string{"cognito:groups": groups},
			},
		}
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

func wantError(t *testing.T, resp events.APIGatewayV2HTTPResponse, status int, msg string) {
	t.Helper()
	if resp.StatusCode != status {
		t.Errorf("status = %d, want %d (body %s)", resp.StatusCode, status, resp.Body)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		t.Fatalf("body %q: %v", resp.Body, err)
	}
	if len(body) != 1 || body["error"] != msg {
		t.Errorf("body = %v, want error %q", body, msg)
	}
}

func TestUnknownRoute(t *testing.T) {
	resp := call(t, storetest.NewFake(), request("PATCH /api/links/{path}", "x", "", ""))
	wantError(t, resp, 404, "Route not found")
}

func TestList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		resp := call(t, storetest.NewFake(), request("GET /api/links", "", "", "[viewer]"))
		if resp.StatusCode != 200 || resp.Body != `{"links":[],"count":0}` {
			t.Fatalf("got %d %s", resp.StatusCode, resp.Body)
		}
	})
	t.Run("viewer sees links", func(t *testing.T) {
		resp := call(t, storetest.NewFake(existing), request("GET /api/links", "", "", "[viewer]"))
		want := `{"links":[{"short_path":"promo","target_url":"https://example.com/promo",` +
			`"created_at":"2026-01-01T00:00:00.000001+00:00","updated_at":"2026-01-01T00:00:00.000001+00:00"}],"count":1}`
		if resp.StatusCode != 200 || resp.Body != want {
			t.Fatalf("got %d %s", resp.StatusCode, resp.Body)
		}
	})
	t.Run("store failure", func(t *testing.T) {
		fake := storetest.NewFake()
		fake.Errors = map[string]error{"ListLinks": errors.New("boom")}
		wantError(t, call(t, fake, request("GET /api/links", "", "", "")), 500, "Internal server error")
	})
}

func TestWritesRequireAdmin(t *testing.T) {
	routes := []struct{ key, path, body string }{
		{"POST /api/links", "", `{"short_path":"new","target_url":"https://example.com/x"}`},
		{"PUT /api/links/{path}", "promo", `{"target_url":"https://example.com/x"}`},
		{"DELETE /api/links/{path}", "promo", ""},
	}
	for _, groups := range []string{"", "[]", "[viewer]", "[admins]", "[viewer reader]", `["viewer"]`} {
		for _, r := range routes {
			fake := storetest.NewFake(existing)
			resp := call(t, fake, request(r.key, r.path, r.body, groups))
			wantError(t, resp, 403, "Admin access required")
			if links := fake.Links(); len(links) != 1 || links[0] != existing {
				t.Errorf("%s with %q changed the store: %+v", r.key, groups, links)
			}
		}
	}
}

func TestUserGroups(t *testing.T) {
	tests := map[string][]string{
		"admin":              {"admin"},
		"[admin]":            {"admin"},
		"[admin viewer]":     {"admin", "viewer"},
		"[viewer admin]":     {"viewer", "admin"},
		" [admin  viewer] ":  {"admin", "viewer"},
		"[viewer,admin]":     {"viewer", "admin"},
		"[viewer, admin]":    {"viewer", "admin"},
		`["admin","viewer"]`: {"admin", "viewer"},
		`["admin viewer"]`:   {"admin viewer"},
		"[]":                 {},
		"[adminviewer]":      {"adminviewer"},
	}
	for claim, want := range tests {
		got := userGroups(request("GET /api/links", "", "", claim))
		if strings.Join(got, "|") != strings.Join(want, "|") || len(got) != len(want) {
			t.Errorf("%q: got %q, want %q", claim, got, want)
		}
	}
	if got := userGroups(request("GET /api/links", "", "", "")); got != nil {
		t.Errorf("no claim: got %q", got)
	}
}

func TestAdminInSeveralGroupsMayWrite(t *testing.T) {
	for _, groups := range []string{"[admin viewer]", "[viewer admin]", `["viewer","admin"]`, "admin"} {
		fake := storetest.NewFake(existing)
		resp := call(t, fake, request("DELETE /api/links/{path}", "promo", "", groups))
		if resp.StatusCode != 204 {
			t.Errorf("%q: status %d, body %s", groups, resp.StatusCode, resp.Body)
		}
	}
}

func TestCreate(t *testing.T) {
	fake := storetest.NewFake()
	resp := call(t, fake, request("POST /api/links", "", `{"short_path":"  new-link ","target_url":" https://example.com/a?b=1&c=2 "}`, "[admin]"))
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, body %s", resp.StatusCode, resp.Body)
	}
	want := store.Link{ShortPath: "new-link", TargetURL: "https://example.com/a?b=1&c=2", CreatedAt: nowStamp, UpdatedAt: nowStamp}
	var got store.Link
	if err := json.Unmarshal([]byte(resp.Body), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("response = %+v, want %+v", got, want)
	}
	if links := fake.Links(); len(links) != 1 || links[0] != want {
		t.Errorf("stored = %+v", links)
	}
}

func TestCreateDuplicate(t *testing.T) {
	fake := storetest.NewFake(existing)
	resp := call(t, fake, request("POST /api/links", "", `{"short_path":"promo","target_url":"https://example.com/other"}`, "[admin]"))
	wantError(t, resp, 409, "Path 'promo' already exists")
	if fake.Links()[0] != existing {
		t.Error("existing link was overwritten")
	}
}

func TestCreateValidation(t *testing.T) {
	const pathRule = "Path must contain only lowercase letters, numbers, and hyphens. Must start and end with a letter or number."
	tests := []struct {
		name, body string
		status     int
		msg        string
	}{
		{"invalid json", `{"short_path":`, 400, "Invalid JSON body"},
		{"empty body", ``, 400, "Path is required"},
		{"missing path", `{"target_url":"https://example.com"}`, 400, "Path is required"},
		{"blank path", `{"short_path":"   ","target_url":"https://example.com"}`, 400, "Path is required"},
		{"reserved", `{"short_path":"admin","target_url":"https://example.com"}`, 400, "Path 'admin' is reserved and cannot be used as a short link"},
		{"reserved api", `{"short_path":"api","target_url":"https://example.com"}`, 400, "Path 'api' is reserved and cannot be used as a short link"},
		{"too long", `{"short_path":"` + strings.Repeat("a", 65) + `","target_url":"https://example.com"}`, 400, "Path must be at most 64 characters"},
		{"double hyphen", `{"short_path":"a--b","target_url":"https://example.com"}`, 400, "Path must not contain consecutive hyphens"},
		{"uppercase", `{"short_path":"Promo","target_url":"https://example.com"}`, 400, pathRule},
		{"leading hyphen", `{"short_path":"-promo","target_url":"https://example.com"}`, 400, pathRule},
		{"trailing hyphen", `{"short_path":"promo-","target_url":"https://example.com"}`, 400, pathRule},
		{"slash", `{"short_path":"a/b","target_url":"https://example.com"}`, 400, pathRule},
		{"non-ascii", `{"short_path":"über","target_url":"https://example.com"}`, 400, pathRule},
		{"missing url", `{"short_path":"ok"}`, 400, "Target URL is required"},
		{"http url", `{"short_path":"ok","target_url":"http://example.com"}`, 400, "Target URL must start with https://"},
		{"short url", `{"short_path":"ok","target_url":"https://x.x"}`, 400, "Target URL must be a valid URL with a domain"},
		{"body is array", `[]`, 500, "Internal server error"},
		{"path is number", `{"short_path":1,"target_url":"https://example.com"}`, 500, "Internal server error"},
		{"path is null", `{"short_path":null,"target_url":"https://example.com"}`, 500, "Internal server error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := storetest.NewFake()
			resp := call(t, fake, request("POST /api/links", "", tt.body, "[admin]"))
			wantError(t, resp, tt.status, tt.msg)
			if len(fake.Links()) != 0 {
				t.Error("invalid link was stored")
			}
		})
	}
}

func TestCreateAcceptsBoundaryPaths(t *testing.T) {
	for _, p := range []string{"a", "0", "a-b", strings.Repeat("a", 64)} {
		resp := call(t, storetest.NewFake(), request("POST /api/links", "", `{"short_path":"`+p+`","target_url":"https://x.xx"}`, "[admin]"))
		if resp.StatusCode != 201 {
			t.Errorf("%q: status %d, body %s", p, resp.StatusCode, resp.Body)
		}
	}
}

func TestUpdate(t *testing.T) {
	fake := storetest.NewFake(existing)
	resp := call(t, fake, request("PUT /api/links/{path}", "promo", `{"target_url":"https://example.com/new"}`, "[admin]"))
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, body %s", resp.StatusCode, resp.Body)
	}
	want := existing
	want.TargetURL = "https://example.com/new"
	want.UpdatedAt = nowStamp
	var got store.Link
	if err := json.Unmarshal([]byte(resp.Body), &got); err != nil {
		t.Fatal(err)
	}
	if got != want || fake.Links()[0] != want {
		t.Errorf("response %+v, stored %+v, want %+v", got, fake.Links()[0], want)
	}
}

func TestUpdateErrors(t *testing.T) {
	t.Run("missing link", func(t *testing.T) {
		resp := call(t, storetest.NewFake(), request("PUT /api/links/{path}", "nope", `{"target_url":"https://example.com/new"}`, "[admin]"))
		wantError(t, resp, 404, "Path 'nope' not found")
	})
	t.Run("invalid url", func(t *testing.T) {
		fake := storetest.NewFake(existing)
		resp := call(t, fake, request("PUT /api/links/{path}", "promo", `{"target_url":"ftp://example.com"}`, "[admin]"))
		wantError(t, resp, 400, "Target URL must start with https://")
		if fake.Links()[0] != existing {
			t.Error("link was changed")
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		resp := call(t, storetest.NewFake(existing), request("PUT /api/links/{path}", "promo", `nope`, "[admin]"))
		wantError(t, resp, 400, "Invalid JSON body")
	})
	t.Run("store failure", func(t *testing.T) {
		fake := storetest.NewFake(existing)
		fake.Errors = map[string]error{"UpdateLinkTarget": errors.New("boom")}
		resp := call(t, fake, request("PUT /api/links/{path}", "promo", `{"target_url":"https://example.com/new"}`, "[admin]"))
		wantError(t, resp, 500, "Internal server error")
	})
}

func TestDelete(t *testing.T) {
	fake := storetest.NewFake(existing)
	resp := call(t, fake, request("DELETE /api/links/{path}", "promo", "", "[admin]"))
	if resp.StatusCode != 204 || resp.Body != "" {
		t.Fatalf("got %d %q", resp.StatusCode, resp.Body)
	}
	if len(fake.Links()) != 0 {
		t.Error("link was not deleted")
	}

	resp = call(t, fake, request("DELETE /api/links/{path}", "promo", "", "[admin]"))
	wantError(t, resp, 404, "Path 'promo' not found")
}
