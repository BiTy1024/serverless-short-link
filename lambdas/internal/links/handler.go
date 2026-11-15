// Package links serves the CRUD API for short links under /api/links. A link
// is addressed by its domain and slug.
package links

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/apigw"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/domain"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/linkslug"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

// Store is the data access the links handler needs.
type Store interface {
	ListLinks(ctx context.Context, domain string) ([]store.Link, error)
	CreateLink(ctx context.Context, link store.Link) error
	UpdateLinkTarget(ctx context.Context, domain, slug, targetURL, updatedAt string) (store.Link, error)
	DeleteLink(ctx context.Context, domain, slug string) error
}

// Handler serves the /api/links routes.
type Handler struct {
	Store Store
	// Domains tells which domains are configured; links can only be
	// created on those.
	Domains Domains
	Now     func() time.Time
}

// Domains reports whether a domain is configured in the domain settings.
type Domains interface {
	Exists(ctx context.Context, domain string) (bool, error)
}

const (
	routeList   = "GET /api/links"
	routeCreate = "POST /api/links"
	routeUpdate = "PUT /api/links/{domain}/{path}"
	routeDelete = "DELETE /api/links/{domain}/{path}"

	adminGroup = "admin"
	// minURLLength is the length of the shortest accepted URL, "https://x.xx".
	minURLLength = len("https://x.xx")
)

type routeFunc func(h *Handler, ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)

var routes = map[string]routeFunc{
	routeList:   (*Handler).list,
	routeCreate: (*Handler).create,
	routeUpdate: (*Handler).update,
	routeDelete: (*Handler).delete,
}

var writeRoutes = []string{routeCreate, routeUpdate, routeDelete}

// Handle routes one request. Unknown routes get a 404, writes by non-admins a
// 403, and any failure inside a route a 500.
func (h *Handler) Handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	slog.Info("Request", "route_key", req.RouteKey)

	route, ok := routes[req.RouteKey]
	if !ok {
		return apigw.Error(404, "Route not found"), nil
	}
	if slices.Contains(writeRoutes, req.RouteKey) && !slices.Contains(apigw.UserGroups(req), adminGroup) {
		return apigw.Error(403, "Admin access required"), nil
	}
	return h.run(ctx, req, route), nil
}

func (h *Handler) run(ctx context.Context, req events.APIGatewayV2HTTPRequest, route routeFunc) (resp events.APIGatewayV2HTTPResponse) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Error handling route", "route_key", req.RouteKey, "error", fmt.Sprint(r))
			resp = apigw.Error(500, "Internal server error")
		}
	}()
	resp, err := route(h, ctx, req)
	if err != nil {
		slog.Error("Error handling route", "route_key", req.RouteKey, "error", err.Error())
		return apigw.Error(500, "Internal server error")
	}
	return resp
}

func validateURL(url string) string {
	switch {
	case url == "":
		return "Target URL is required"
	case !strings.HasPrefix(url, "https://"):
		return "Target URL must start with https://"
	case utf8.RuneCountInString(url) < minURLLength:
		return "Target URL must be a valid URL with a domain"
	}
	return ""
}

var errInvalidJSON = errors.New("invalid JSON body")

// requestBody is a decoded JSON object body. An empty request body counts as {}.
type requestBody map[string]any

func parseBody(raw string) (requestBody, error) {
	if raw == "" {
		raw = "{}"
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, errInvalidJSON
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("request body is %T, not an object", v)
	}
	return obj, nil
}

// field returns the trimmed string value of key, "" when it is absent, and
// an error when it holds anything but a string.
func (b requestBody) field(key string) (string, error) {
	v, ok := b[key]
	if !ok {
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("field %s is %T, not a string", key, v)
	}
	return apigw.TrimSpace(s), nil
}

type listResponse struct {
	Links []store.Link `json:"links"`
	Count int          `json:"count"`
}

// list returns the links of the domain given as ?domain=, or of every domain
// when the parameter is absent or empty.
func (h *Handler) list(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	name, err := domain.ParseOptional(req.QueryStringParameters["domain"])
	if err != nil {
		return apigw.Error(400, err.Error()), nil
	}
	links, err := h.Store.ListLinks(ctx, name)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	if links == nil {
		links = []store.Link{}
	}
	return apigw.JSON(200, listResponse{Links: links, Count: len(links)}), nil
}

func (h *Handler) create(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	body, err := parseBody(req.Body)
	if errors.Is(err, errInvalidJSON) {
		return apigw.Error(400, "Invalid JSON body"), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	rawDomain, err := body.field("domain")
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	slug, err := body.field("slug")
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	targetURL, err := body.field("target_url")
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	name, err := domain.Parse(rawDomain)
	if err != nil {
		return apigw.Error(400, err.Error()), nil
	}
	if msg := linkslug.Validate(slug); msg != "" {
		return apigw.Error(400, msg), nil
	}
	if msg := validateURL(targetURL); msg != "" {
		return apigw.Error(400, msg), nil
	}
	known, err := h.Domains.Exists(ctx, name)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	if !known {
		return apigw.Error(400, fmt.Sprintf("Unknown domain '%s'; add it in the domain settings first", name)), nil
	}

	now := store.Timestamp(h.Now())
	link := store.Link{Domain: name, Slug: slug, TargetURL: targetURL, CreatedAt: now, UpdatedAt: now}
	err = h.Store.CreateLink(ctx, link)
	if errors.Is(err, store.ErrLinkExists) {
		return apigw.Error(409, fmt.Sprintf("Slug '%s' already exists on %s", slug, name)), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	slog.Info("Created link", "domain", name, "slug", slug, "target_url", targetURL)
	return apigw.JSON(201, link), nil
}

func (h *Handler) update(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	name, err := domain.Parse(req.PathParameters["domain"])
	if err != nil {
		return apigw.Error(400, err.Error()), nil
	}
	slug := apigw.TrimSpace(req.PathParameters["path"])
	if msg := linkslug.Validate(slug); msg != "" {
		return apigw.Error(400, msg), nil
	}

	body, err := parseBody(req.Body)
	if errors.Is(err, errInvalidJSON) {
		return apigw.Error(400, "Invalid JSON body"), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	targetURL, err := body.field("target_url")
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	if msg := validateURL(targetURL); msg != "" {
		return apigw.Error(400, msg), nil
	}

	link, err := h.Store.UpdateLinkTarget(ctx, name, slug, targetURL, store.Timestamp(h.Now()))
	if errors.Is(err, store.ErrLinkNotFound) {
		return notFound(name, slug), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	slog.Info("Updated link", "domain", name, "slug", slug, "target_url", targetURL)
	return apigw.JSON(200, link), nil
}

func (h *Handler) delete(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	name, err := domain.Parse(req.PathParameters["domain"])
	if err != nil {
		return apigw.Error(400, err.Error()), nil
	}
	slug := apigw.TrimSpace(req.PathParameters["path"])
	if msg := linkslug.Validate(slug); msg != "" {
		return apigw.Error(400, msg), nil
	}

	err = h.Store.DeleteLink(ctx, name, slug)
	if errors.Is(err, store.ErrLinkNotFound) {
		return notFound(name, slug), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	slog.Info("Deleted link", "domain", name, "slug", slug)
	return apigw.JSON(204, nil), nil
}

func notFound(name, slug string) events.APIGatewayV2HTTPResponse {
	return apigw.Error(404, fmt.Sprintf("Slug '%s' not found on %s", slug, name))
}
