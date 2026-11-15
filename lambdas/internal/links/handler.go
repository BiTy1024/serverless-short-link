// Package links serves the CRUD API for short links under /api/links.
package links

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/apigw"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

// Store is the data access the links handler needs.
type Store interface {
	ListLinks(ctx context.Context) ([]store.Link, error)
	CreateLink(ctx context.Context, link store.Link) error
	UpdateLinkTarget(ctx context.Context, shortPath, targetURL, updatedAt string) (store.Link, error)
	DeleteLink(ctx context.Context, shortPath string) error
}

// Handler serves the /api/links routes.
type Handler struct {
	Store Store
	Now   func() time.Time
}

const (
	routeList   = "GET /api/links"
	routeCreate = "POST /api/links"
	routeUpdate = "PUT /api/links/{path}"
	routeDelete = "DELETE /api/links/{path}"

	adminGroup    = "admin"
	maxPathLength = 64
	// minURLLength is the length of the shortest accepted URL, "https://x.xx".
	minURLLength = len("https://x.xx")
)

var (
	pathPattern   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)
	reservedPaths = []string{"api", "admin", "health", "status"}
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
	if slices.Contains(writeRoutes, req.RouteKey) && !slices.Contains(userGroups(req), adminGroup) {
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

// userGroups reads the cognito:groups claim, which the HTTP API JWT
// authorizer passes as a string such as "[admin]": surrounding brackets and
// all spaces are removed and the rest is split on commas.
func userGroups(req events.APIGatewayV2HTTPRequest) []string {
	auth := req.RequestContext.Authorizer
	if auth == nil || auth.JWT == nil {
		return nil
	}
	groups := auth.JWT.Claims["cognito:groups"]
	if groups == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(strings.Trim(groups, "[]"), " ", ""), ",")
}

func validatePath(path string) string {
	switch {
	case path == "":
		return "Path is required"
	case slices.Contains(reservedPaths, path):
		return fmt.Sprintf("Path '%s' is reserved and cannot be used as a short link", path)
	case utf8.RuneCountInString(path) > maxPathLength:
		return fmt.Sprintf("Path must be at most %d characters", maxPathLength)
	case strings.Contains(path, "--"):
		return "Path must not contain consecutive hyphens"
	case !pathPattern.MatchString(path):
		return "Path must contain only lowercase letters, numbers, and hyphens. Must start and end with a letter or number."
	}
	return ""
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

func (h *Handler) list(ctx context.Context, _ events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	links, err := h.Store.ListLinks(ctx)
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
	shortPath, err := body.field("short_path")
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	targetURL, err := body.field("target_url")
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	if msg := validatePath(shortPath); msg != "" {
		return apigw.Error(400, msg), nil
	}
	if msg := validateURL(targetURL); msg != "" {
		return apigw.Error(400, msg), nil
	}

	now := store.Timestamp(h.Now())
	link := store.Link{ShortPath: shortPath, TargetURL: targetURL, CreatedAt: now, UpdatedAt: now}
	err = h.Store.CreateLink(ctx, link)
	if errors.Is(err, store.ErrLinkExists) {
		return apigw.Error(409, fmt.Sprintf("Path '%s' already exists", shortPath)), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	slog.Info("Created link", "short_path", shortPath, "target_url", targetURL)
	return apigw.JSON(201, link), nil
}

func (h *Handler) update(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	shortPath := req.PathParameters["path"]

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

	link, err := h.Store.UpdateLinkTarget(ctx, shortPath, targetURL, store.Timestamp(h.Now()))
	if errors.Is(err, store.ErrLinkNotFound) {
		return apigw.Error(404, fmt.Sprintf("Path '%s' not found", shortPath)), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	slog.Info("Updated link", "short_path", shortPath, "target_url", targetURL)
	return apigw.JSON(200, link), nil
}

func (h *Handler) delete(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	shortPath := req.PathParameters["path"]

	err := h.Store.DeleteLink(ctx, shortPath)
	if errors.Is(err, store.ErrLinkNotFound) {
		return apigw.Error(404, fmt.Sprintf("Path '%s' not found", shortPath)), nil
	}
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}

	slog.Info("Deleted link", "short_path", shortPath)
	return apigw.JSON(204, nil), nil
}
