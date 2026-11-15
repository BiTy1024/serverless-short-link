package links

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings/settingstest"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store/storetest"
)

var fixedNow = time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)

const nowStamp = "2026-03-04T05:06:07.123456+00:00"

var existing = store.Link{
	Domain:    "go.example.com",
	Slug:      "promo",
	TargetURL: "https://example.com/promo",
	CreatedAt: "2026-01-01T00:00:00.000001+00:00",
	UpdatedAt: "2026-01-01T00:00:00.000001+00:00",
}

func newHandler(s Store) *Handler {
	return &Handler{Store: s, Domains: anyDomain{}, Now: func() time.Time { return fixedNow }}
}

// anyDomain treats every domain as configured.
type anyDomain struct{}

func (anyDomain) Exists(context.Context, string) (bool, error) { return true, nil }

// request builds a request; path is "<domain>/<slug>" for the routes that
// take both, or "".
func request(routeKey, path, body, groups string) events.APIGatewayV2HTTPRequest {
	req := events.APIGatewayV2HTTPRequest{RouteKey: routeKey, Body: body}
	if path != "" {
		d, slug, _ := strings.Cut(path, "/")
		req.PathParameters = map[string]string{"domain": d, "path": slug}
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
	resp := call(t, storetest.NewFake(), request("PUT /api/links/{path}", "x", "", "[admin]"))
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
		want := `{"links":[{"domain":"go.example.com","slug":"promo","target_url":"https://example.com/promo",` +
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
		{"POST /api/links", "", `{"domain":"go.example.com","slug":"new","target_url":"https://example.com/x"}`},
		{routeUpdate, "go.example.com/promo", `{"target_url":"https://example.com/x"}`},
		{routeDelete, "go.example.com/promo", ""},
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
		resp := call(t, fake, request(routeDelete, "go.example.com/promo", "", groups))
		if resp.StatusCode != 204 {
			t.Errorf("%q: status %d, body %s", groups, resp.StatusCode, resp.Body)
		}
	}
}

func TestCreate(t *testing.T) {
	fake := storetest.NewFake()
	resp := call(t, fake, request("POST /api/links", "", `{"domain":" Go.Example.COM ","slug":"  new-link ","target_url":" https://example.com/a?b=1&c=2 "}`, "[admin]"))
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, body %s", resp.StatusCode, resp.Body)
	}
	want := store.Link{Domain: "go.example.com", Slug: "new-link", TargetURL: "https://example.com/a?b=1&c=2", CreatedAt: nowStamp, UpdatedAt: nowStamp}
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
	resp := call(t, fake, request("POST /api/links", "", `{"domain":"go.example.com","slug":"promo","target_url":"https://example.com/other"}`, "[admin]"))
	wantError(t, resp, 409, "Slug 'promo' already exists on go.example.com")
	if fake.Links()[0] != existing {
		t.Error("existing link was overwritten")
	}
}

