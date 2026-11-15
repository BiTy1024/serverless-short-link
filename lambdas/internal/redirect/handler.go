// Package redirect resolves a short path to its target URL, records the click
// and answers with a 301.
package redirect

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

// Store is the data access the redirect handler needs.
type Store interface {
	GetLink(ctx context.Context, shortPath string) (store.Link, bool, error)
	PutClick(ctx context.Context, click store.Click) error
}

// Handler serves every non-API route of the HTTP API.
type Handler struct {
	Store Store
	// DefaultRedirectURL is the target for "/" and for unknown short paths.
	DefaultRedirectURL string
	// AdminOrigin is the only origin allowed by the CORS headers on /api paths.
	AdminOrigin string
	Now         func() time.Time
}

// Handle answers one request. It never returns an error: failures become a
// 502 response.
func (h *Handler) Handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (resp events.APIGatewayV2HTTPResponse, err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Error processing request", "error", fmt.Sprint(r))
			resp, err = unavailable(), nil
		}
	}()

	path := req.RawPath
	if path == "" {
		path = "/"
	}
	slog.Info("Request received", "path", path)

	if strings.HasPrefix(path, "/api") {
		return h.apiFallback(req), nil
	}

	target, known, err := h.resolve(ctx, path)
	if err != nil {
		slog.Error("Error processing request", "error", err.Error())
		return unavailable(), nil
	}
	if known {
		h.track(ctx, "/"+strings.Trim(path, "/"), target)
	}

	return events.APIGatewayV2HTTPResponse{
		StatusCode: 301,
		Headers:    map[string]string{"Location": target},
	}, nil
}

// apiFallback answers /api paths that no API route matched: CORS preflight
// gets a 200, everything else a JSON 404.
func (h *Handler) apiFallback(req events.APIGatewayV2HTTPRequest) events.APIGatewayV2HTTPResponse {
	cors := map[string]string{
		"Access-Control-Allow-Origin":  h.AdminOrigin,
		"Access-Control-Allow-Methods": "GET, POST, PUT, DELETE, OPTIONS",
		"Access-Control-Allow-Headers": "Content-Type, Authorization",
		"Access-Control-Max-Age":       "3600",
	}
	if req.RequestContext.HTTP.Method == "OPTIONS" {
		return events.APIGatewayV2HTTPResponse{StatusCode: 200, Headers: cors}
	}
	headers := maps.Clone(cors)
	headers["Content-Type"] = "application/json"
	return events.APIGatewayV2HTTPResponse{
		StatusCode: 404,
		Headers:    headers,
		Body:       `{"error": "Not found"}`,
	}
}

// resolve returns the target for path; known is false when the default URL is used.
func (h *Handler) resolve(ctx context.Context, path string) (target string, known bool, err error) {
	shortPath := strings.Trim(path, "/")
	if shortPath == "" {
		return h.DefaultRedirectURL, false, nil
	}
	link, ok, err := h.Store.GetLink(ctx, shortPath)
	if err != nil {
		return "", false, err
	}
	if ok {
		slog.Info("Redirect hit", "short_path", shortPath, "target_url", link.TargetURL)
		return link.TargetURL, true, nil
	}
	slog.Info("Redirect miss, using default", "short_path", shortPath, "target_url", h.DefaultRedirectURL)
	return h.DefaultRedirectURL, false, nil
}

// track records a click. A failure is logged and never fails the redirect.
func (h *Handler) track(ctx context.Context, redirectPath, target string) {
	ts := store.Timestamp(h.Now())
	err := h.Store.PutClick(ctx, store.Click{
		RedirectPath: redirectPath,
		Timestamp:    ts,
		TargetURL:    &target,
	})
	if err != nil {
		slog.Error("Failed to track redirect", "error", err.Error())
		return
	}
	slog.Info("Tracked click", "path", redirectPath, "timestamp", ts)
}

func unavailable() events.APIGatewayV2HTTPResponse {
	return events.APIGatewayV2HTTPResponse{
		StatusCode: 502,
		Headers:    map[string]string{"Content-Type": "text/plain"},
		Body:       "Service temporarily unavailable",
	}
}
