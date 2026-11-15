package cognito

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/testhttp"
)

const (
	testClientID = "test-client-id"
	testUser     = "user@example.test"
	testPassword = "not-a-real-password"
)

// fakeCognito answers InitiateAuth for USER_PASSWORD_AUTH and
// REFRESH_TOKEN_AUTH the way Cognito does for a client without a secret.
type fakeCognito struct {
	t *testing.T

	mu           sync.Mutex
	issued       int
	refreshCalls int
	revoked      bool
	challenge    string
	expiresIn    int
}

func newFakeCognito(t *testing.T) *fakeCognito {
	return &fakeCognito{t: t, expiresIn: 3600}
}

func (f *fakeCognito) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if ct := r.Header.Get("Content-Type"); ct != "application/x-amz-json-1.1" {
		f.t.Errorf("Content-Type = %q", ct)
	}
	if tgt := r.Header.Get("X-Amz-Target"); tgt != "AWSCognitoIdentityProviderService.InitiateAuth" {
		f.fail(w, "UnknownOperationException", tgt)
		return
	}
	var req struct {
		AuthFlow       string
		ClientId       string
		AuthParameters map[string]string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Fatalf("decode: %v", err)
	}
	if req.ClientId != testClientID {
		f.fail(w, "ResourceNotFoundException", "User pool client does not exist.")
		return
	}

	switch req.AuthFlow {
	case "USER_PASSWORD_AUTH":
		if req.AuthParameters["USERNAME"] != testUser || req.AuthParameters["PASSWORD"] != testPassword {
			f.fail(w, "NotAuthorizedException", "Incorrect username or password.")
			return
		}
		if f.challenge != "" {
			writeJSON(w, map[string]any{"ChallengeName": f.challenge, "Session": "s"})
			return
		}
		f.issued++
		writeJSON(w, map[string]any{"AuthenticationResult": map[string]any{
			"AccessToken":  fmt.Sprintf("access-%d", f.issued),
			"ExpiresIn":    f.expiresIn,
			"RefreshToken": "refresh-valid",
		}})
	case "REFRESH_TOKEN_AUTH":
		f.refreshCalls++
		if f.revoked || req.AuthParameters["REFRESH_TOKEN"] != "refresh-valid" {
			f.fail(w, "NotAuthorizedException", "Refresh Token has been revoked")
			return
		}
		f.issued++
		// No RefreshToken in the response: rotation is off.
		writeJSON(w, map[string]any{"AuthenticationResult": map[string]any{
			"AccessToken": fmt.Sprintf("access-%d", f.issued), "ExpiresIn": f.expiresIn,
		}})
	default:
		f.fail(w, "InvalidParameterException", "Auth flow not enabled for this client")
	}
}

func (f *fakeCognito) fail(w http.ResponseWriter, typ, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, map[string]string{"__type": typ, "message": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

func testClient(f *fakeCognito) Client {
	return Client{Region: "eu-central-1", ClientID: testClientID, Endpoint: "https://cognito.test/", HTTP: testhttp.Client(f)}
}

func TestLoginReturnsRefreshToken(t *testing.T) {
	c := testClient(newFakeCognito(t))
	tok, err := c.Login(context.Background(), testUser, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "refresh-valid" {
		t.Fatalf("refresh token = %q", tok)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	c := testClient(newFakeCognito(t))
	_, err := c.Login(context.Background(), testUser, "wrong-password")
	var ce *Error
	if !errors.As(err, &ce) || ce.Type != "NotAuthorizedException" {
		t.Fatalf("err = %v, want NotAuthorizedException", err)
	}
	if strings.Contains(err.Error(), "wrong-password") {
		t.Fatal("error message leaks the password")
	}
}

func TestLoginNewPasswordRequired(t *testing.T) {
	f := newFakeCognito(t)
	f.challenge = "NEW_PASSWORD_REQUIRED"
	_, err := testClient(f).Login(context.Background(), testUser, testPassword)
	if err == nil || !strings.Contains(err.Error(), "new password") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginWrongClientFlow(t *testing.T) {
	c := testClient(newFakeCognito(t))
	c.ClientID = "web-client"
	if _, err := c.Login(context.Background(), testUser, testPassword); err == nil {
		t.Fatal("expected error for unknown client")
	}
}

func TestTokenCachedAndRenewedBeforeExpiry(t *testing.T) {
	f := newFakeCognito(t)
	s, err := NewTokenSource(testClient(f), "refresh-valid")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }

	tok, err := s.Token(context.Background())
	if err != nil || tok != "access-1" {
		t.Fatalf("Token = %q, %v", tok, err)
	}
	now = now.Add(time.Hour - refreshMargin - time.Second)
	if tok, _ = s.Token(context.Background()); tok != "access-1" || f.refreshCalls != 1 {
		t.Fatalf("expected cached token, got %q after %d refreshes", tok, f.refreshCalls)
	}
	now = now.Add(2 * time.Second) // inside the refresh margin
	if tok, _ = s.Token(context.Background()); tok != "access-2" || f.refreshCalls != 2 {
		t.Fatalf("expected renewed token, got %q after %d refreshes", tok, f.refreshCalls)
	}
}

func TestInvalidateForcesRefresh(t *testing.T) {
	f := newFakeCognito(t)
	s, _ := NewTokenSource(testClient(f), "refresh-valid")
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.Invalidate()
	if tok, err := s.Token(context.Background()); err != nil || tok != "access-2" {
		t.Fatalf("Token = %q, %v", tok, err)
	}
}

func TestRevokedRefreshTokenAsksForLogin(t *testing.T) {
	f := newFakeCognito(t)
	f.revoked = true
	s, _ := NewTokenSource(testClient(f), "refresh-valid")
	_, err := s.Token(context.Background())
	if !errors.Is(err, ErrRefreshTokenRejected) {
		t.Fatalf("err = %v, want ErrRefreshTokenRejected", err)
	}
	if !strings.Contains(err.Error(), "run `shortlink-mcp login`") {
		t.Fatalf("message %q does not tell the user to log in", err)
	}
}

func TestNewTokenSourceRequiresRefreshToken(t *testing.T) {
	if _, err := NewTokenSource(Client{Region: "eu-central-1", ClientID: testClientID}, ""); err == nil {
		t.Fatal("expected error")
	}
}