func TestCreateValidation(t *testing.T) {
	const slugRule = "Slug must contain only lowercase letters, numbers, and hyphens. Must start and end with a letter or number."
	tests := []struct {
		name, body string
		status     int
		msg        string
	}{
		{"invalid json", `{"slug":`, 400, "Invalid JSON body"},
		{"empty body", ``, 400, "Domain is required"},
		{"missing domain", `{"slug":"ok","target_url":"https://example.com"}`, 400, "Domain is required"},
		{"blank domain", `{"domain":"  ","slug":"ok","target_url":"https://example.com"}`, 400, "Domain is required"},
		{"domain with port", `{"domain":"go.example.com:443","slug":"ok","target_url":"https://example.com"}`, 400, "Domain 'go.example.com:443' is not a valid hostname"},
		{"domain with scheme", `{"domain":"https://go.example.com","slug":"ok","target_url":"https://example.com"}`, 400, "Domain 'https://go.example.com' is not a valid hostname"},
		{"single label domain", `{"domain":"localhost","slug":"ok","target_url":"https://example.com"}`, 400, "Domain 'localhost' is not a valid hostname"},
		{"ip domain", `{"domain":"127.0.0.1","slug":"ok","target_url":"https://example.com"}`, 400, "Domain '127.0.0.1' is not a valid hostname"},
		{"domain with path", `{"domain":"go.example.com/x","slug":"ok","target_url":"https://example.com"}`, 400, "Domain 'go.example.com/x' is not a valid hostname"},
		{"domain is number", `{"domain":1,"slug":"ok","target_url":"https://example.com"}`, 500, "Internal server error"},
		{"missing slug", `{"domain":"go.example.com","target_url":"https://example.com"}`, 400, "Slug is required"},
		{"blank slug", `{"domain":"go.example.com","slug":"   ","target_url":"https://example.com"}`, 400, "Slug is required"},
		{"legacy short_path field", `{"domain":"go.example.com","short_path":"ok","target_url":"https://example.com"}`, 400, "Slug is required"},
		{"reserved", `{"domain":"go.example.com","slug":"admin","target_url":"https://example.com"}`, 400, "Slug 'admin' is reserved and cannot be used as a short link"},
		{"reserved api", `{"domain":"go.example.com","slug":"api","target_url":"https://example.com"}`, 400, "Slug 'api' is reserved and cannot be used as a short link"},
		{"too long", `{"domain":"go.example.com","slug":"` + strings.Repeat("a", 65) + `","target_url":"https://example.com"}`, 400, "Slug must be at most 64 characters"},
		{"double hyphen", `{"domain":"go.example.com","slug":"a--b","target_url":"https://example.com"}`, 400, "Slug must not contain consecutive hyphens"},
		{"uppercase", `{"domain":"go.example.com","slug":"Promo","target_url":"https://example.com"}`, 400, slugRule},
		{"leading hyphen", `{"domain":"go.example.com","slug":"-promo","target_url":"https://example.com"}`, 400, slugRule},
		{"trailing hyphen", `{"domain":"go.example.com","slug":"promo-","target_url":"https://example.com"}`, 400, slugRule},
		{"slash", `{"domain":"go.example.com","slug":"a/b","target_url":"https://example.com"}`, 400, slugRule},
		{"hash", `{"domain":"go.example.com","slug":"a#b","target_url":"https://example.com"}`, 400, slugRule},
		{"non-ascii", `{"domain":"go.example.com","slug":"über","target_url":"https://example.com"}`, 400, slugRule},
		{"missing url", `{"domain":"go.example.com","slug":"ok"}`, 400, "Target URL is required"},
		{"http url", `{"domain":"go.example.com","slug":"ok","target_url":"http://example.com"}`, 400, "Target URL must start with https://"},
		{"short url", `{"domain":"go.example.com","slug":"ok","target_url":"https://x.x"}`, 400, "Target URL must be a valid URL with a domain"},
		{"body is array", `[]`, 500, "Internal server error"},
		{"slug is number", `{"domain":"go.example.com","slug":1,"target_url":"https://example.com"}`, 500, "Internal server error"},
		{"slug is null", `{"domain":"go.example.com","slug":null,"target_url":"https://example.com"}`, 500, "Internal server error"},
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
		resp := call(t, storetest.NewFake(), request("POST /api/links", "", `{"domain":"a.co","slug":"`+p+`","target_url":"https://x.xx"}`, "[admin]"))
		if resp.StatusCode != 201 {
			t.Errorf("%q: status %d, body %s", p, resp.StatusCode, resp.Body)
		}
	}
}

