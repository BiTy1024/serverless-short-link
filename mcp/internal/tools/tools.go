// Package tools exposes the short-link management API as MCP tools.
package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/shortlink"
)

// Instructions is sent to clients on initialize.
const Instructions = `Manage the short links of a serverless short-link service.
A short link maps a slug (e.g. "docs", served at https://<domain>/docs) to an https target URL.
Slugs are lowercase letters, digits and single hyphens, 1-64 characters, and must not be one of: api, admin, health, status.
Read tools work for users in the Cognito group 'viewer' or 'admin'; create, update and delete need 'admin'.`

const (
	defaultListLimit  = 100
	maxListLimit      = 1000
	defaultStatsLimit = 50
)

// Register adds all tools to server, backed by client.
func Register(server *mcp.Server, client *shortlink.Client) error {
	h := &handlers{c: client}

	listSchema, err := schemaFor[ListLinksInput](nil)
	if err != nil {
		return err
	}
	slugSchema, err := schemaFor[SlugInput](slugProps("slug"))
	if err != nil {
		return err
	}
	writeSchema, err := schemaFor[WriteLinkInput](func(s *jsonschema.Schema) {
		slugProps("slug")(s)
		s.Properties["target_url"].Pattern = "^https://"
	})
	if err != nil {
		return err
	}
	overviewSchema, err := schemaFor[StatsOverviewInput](nil)
	if err != nil {
		return err
	}
	statsSchema, err := schemaFor[LinkStatsInput](func(s *jsonschema.Schema) {
		slugProps("slug")(s)
		date := `^[0-9]{4}-[0-9]{2}-[0-9]{2}$`
		s.Properties["from"].Pattern = date
		s.Properties["to"].Pattern = date
		s.Properties["days"].Minimum = ptr(1.0)
		s.Properties["days"].Maximum = ptr(3650.0)
	})
	if err != nil {
		return err
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:  "list_links",
		Title: "List short links",
		Description: "List short links with their target URLs and timestamps, sorted by slug. " +
			"Use 'contains' to filter by a substring of slug or target URL, and 'limit'/'offset' to page through large result sets. " +
			"Returns links, total (matches before paging) and next_offset when more results exist.",
		InputSchema: listSchema,
		Annotations: readOnly("List short links"),
	}, h.listLinks)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_link",
		Title:       "Get a short link",
		Description: "Get one short link by slug: its target URL and creation/update timestamps. Fails with a not-found error if the slug does not exist.",
		InputSchema: slugSchema,
		Annotations: readOnly("Get a short link"),
	}, h.getLink)

	mcp.AddTool(server, &mcp.Tool{
		Name:  "create_link",
		Title: "Create a short link",
		Description: "Create a new short link from slug to an https target URL. " +
			"Fails if the slug already exists (use update_link to change an existing link). Requires the 'admin' group.",
		InputSchema: writeSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Create a short link",
			DestructiveHint: ptr(false),
			OpenWorldHint:   ptr(false),
		},
	}, h.createLink)

	mcp.AddTool(server, &mcp.Tool{
		Name:  "update_link",
		Title: "Change a short link's target",
		Description: "Replace the target URL of an existing short link. The previous target is overwritten and visitors are redirected to the new one immediately. " +
			"Fails if the slug does not exist. Requires the 'admin' group.",
		InputSchema: writeSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Change a short link's target",
			DestructiveHint: ptr(true),
			IdempotentHint:  true,
			OpenWorldHint:   ptr(false),
		},
	}, h.updateLink)

	mcp.AddTool(server, &mcp.Tool{
		Name:  "delete_link",
		Title: "Delete a short link",
		Description: "Permanently delete a short link; its URL stops redirecting. Recorded click statistics are kept. " +
			"Cannot be undone. Requires the 'admin' group.",
		InputSchema: slugSchema,
		Annotations: &mcp.ToolAnnotations{
			Title:           "Delete a short link",
			DestructiveHint: ptr(true),
			IdempotentHint:  true,
			OpenWorldHint:   ptr(false),
		},
	}, h.deleteLink)

	mcp.AddTool(server, &mcp.Tool{
		Name:  "get_stats_overview",
		Title: "Click statistics overview",
		Description: "Click totals per redirect path, sorted by clicks descending, plus the overall total. " +
			"By default only paths that currently have a link are counted (linked_only=true); set linked_only=false to include clicks on deleted or unknown paths. " +
			"Paths in the result include the leading slash, e.g. /docs. Use 'limit' to cap the number of rows.",
		InputSchema: overviewSchema,
		Annotations: readOnly("Click statistics overview"),
	}, h.statsOverview)

	mcp.AddTool(server, &mcp.Tool{
		Name:  "get_link_stats",
		Title: "Click statistics for one link",
		Description: "Click count and the 20 most recent clicks for one slug. " +
			"Optionally restrict to the last 'days' days, or to a date range with 'from' and/or 'to' (YYYY-MM-DD, UTC, inclusive); do not combine 'days' with 'from'/'to'. " +
			"Without a filter, all recorded clicks are counted.",
		InputSchema: statsSchema,
		Annotations: readOnly("Click statistics for one link"),
	}, h.linkStats)

	return nil
}

