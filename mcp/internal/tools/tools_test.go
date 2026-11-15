package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/cognito"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/shortlink"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/shortlink/fakeapi"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/testhttp"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/tools"
)

// connect starts the server with all tools over an in-memory transport and
// returns a connected client session.
func connect(t *testing.T, api *fakeapi.API) *mcp.ClientSession {
	t.Helper()
	return connectWith(t, api, fakeapi.NewTokens())
}

func connectWith(t *testing.T, api *fakeapi.API, tokens shortlink.TokenProvider) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	client := shortlink.NewClient("https://short.test", tokens, testhttp.Client(api))
	server := mcp.NewServer(&mcp.Implementation{Name: "shortlink", Version: "test"}, nil)
	if err := tools.Register(server, client); err != nil {
		t.Fatalf("Register: %v", err)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// call invokes a tool and returns its result; protocol errors fail the test.
func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func text(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// decode reads the structured output of a successful call into v.
func decode(t *testing.T, res *mcp.CallToolResult, v any) {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %s", text(res))
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
}

func TestToolList(t *testing.T) {
	cs := connect(t, fakeapi.New())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*mcp.Tool{}
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"list_links", "get_link", "create_link", "update_link", "delete_link", "get_stats_overview", "get_link_stats"} {
		tool := byName[name]
		if tool == nil {
			t.Errorf("tool %s missing", name)
			continue
		}
		if tool.Description == "" || tool.Annotations == nil {
			t.Errorf("tool %s lacks description or annotations", name)
		}
	}
	del := byName["delete_link"].Annotations
	if del.DestructiveHint == nil || !*del.DestructiveHint || del.ReadOnlyHint {
		t.Errorf("delete_link annotations = %+v, want destructive", del)
	}
	if !byName["list_links"].Annotations.ReadOnlyHint {
		t.Error("list_links should be read-only")
	}

	schema, _ := json.Marshal(byName["create_link"].InputSchema)
	for _, want := range []string{shortlink.SlugPattern, `"admin"`, `"required"`} {
		if !strings.Contains(string(schema), strings.ReplaceAll(want, `\`, `\\`)) {
			t.Errorf("create_link schema %s lacks %s", schema, want)
		}
	}
}

func TestCreateGetUpdateDelete(t *testing.T) {
	cs := connect(t, fakeapi.New())

	var link shortlink.Link
	decode(t, call(t, cs, "create_link", map[string]any{"slug": "docs", "target_url": "https://example.com/docs"}), &link)
	if link.ShortPath != "docs" || link.TargetURL != "https://example.com/docs" {
		t.Fatalf("created %+v", link)
	}

	res := call(t, cs, "create_link", map[string]any{"slug": "docs", "target_url": "https://example.com/x"})
	if !res.IsError || !strings.Contains(text(res), "update_link") {
		t.Fatalf("duplicate create: %s", text(res))
	}

	decode(t, call(t, cs, "update_link", map[string]any{"slug": "docs", "target_url": "https://example.com/v2"}), &link)
	if link.TargetURL != "https://example.com/v2" {
		t.Fatalf("updated %+v", link)
	}

	decode(t, call(t, cs, "get_link", map[string]any{"slug": "docs"}), &link)
	if link.TargetURL != "https://example.com/v2" {
		t.Fatalf("get %+v", link)
	}

	var del tools.DeleteLinkOutput
	decode(t, call(t, cs, "delete_link", map[string]any{"slug": "docs"}), &del)
	if del.Deleted != "docs" {
		t.Fatalf("deleted %+v", del)
	}

	res = call(t, cs, "get_link", map[string]any{"slug": "docs"})
	if !res.IsError || !strings.Contains(text(res), "list_links") {
		t.Fatalf("get after delete: %s", text(res))
	}
}

func TestInputValidation(t *testing.T) {
	cs := connect(t, fakeapi.New())
	cases := []map[string]any{
		{"slug": "api", "target_url": "https://example.com"},
		{"slug": "Docs", "target_url": "https://example.com"},
		{"slug": "a--b", "target_url": "https://example.com"},
		{"slug": "-a", "target_url": "https://example.com"},
		{"slug": strings.Repeat("a", 65), "target_url": "https://example.com"},
		{"slug": "docs", "target_url": "http://example.com"},
		{"slug": "docs"},
	}
	for _, args := range cases {
		if res := call(t, cs, "create_link", args); !res.IsError {
			t.Errorf("create_link(%v) succeeded, want validation error", args)
		}
	}
	res := call(t, cs, "get_link_stats", map[string]any{"slug": "docs", "days": 7, "from": "2026-01-01"})
	if !res.IsError {
		t.Error("days together with from should be rejected")
	}
	res = call(t, cs, "get_link_stats", map[string]any{"slug": "docs", "from": "01.01.2026"})
	if !res.IsError {
		t.Error("non-ISO date should be rejected")
	}
}

func TestViewerGetsActionableError(t *testing.T) {
	api := fakeapi.New()
	api.ViewerOnly = true
	cs := connect(t, api)
	res := call(t, cs, "delete_link", map[string]any{"slug": "docs"})
	if !res.IsError || !strings.Contains(text(res), "admin") {
		t.Fatalf("viewer delete: %s", text(res))
	}
}

func TestListLinksFilterAndPaging(t *testing.T) {
	api := fakeapi.New()
	cs := connect(t, api)
	for _, s := range []string{"c", "a", "b", "docs"} {
		call(t, cs, "create_link", map[string]any{"slug": s, "target_url": "https://example.com/" + s})
	}

	var out tools.ListLinksOutput
	decode(t, call(t, cs, "list_links", map[string]any{"limit": 2}), &out)
	if out.Total != 4 || len(out.Links) != 2 || out.Links[0].ShortPath != "a" || out.NextOffset == nil || *out.NextOffset != 2 {
		t.Fatalf("page 1 = %+v", out)
	}
	out = tools.ListLinksOutput{}
	decode(t, call(t, cs, "list_links", map[string]any{"limit": 2, "offset": 2}), &out)
	if len(out.Links) != 2 || out.Links[1].ShortPath != "docs" || out.NextOffset != nil {
		t.Fatalf("page 2 = %+v", out)
	}
	out = tools.ListLinksOutput{}
	decode(t, call(t, cs, "list_links", map[string]any{"contains": "DOC"}), &out)
	if out.Total != 1 || out.Links[0].ShortPath != "docs" {
		t.Fatalf("filtered = %+v", out)
	}
}

func TestStatsTools(t *testing.T) {
	api := fakeapi.New()
	cs := connect(t, api)
	call(t, cs, "create_link", map[string]any{"slug": "docs", "target_url": "https://example.com/docs"})
	api.AddClick("/docs", "https://example.com/docs", "2026-11-01T10:00:00+00:00")
	api.AddClick("/docs", "https://example.com/docs", "2026-11-03T10:00:00+00:00")
	api.AddClick("/gone", "https://example.com/gone", "2026-11-02T10:00:00+00:00")

	var ov tools.StatsOverviewOutput
	decode(t, call(t, cs, "get_stats_overview", nil), &ov)
	if ov.TotalClicks != 2 || ov.TotalPaths != 1 {
		t.Fatalf("default overview = %+v", ov)
	}
	if api.LastQuery() != "linked_only=true" {
		t.Fatalf("query = %q", api.LastQuery())
	}
	decode(t, call(t, cs, "get_stats_overview", map[string]any{"linked_only": false, "limit": 1}), &ov)
	if ov.TotalClicks != 3 || ov.TotalPaths != 2 || len(ov.Stats) != 1 || ov.Stats[0].Path != "/docs" {
		t.Fatalf("full overview = %+v", ov)
	}

	var st shortlink.LinkStats
	decode(t, call(t, cs, "get_link_stats", map[string]any{"slug": "docs", "from": "2026-11-02"}), &st)
	if st.Clicks != 1 || len(st.RecentClicks) != 1 {
		t.Fatalf("link stats = %+v", st)
	}
}

func TestRevokedRefreshTokenTellsUserToLogIn(t *testing.T) {
	tokens := fakeapi.NewTokens()
	tokens.Err = cognito.ErrRefreshTokenRejected
	cs := connectWith(t, fakeapi.New(), tokens)
	res := call(t, cs, "list_links", nil)
	if !res.IsError || !strings.Contains(text(res), "run `shortlink-mcp login`") {
		t.Fatalf("result: %s", text(res))
	}
}
