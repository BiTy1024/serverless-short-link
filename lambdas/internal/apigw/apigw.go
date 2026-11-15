// Package apigw has the response and request helpers shared by the API
// Gateway HTTP API (payload 2.0) handlers.
package apigw

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/aws/aws-lambda-go/events"
)

// JSON builds a response with a JSON content type. A nil body leaves the
// response body empty.
func JSON(status int, body any) events.APIGatewayV2HTTPResponse {
	resp := events.APIGatewayV2HTTPResponse{
		StatusCode: status,
		Headers:    map[string]string{"Content-Type": "application/json"},
	}
	if body == nil {
		return resp
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		panic(err)
	}
	resp.Body = strings.TrimSuffix(buf.String(), "\n")
	return resp
}

// Error builds a JSON response with body {"error": msg}.
func Error(status int, msg string) events.APIGatewayV2HTTPResponse {
	return JSON(status, map[string]string{"error": msg})
}

// TrimSpace removes leading and trailing whitespace from request values. Next
// to unicode.IsSpace it treats the separators U+001C to U+001F as whitespace.
func TrimSpace(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
	})
}

// UserGroups reads the cognito:groups claim. The HTTP API JWT authorizer
// passes array claims as a string: "[admin viewer]" (space separated), a
// single group as "admin" or "[admin]". A JSON array string such as
// ["admin","viewer"] is accepted as well. Commas also separate groups.
func UserGroups(req events.APIGatewayV2HTTPRequest) []string {
	auth := req.RequestContext.Authorizer
	if auth == nil || auth.JWT == nil {
		return nil
	}
	claim := strings.TrimSpace(auth.JWT.Claims["cognito:groups"])
	if claim == "" {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(claim), &list); err == nil {
		return list
	}
	claim = strings.TrimSuffix(strings.TrimPrefix(claim, "["), "]")
	return strings.FieldsFunc(claim, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
}