// ListLinksInput is the input of list_links.
type ListLinksInput struct {
	Contains string `json:"contains,omitempty" jsonschema:"case-insensitive substring to match against slug or target URL"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum number of links to return (1-1000, default 100)"`
	Offset   int    `json:"offset,omitempty" jsonschema:"number of matching links to skip, for paging (default 0)"`
}

// ListLinksOutput is the output of list_links.
type ListLinksOutput struct {
	Links      []shortlink.Link `json:"links"`
	Total      int              `json:"total" jsonschema:"number of links matching the filter, before paging"`
	NextOffset *int             `json:"next_offset,omitempty" jsonschema:"offset of the next page; absent on the last page"`
}

// SlugInput identifies one link.
type SlugInput struct {
	Slug string `json:"slug" jsonschema:"the short path, without leading slash, e.g. docs"`
}

// WriteLinkInput is the input of create_link and update_link.
type WriteLinkInput struct {
	Slug      string `json:"slug" jsonschema:"the short path, without leading slash, e.g. docs"`
	TargetURL string `json:"target_url" jsonschema:"the https URL to redirect to, e.g. https://example.com/page"`
}

// DeleteLinkOutput is the output of delete_link.
type DeleteLinkOutput struct {
	Deleted string `json:"deleted" jsonschema:"slug of the deleted link"`
}

// StatsOverviewInput is the input of get_stats_overview.
type StatsOverviewInput struct {
	LinkedOnly *bool `json:"linked_only,omitempty" jsonschema:"count only paths that currently have a link (default true)"`
	Limit      int   `json:"limit,omitempty" jsonschema:"maximum number of paths to return, most clicked first (1-1000, default 50)"`
}

// StatsOverviewOutput is the output of get_stats_overview.
type StatsOverviewOutput struct {
	Stats       []shortlink.LinkStatsSummary `json:"stats"`
	TotalClicks int                          `json:"total_clicks" jsonschema:"sum of clicks over all counted paths, including those cut off by limit"`
	TotalPaths  int                          `json:"total_paths" jsonschema:"number of counted paths before limit"`
}

// LinkStatsInput is the input of get_link_stats.
type LinkStatsInput struct {
	Slug string `json:"slug" jsonschema:"the short path, without leading slash, e.g. docs"`
	Days int    `json:"days,omitempty" jsonschema:"only count clicks from the last N days (1-3650)"`
	From string `json:"from,omitempty" jsonschema:"only count clicks on or after this UTC date, YYYY-MM-DD"`
	To   string `json:"to,omitempty" jsonschema:"only count clicks on or before this UTC date, YYYY-MM-DD"`
}

type handlers struct {
	c *shortlink.Client
}

func (h *handlers) listLinks(ctx context.Context, _ *mcp.CallToolRequest, in ListLinksInput) (*mcp.CallToolResult, ListLinksOutput, error) {
	limit, err := limitOrDefault(in.Limit, defaultListLimit)
	if err != nil {
		return nil, ListLinksOutput{}, err
	}
	if in.Offset < 0 {
		return nil, ListLinksOutput{}, errors.New("offset must not be negative")
	}
	links, err := h.c.ListLinks(ctx)
	if err != nil {
		return nil, ListLinksOutput{}, explain(err, "")
	}
	needle := strings.ToLower(in.Contains)
	matched := links[:0:0]
	for _, l := range links {
		if needle == "" || strings.Contains(l.ShortPath, needle) || strings.Contains(strings.ToLower(l.TargetURL), needle) {
			matched = append(matched, l)
		}
	}
	sortLinks(matched)

	out := ListLinksOutput{Total: len(matched), Links: []shortlink.Link{}}
	if in.Offset < len(matched) {
		end := min(in.Offset+limit, len(matched))
		out.Links = matched[in.Offset:end]
		if end < len(matched) {
			out.NextOffset = &end
		}
	}
	return nil, out, nil
}

func (h *handlers) getLink(ctx context.Context, _ *mcp.CallToolRequest, in SlugInput) (*mcp.CallToolResult, *shortlink.Link, error) {
	if err := shortlink.ValidateSlug(in.Slug); err != nil {
		return nil, nil, err
	}
	l, err := h.c.GetLink(ctx, in.Slug)
	return nil, l, explain(err, in.Slug)
}

func (h *handlers) createLink(ctx context.Context, _ *mcp.CallToolRequest, in WriteLinkInput) (*mcp.CallToolResult, *shortlink.Link, error) {
	if err := validateWrite(in); err != nil {
		return nil, nil, err
	}
	l, err := h.c.CreateLink(ctx, in.Slug, in.TargetURL)
	return nil, l, explain(err, in.Slug)
}

