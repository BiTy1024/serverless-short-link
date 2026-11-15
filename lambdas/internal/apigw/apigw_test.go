package apigw

import (
	"strings"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func withGroups(groups string) events.APIGatewayV2HTTPRequest {
	var req events.APIGatewayV2HTTPRequest
	if groups != "" {
		req.RequestContext.Authorizer = &events.APIGatewayV2HTTPRequestContextAuthorizerDescription{
			JWT: &events.APIGatewayV2HTTPRequestContextAuthorizerJWTDescription{
				Claims: map[string]string{"cognito:groups": groups},
			},
		}
	}
	return req
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
		got := UserGroups(withGroups(claim))
		if strings.Join(got, "|") != strings.Join(want, "|") || len(got) != len(want) {
			t.Errorf("%q: got %q, want %q", claim, got, want)
		}
	}
	if got := UserGroups(withGroups("")); got != nil {
		t.Errorf("no claim: got %q", got)
	}
}
