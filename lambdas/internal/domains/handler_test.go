package domains

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/aws/aws-lambda-go/events"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
)

func request(routeKey, domain, body, groups string) events.APIGatewayV2HTTPRequest {
	req := events.APIGatewayV2HTTPRequest{RouteKey: routeKey, Body: body}
	if domain != "" {
		req.PathParameters = map[string]string{"domain": domain}
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

func serve(t *testing.T, e *env, req events.APIGatewayV2HTTPRequest) (int, map[string]any) {
	t.Helper()
	resp, err := (&Handler{Manager: e.m}).Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Headers["Content-Type"] != "application/json" {
		t.Errorf("Content-Type = %q", resp.Headers["Content-Type"])
	}
	var body map[string]any
	if resp.Body != "" {
		if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
			t.Fatalf("body %q: %v", resp.Body, err)
		}
	}
	return resp.StatusCode, body
}

func TestHandlerListForViewers(t *testing.T) {
	e := newEnv(t, zoneID)
	createExternal(t, e)
	status, body := serve(t, e, request(routeList, "", "", "[viewer]"))
	if status != 200 || body["count"] != 2.0 {
		t.Fatalf("list = %d %v", status, body)
	}
	list := body["domains"].([]any)
	if list[0].(map[string]any)["role"] != "primary" || list[1].(map[string]any)["domain"] != video {
		t.Errorf("domains = %v", list)
	}
	if zones := body["route53_zone_ids"].([]any); len(zones) != 1 || zones[0] != zoneID {
		t.Errorf("zones = %v", zones)
	}
}

func TestHandlerWritesNeedAdmin(t *testing.T) {
	for _, req := range []events.APIGatewayV2HTTPRequest{
		request(routeCreate, "", `{"domain":"x.example.org","dns_mode":"external","fallback_url":"https://x.xx"}`, "[viewer]"),
		request(routeCheck, video, "", "[viewer]"),
		request(routeUpdate, video, `{"fallback_url":"https://x.xx"}`, ""),
		request(routeDelete, video, "", "[viewer]"),
	} {
		e := newEnv(t)
		status, body := serve(t, e, req)
		if status != 403 || body["error"] != "Admin access required" {
			t.Errorf("%s: %d %v", req.RouteKey, status, body)
		}
		if e.certs.total() != 0 || len(e.ssm.Names()) != 1 {
			t.Errorf("%s by a viewer changed something", req.RouteKey)
		}
	}
}

func TestHandlerCreateCheckUpdateDelete(t *testing.T) {
	e := newEnv(t)
	status, body := serve(t, e, request(routeCreate, "", `{"domain":"video.example.org","dns_mode":"external","fallback_url":"https://example.org/"}`, "[admin]"))
	if status != 201 || body["status"] != settings.StatusPendingValidation || body["cert_arn"] != certARN(video) {
		t.Fatalf("create = %d %v", status, body)
	}

	e.certs.issue(video, CertIssued)
	status, body = serve(t, e, request(routeCheck, video, "", "[admin]"))
	if status != 200 || body["status"] != settings.StatusPendingDNS || body["target_domain"] != target(video) {
		t.Fatalf("check = %d %v", status, body)
	}

	status, body = serve(t, e, request(routeUpdate, video, `{"fallback_url":"https://example.org/x"}`, "[admin]"))
	if status != 200 || body["fallback_url"] != "https://example.org/x" {
		t.Fatalf("update = %d %v", status, body)
	}
	status, body = serve(t, e, request(routeUpdate, video, `{"status":"active"}`, "[admin]"))
	if status != 400 || body["error"] != "Only fallback_url can be changed" {
		t.Errorf("update status = %d %v", status, body)
	}

	e.addLink(video, "promo")
	status, body = serve(t, e, request(routeDelete, video, "", "[admin]"))
	if status != 409 || body["link_count"] != 1.0 {
		t.Fatalf("delete with link = %d %v", status, body)
	}
	_ = e.links.DeleteLink(ctx, video, "promo")
	if status, _ = serve(t, e, request(routeDelete, video, "", "[admin]")); status != 204 {
		t.Fatalf("delete = %d", status)
	}
	if status, _ = serve(t, e, request(routeCheck, video, "", "[admin]")); status != 404 {
		t.Errorf("check after delete = %d", status)
	}
}

func TestHandlerBadInput(t *testing.T) {
	e := newEnv(t)
	for _, b := range []string{`[1]`, `{"domain":1}`, `nope`} {
		status, _ := serve(t, e, request(routeCreate, "", b, "[admin]"))
		if status != 400 {
			t.Errorf("body %s: status %d", b, status)
		}
	}
	if status, _ := serve(t, e, request("GET /api/domains/{domain}", video, "", "[admin]")); status != 404 {
		t.Errorf("unknown route: %d", status)
	}
	e.ssm.SetErr(errSSM)
	if status, body := serve(t, e, request(routeList, "", "", "[admin]")); status != 500 || body["error"] != "Internal server error" {
		t.Errorf("store failure: %d %v", status, body)
	}
}

var errSSM = errors.New("ssm down")

func TestInvokeDispatchesScheduledEvent(t *testing.T) {
	e := newEnv(t)
	createExternal(t, e)
	e.certs.issue(video, CertIssued)
	h := &Handler{Manager: e.m}

	scheduled := `{"version":"0","source":"aws.events","detail-type":"Scheduled Event","detail":{}}`
	if _, err := h.Invoke(ctx, json.RawMessage(scheduled)); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, e.stored(t, video), settings.StatusPendingDNS)

	raw, _ := json.Marshal(request(routeList, "", "", "[viewer]"))
	out, err := h.Invoke(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if resp, ok := out.(events.APIGatewayV2HTTPResponse); !ok || resp.StatusCode != 200 {
		t.Errorf("API invoke = %#v", out)
	}
}