func TestUpdate(t *testing.T) {
	fake := storetest.NewFake(existing)
	resp := call(t, fake, request(routeUpdate, "Go.Example.com/promo", `{"target_url":"https://example.com/new"}`, "[admin]"))
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
		resp := call(t, storetest.NewFake(existing), request(routeUpdate, "go.example.com/nope", `{"target_url":"https://example.com/new"}`, "[admin]"))
		wantError(t, resp, 404, "Slug 'nope' not found on go.example.com")
	})
	t.Run("slug on another domain", func(t *testing.T) {
		resp := call(t, storetest.NewFake(existing), request(routeUpdate, "other.example/promo", `{"target_url":"https://example.com/new"}`, "[admin]"))
		wantError(t, resp, 404, "Slug 'promo' not found on other.example")
	})
	t.Run("invalid domain", func(t *testing.T) {
		fake := storetest.NewFake(existing)
		resp := call(t, fake, request(routeUpdate, "go.example.com:443/promo", `{"target_url":"https://example.com/new"}`, "[admin]"))
		wantError(t, resp, 400, "Domain 'go.example.com:443' is not a valid hostname")
		if fake.Calls["UpdateLinkTarget"] != 0 {
			t.Error("store was called")
		}
	})
	t.Run("invalid url", func(t *testing.T) {
		fake := storetest.NewFake(existing)
		resp := call(t, fake, request(routeUpdate, "go.example.com/promo", `{"target_url":"ftp://example.com"}`, "[admin]"))
		wantError(t, resp, 400, "Target URL must start with https://")
		if fake.Links()[0] != existing {
			t.Error("link was changed")
		}
	})
	t.Run("invalid json", func(t *testing.T) {
		resp := call(t, storetest.NewFake(existing), request(routeUpdate, "go.example.com/promo", `nope`, "[admin]"))
		wantError(t, resp, 400, "Invalid JSON body")
	})
	t.Run("store failure", func(t *testing.T) {
		fake := storetest.NewFake(existing)
		fake.Errors = map[string]error{"UpdateLinkTarget": errors.New("boom")}
		resp := call(t, fake, request(routeUpdate, "go.example.com/promo", `{"target_url":"https://example.com/new"}`, "[admin]"))
		wantError(t, resp, 500, "Internal server error")
	})
}

func TestDelete(t *testing.T) {
	fake := storetest.NewFake(existing)
	resp := call(t, fake, request(routeDelete, "go.example.com/promo", "", "[admin]"))
	if resp.StatusCode != 204 || resp.Body != "" {
		t.Fatalf("got %d %q", resp.StatusCode, resp.Body)
	}
	if len(fake.Links()) != 0 {
		t.Error("link was not deleted")
	}

	resp = call(t, fake, request(routeDelete, "go.example.com/promo", "", "[admin]"))
	wantError(t, resp, 404, "Slug 'promo' not found on go.example.com")

	resp = call(t, fake, request(routeDelete, "/promo", "", "[admin]"))
	wantError(t, resp, 400, "Domain is required")
}

func TestSameSlugOnTwoDomains(t *testing.T) {
	fake := storetest.NewFake(existing)
	resp := call(t, fake, request("POST /api/links", "", `{"domain":"other.example","slug":"promo","target_url":"https://example.com/other"}`, "[admin]"))
	if resp.StatusCode != 201 {
		t.Fatalf("status = %d, body %s", resp.StatusCode, resp.Body)
	}
	if n := len(fake.Links()); n != 2 {
		t.Fatalf("links = %d, want 2", n)
	}

	resp = call(t, fake, request(routeUpdate, "other.example/promo", `{"target_url":"https://example.com/changed"}`, "[admin]"))
	if resp.StatusCode != 200 {
		t.Fatalf("update status = %d", resp.StatusCode)
	}
	resp = call(t, fake, request(routeDelete, "other.example/promo", "", "[admin]"))
	if resp.StatusCode != 204 {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	if links := fake.Links(); len(links) != 1 || links[0] != existing {
		t.Errorf("the link on go.example.com changed: %+v", links)
	}
}

func TestListByDomain(t *testing.T) {
	other := store.Link{Domain: "other.example", Slug: "promo", TargetURL: "https://example.com/o"}
	tests := []struct {
		name, query, wantDomain string
		wantCount               int
	}{
		{"all domains", "", "", 2},
		{"one domain", "go.example.com", "go.example.com", 1},
		{"uppercase domain", " GO.Example.com ", "go.example.com", 1},
		{"unknown domain", "none.example", "none.example", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := storetest.NewFake(existing, other)
			req := request("GET /api/links", "", "", "[viewer]")
			if tt.query != "" {
				req.QueryStringParameters = map[string]string{"domain": tt.query}
			}
			resp := call(t, fake, req)
			var body struct {
				Links []store.Link `json:"links"`
				Count int          `json:"count"`
			}
			if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 || body.Count != tt.wantCount || len(body.Links) != tt.wantCount {
				t.Errorf("got %d %s", resp.StatusCode, resp.Body)
			}
			if fake.LastDomain != tt.wantDomain {
				t.Errorf("store domain = %q, want %q", fake.LastDomain, tt.wantDomain)
			}
		})
	}

	fake := storetest.NewFake(existing)
	req := request("GET /api/links", "", "", "[viewer]")
	req.QueryStringParameters = map[string]string{"domain": "not a domain"}
	wantError(t, call(t, fake, req), 400, "Domain 'not a domain' is not a valid hostname")
	if fake.Calls["ListLinks"] != 0 {
		t.Error("store was called")
	}
}

