package domains

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/apigw"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
)

const (
	routeList   = "GET /api/domains"
	routeCreate = "POST /api/domains"
	routeCheck  = "POST /api/domains/{domain}/check"
	routeUpdate = "PATCH /api/domains/{domain}"
	routeDelete = "DELETE /api/domains/{domain}"

	adminGroup = "admin"
)

// Handler serves the /api/domains routes and the scheduled check.
type Handler struct {
	Manager *Manager
}

type routeFunc func(h *Handler, ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error)

var routes = map[string]routeFunc{
	routeList:   (*Handler).list,
	routeCreate: (*Handler).create,
	routeCheck:  (*Handler).check,
	routeUpdate: (*Handler).update,
	routeDelete: (*Handler).delete,
}

// Invoke is the Lambda entry point. An EventBridge scheduled event checks
// every pending domain; anything else is handled as an HTTP API request.
func (h *Handler) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var probe struct {
		Source     string `json:"source"`
		DetailType string `json:"detail-type"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil && probe.Source == "aws.events" {
		slog.Info("Scheduled check", "detail_type", probe.DetailType)
		return nil, h.Manager.CheckAll(ctx)
	}
	var req events.APIGatewayV2HTTPRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decoding event: %w", err)
	}
	return h.Handle(ctx, req)
}

// Handle routes one request. Unknown routes get a 404, writes by non-admins a
// 403, and any unexpected failure a 500.
func (h *Handler) Handle(ctx context.Context, req events.APIGatewayV2HTTPRequest) (resp events.APIGatewayV2HTTPResponse, err error) {
	slog.Info("Request", "route_key", req.RouteKey)
	route, ok := routes[req.RouteKey]
	if !ok {
		return apigw.Error(404, "Route not found"), nil
	}
	if req.RouteKey != routeList && !slices.Contains(apigw.UserGroups(req), adminGroup) {
		return apigw.Error(403, "Admin access required"), nil
	}
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Error handling route", "route_key", req.RouteKey, "error", fmt.Sprint(r))
			resp, err = apigw.Error(500, "Internal server error"), nil
		}
	}()
	resp, err = route(h, ctx, req)
	if err == nil {
		return resp, nil
	}
	var e *Error
	if errors.As(err, &e) {
		if e.LinkCount > 0 {
			return apigw.JSON(e.Status, map[string]any{"error": e.Message, "link_count": e.LinkCount}), nil
		}
		return apigw.Error(e.Status, e.Message), nil
	}
	slog.Error("Error handling route", "route_key", req.RouteKey, "error", err.Error())
	return apigw.Error(500, "Internal server error"), nil
}

type listResponse struct {
	Domains []settings.Domain `json:"domains"`
	Count   int               `json:"count"`
	// Route53ZoneIDs are the hosted zones a secondary domain may use in
	// route53 mode.
	Route53ZoneIDs []string `json:"route53_zone_ids"`
}

func (h *Handler) list(ctx context.Context, _ events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	all, err := h.Manager.List(ctx)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	zones := h.Manager.Route53Zones
	if zones == nil {
		zones = []string{}
	}
	return apigw.JSON(200, listResponse{Domains: all, Count: len(all), Route53ZoneIDs: zones}), nil
}

// body decodes a JSON object of string fields; an empty body counts as {}.
func body(raw string) (map[string]string, error) {
	if raw == "" {
		raw = "{}"
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil || fields == nil {
		return nil, badRequest("Invalid JSON body")
	}
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		s, ok := v.(string)
		if !ok {
			return nil, badRequest("Field %s must be a string", k)
		}
		out[k] = apigw.TrimSpace(s)
	}
	return out, nil
}

func (h *Handler) create(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	b, err := body(req.Body)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	d, err := h.Manager.Create(ctx, CreateRequest{
		Domain:       b["domain"],
		DNSMode:      b["dns_mode"],
		HostedZoneID: b["hosted_zone_id"],
		FallbackURL:  b["fallback_url"],
	})
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	return apigw.JSON(201, d), nil
}

func (h *Handler) check(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	d, err := h.Manager.Check(ctx, req.PathParameters["domain"])
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	return apigw.JSON(200, d), nil
}

func (h *Handler) update(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	b, err := body(req.Body)
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	for k := range b {
		if k != "fallback_url" {
			return events.APIGatewayV2HTTPResponse{}, badRequest("Only fallback_url can be changed")
		}
	}
	d, err := h.Manager.UpdateFallback(ctx, req.PathParameters["domain"], b["fallback_url"])
	if err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	return apigw.JSON(200, d), nil
}

func (h *Handler) delete(ctx context.Context, req events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
	if err := h.Manager.Delete(ctx, req.PathParameters["domain"]); err != nil {
		return events.APIGatewayV2HTTPResponse{}, err
	}
	return apigw.JSON(204, nil), nil
}
