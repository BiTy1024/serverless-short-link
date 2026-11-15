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