func TestPathSlugIsValidatedLikeCreate(t *testing.T) {
	const slugRule = "Slug must contain only lowercase letters, numbers, and hyphens. Must start and end with a letter or number."
	tests := []struct {
		slug, msg string
	}{
		{"Promo", slugRule},
		{"PROMO", slugRule},
		{"a--b", "Slug must not contain consecutive hyphens"},
		{"admin", "Slug 'admin' is reserved and cannot be used as a short link"},
		{strings.Repeat("a", 65), "Slug must be at most 64 characters"},
		{"   ", "Slug is required"},
	}
	for _, route := range []struct{ key, body string }{
		{routeUpdate, `{"target_url":"https://example.com/new"}`},
		{routeDelete, ""},
	} {
		for _, tt := range tests {
			fake := storetest.NewFake(existing)
			resp := call(t, fake, request(route.key, "go.example.com/"+tt.slug, route.body, "[admin]"))
			wantError(t, resp, 400, tt.msg)
			if fake.Calls["UpdateLinkTarget"]+fake.Calls["DeleteLink"] != 0 {
				t.Errorf("%s %q reached the store", route.key, tt.slug)
			}
		}
	}

	// Surrounding whitespace is trimmed, as on create.
	fake := storetest.NewFake(existing)
	resp := call(t, fake, request(routeDelete, "go.example.com/ promo ", "", "[admin]"))
	if resp.StatusCode != 204 || len(fake.Links()) != 0 {
		t.Errorf("delete of trimmed slug: %d %s", resp.StatusCode, resp.Body)
	}
}

func TestCreateRejectsUnknownDomain(t *testing.T) {
	ssm := settingstest.New()
	ssm.Put("/stack/domains", settings.Domain{Domain: "go.example.com", Status: settings.StatusPendingDNS})
	fake := storetest.NewFake()
	h := &Handler{Store: fake, Domains: ssm.Store("/stack/domains"), Now: func() time.Time { return fixedNow }}

	resp, err := h.Handle(context.Background(), request("POST /api/links", "", `{"domain":"other.example.com","slug":"new","target_url":"https://example.com/x"}`, "[admin]"))
	if err != nil {
		t.Fatal(err)
	}
	wantError(t, resp, 400, "Unknown domain 'other.example.com'; add it in the domain settings first")
	if len(fake.Links()) != 0 {
		t.Errorf("links = %v, want none", fake.Links())
	}

	resp, err = h.Handle(context.Background(), request("POST /api/links", "", `{"domain":"Go.Example.com","slug":"new","target_url":"https://example.com/x"}`, "[admin]"))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 {
		t.Errorf("create on a configured, not yet active domain: status = %d, body %s", resp.StatusCode, resp.Body)
	}

	ssm.SetErr(errors.New("throttled"))
	resp, _ = h.Handle(context.Background(), request("POST /api/links", "", `{"domain":"go.example.com","slug":"other","target_url":"https://example.com/x"}`, "[admin]"))
	wantError(t, resp, 500, "Internal server error")
}
