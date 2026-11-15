package login

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/cognito"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/testhttp"
)

type fakePrompter struct {
	user, pass       string
	sawUser, sawPass bool
}

func (p *fakePrompter) ReadLine(string) (string, error)   { p.sawUser = true; return p.user, nil }
func (p *fakePrompter) ReadSecret(string) (string, error) { p.sawPass = true; return p.pass, nil }

// cognitoStub accepts one user and answers USER_PASSWORD_AUTH.
func cognitoStub(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			AuthFlow       string
			AuthParameters map[string]string
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.AuthFlow != "USER_PASSWORD_AUTH" {
			t.Errorf("AuthFlow = %q", req.AuthFlow)
		}
		if req.AuthParameters["USERNAME"] != "user@example.test" || req.AuthParameters["PASSWORD"] != "pw" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"__type": "NotAuthorizedException", "message": "Incorrect username or password."})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"AuthenticationResult": map[string]any{
			"AccessToken": "access", "ExpiresIn": 3600, "RefreshToken": "refresh-token-value",
		}})
	})
}

func client(t *testing.T) cognito.Client {
	return cognito.Client{Region: "eu-central-1", ClientID: "mcp", Endpoint: "https://cognito.test/", HTTP: testhttp.Client(cognitoStub(t))}
}

func TestRunPrintsOnlyTheRefreshToken(t *testing.T) {
	var stdout, stderr bytes.Buffer
	p := &fakePrompter{user: " user@example.test ", pass: "pw"}
	if err := Run(context.Background(), client(t), p, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "refresh-token-value\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "secret set SHORTLINK_REFRESH_TOKEN") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "refresh-token-value") || strings.Contains(stderr.String(), "pw") {
		t.Fatal("stderr leaks a credential")
	}
	if !p.sawUser || !p.sawPass {
		t.Fatal("prompter not used")
	}
}

func TestRunWrongPasswordPrintsNothing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), client(t), &fakePrompter{user: "user@example.test", pass: "nope"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "NotAuthorizedException") {
		t.Fatalf("err = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunRequiresInput(t *testing.T) {
	var out bytes.Buffer
	if err := Run(context.Background(), client(t), &fakePrompter{pass: "pw"}, &out, &out); err == nil {
		t.Fatal("expected error for empty username")
	}
	if err := Run(context.Background(), client(t), &fakePrompter{user: "u"}, &out, &out); err == nil {
		t.Fatal("expected error for empty password")
	}
}