func (h *handlers) updateLink(ctx context.Context, _ *mcp.CallToolRequest, in WriteLinkInput) (*mcp.CallToolResult, *shortlink.Link, error) {
	if err := validateWrite(in); err != nil {
		return nil, nil, err
	}
	l, err := h.c.UpdateLink(ctx, in.Slug, in.TargetURL)
	return nil, l, explain(err, in.Slug)
}

func (h *handlers) deleteLink(ctx context.Context, _ *mcp.CallToolRequest, in SlugInput) (*mcp.CallToolResult, DeleteLinkOutput, error) {
	if err := shortlink.ValidateSlug(in.Slug); err != nil {
		return nil, DeleteLinkOutput{}, err
	}
	if err := h.c.DeleteLink(ctx, in.Slug); err != nil {
		return nil, DeleteLinkOutput{}, explain(err, in.Slug)
	}
	return nil, DeleteLinkOutput{Deleted: in.Slug}, nil
}

func (h *handlers) statsOverview(ctx context.Context, _ *mcp.CallToolRequest, in StatsOverviewInput) (*mcp.CallToolResult, StatsOverviewOutput, error) {
	limit, err := limitOrDefault(in.Limit, defaultStatsLimit)
	if err != nil {
		return nil, StatsOverviewOutput{}, err
	}
	linkedOnly := in.LinkedOnly == nil || *in.LinkedOnly
	ov, err := h.c.StatsOverview(ctx, linkedOnly)
	if err != nil {
		return nil, StatsOverviewOutput{}, explain(err, "")
	}
	out := StatsOverviewOutput{Stats: ov.Stats, TotalClicks: ov.TotalClicks, TotalPaths: len(ov.Stats)}
	if len(out.Stats) > limit {
		out.Stats = out.Stats[:limit]
	}
	return nil, out, nil
}

func (h *handlers) linkStats(ctx context.Context, _ *mcp.CallToolRequest, in LinkStatsInput) (*mcp.CallToolResult, *shortlink.LinkStats, error) {
	if err := shortlink.ValidateSlug(in.Slug); err != nil {
		return nil, nil, err
	}
	if in.Days != 0 && (in.From != "" || in.To != "") {
		return nil, nil, errors.New("use either 'days' or 'from'/'to', not both")
	}
	if in.Days < 0 {
		return nil, nil, errors.New("days must be positive")
	}
	st, err := h.c.LinkStats(ctx, in.Slug, shortlink.StatsFilter{Days: in.Days, From: in.From, To: in.To})
	return nil, st, explain(err, in.Slug)
}

func validateWrite(in WriteLinkInput) error {
	if err := shortlink.ValidateSlug(in.Slug); err != nil {
		return err
	}
	return shortlink.ValidateTargetURL(in.TargetURL)
}

// explain adds a next step to API errors an agent can act on.
func explain(err error, slug string) error {
	var apiErr *shortlink.APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Status {
	case http.StatusNotFound:
		return fmt.Errorf("%w; no link with slug %q exists, use list_links to find existing slugs", err, slug)
	case http.StatusConflict:
		return fmt.Errorf("%w; use update_link to change the target of an existing link, or choose another slug", err)
	}
	return err
}

func limitOrDefault(limit, def int) (int, error) {
	if limit == 0 {
		return def, nil
	}
	if limit < 1 || limit > maxListLimit {
		return 0, fmt.Errorf("limit must be between 1 and %d", maxListLimit)
	}
	return limit, nil
}

// schemaFor infers the input schema of T and applies edit to it.
func schemaFor[T any](edit func(*jsonschema.Schema)) (*jsonschema.Schema, error) {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		return nil, err
	}
	if edit != nil {
		edit(s)
	}
	return s, nil
}

// slugProps constrains the named property to the API's slug rules.
func slugProps(name string) func(*jsonschema.Schema) {
	return func(s *jsonschema.Schema) {
		p := s.Properties[name]
		p.Pattern = shortlink.SlugPattern
		p.MinLength = ptr(1)
		p.MaxLength = ptr(shortlink.MaxSlugLength)
		reserved := make([]any, len(shortlink.ReservedSlugs))
		for i, r := range shortlink.ReservedSlugs {
			reserved[i] = r
		}
		p.Not = &jsonschema.Schema{AnyOf: []*jsonschema.Schema{
			{Enum: reserved},
			{Pattern: "--"},
		}}
	}
}

func sortLinks(links []shortlink.Link) {
	slices.SortFunc(links, func(a, b shortlink.Link) int { return strings.Compare(a.ShortPath, b.ShortPath) })
}

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: ptr(false)}
}

func ptr[T any](v T) *T { return &v }
