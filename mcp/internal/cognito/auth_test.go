package cognito

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/testhttp"
)

const (
	testPoolID   = "eu-central-1_TestPool"
	testClientID = "test-client-id"
	testUser     = "user@example.test"
	testUserID   = "11111111-2222-3333-4444-555555555555"
	testPassword = "not-a-real-password"
)

// fakeCognito implements the server side of USER_SRP_AUTH and
// REFRESH_TOKEN_AUTH independently of the client code: it stores only the
// password verifier v = g^x and checks the client's signature against the
// shared secret computed as S = (A * v^u)^b.
type fakeCognito struct {
	t        *testing.T
	salt     *big.Int
	verifier *big.Int

	mu            sync.Mutex
	b, B          *big.Int
	A             *big.Int
	secretBlock   []byte
	issued        int
	refreshCalls  int
	signIns       int
	rejectRefresh bool
	expiresIn     int
}

func newFakeCognito(t *testing.T) *fakeCognito {
	salt := randInt(t, 16)
	x := passwordHash("TestPool", testUserID, testPassword, salt)
	return &fakeCognito{
		t:         t,
		salt:      salt,
		verifier:  new(big.Int).Exp(srpG, x, srpN),
		expiresIn: 3600,
	}
}

func randInt(t *testing.T, n int) *big.Int {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return new(big.Int).SetBytes(buf)
}

func (f *fakeCognito) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if ct := r.Header.Get("Content-Type"); ct != "application/x-amz-json-1.1" {
		f.t.Errorf("Content-Type = %q", ct)
	}
	var req struct {
		AuthFlow           string
		ClientId           string
		AuthParameters     map[string]string
		ChallengeName      string
		ChallengeResponses map[string]string
		Session            string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Fatalf("decode: %v", err)
	}
	if req.ClientId != testClientID {
		f.fail(w, "ResourceNotFoundException", "unknown client")
		return
	}

	switch r.Header.Get("X-Amz-Target") {
	case "AWSCognitoIdentityProviderService.InitiateAuth":
		switch req.AuthFlow {
		case "USER_SRP_AUTH":
			f.initiateSRP(w, req.AuthParameters)
		case "REFRESH_TOKEN_AUTH":
			f.refreshCalls++
			if f.rejectRefresh || !strings.HasPrefix(req.AuthParameters["REFRESH_TOKEN"], "refresh-") {
				f.fail(w, "NotAuthorizedException", "Invalid Refresh Token")
				return
			}
			f.issued++
			writeJSON(w, map[string]any{"AuthenticationResult": map[string]any{
				"AccessToken": fmt.Sprintf("access-%d", f.issued), "ExpiresIn": f.expiresIn,
			}})
		default:
			f.fail(w, "InvalidParameterException", "flow not enabled")
		}
	case "AWSCognitoIdentityProviderService.RespondToAuthChallenge":
		f.verify(w, req.ChallengeName, req.Session, req.ChallengeResponses)
	default:
		f.fail(w, "UnknownOperationException", "")
	}
}

func (f *fakeCognito) initiateSRP(w http.ResponseWriter, p map[string]string) {
	if p["USERNAME"] != testUser {
		f.fail(w, "NotAuthorizedException", "Incorrect username or password.")
		return
	}
	A, ok := new(big.Int).SetString(p["SRP_A"], 16)
	if !ok || new(big.Int).Mod(A, srpN).Sign() == 0 {
		f.fail(w, "InvalidParameterException", "bad SRP_A")
		return
	}
	f.A = A
	f.b = randInt(f.t, 128)
	// B = k*v + g^b mod N
	f.B = new(big.Int).Mul(srpK, f.verifier)
	f.B.Add(f.B, new(big.Int).Exp(srpG, f.b, srpN))
	f.B.Mod(f.B, srpN)
	f.secretBlock = randInt(f.t, 64).Bytes()
	writeJSON(w, map[string]any{
		"ChallengeName": "PASSWORD_VERIFIER",
		"Session":       "session-1",
		"ChallengeParameters": map[string]string{
			"USER_ID_FOR_SRP": testUserID,
			"SRP_B":           f.B.Text(16),
			"SALT":            f.salt.Text(16),
			"SECRET_BLOCK":    base64.StdEncoding.EncodeToString(f.secretBlock),
		},
	})
}

func (f *fakeCognito) verify(w http.ResponseWriter, name, session string, r map[string]string) {
	if name != "PASSWORD_VERIFIER" || session != "session-1" || r["USERNAME"] != testUserID {
		f.fail(w, "InvalidParameterException", "unexpected challenge response")
		return
	}
	if _, err := time.Parse("Mon Jan 2 15:04:05 UTC 2006", r["TIMESTAMP"]); err != nil {
		f.fail(w, "InvalidParameterException", "bad TIMESTAMP")
		return
	}
	u := hashHexToInt(padHex(f.A) + padHex(f.B))
	// S = (A * v^u)^b mod N
	S := new(big.Int).Exp(f.verifier, u, srpN)
	S.Mul(S, f.A).Mod(S, srpN)
	S.Exp(S, f.b, srpN)
	key := hkdf(mustHex(padHex(S)), mustHex(padHex(u)))

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("TestPool" + testUserID))
	mac.Write(f.secretBlock)
	mac.Write([]byte(r["TIMESTAMP"]))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if r["PASSWORD_CLAIM_SIGNATURE"] != want {
		f.fail(w, "NotAuthorizedException", "Incorrect username or password.")
		return
	}
	f.signIns++
	f.issued++
	writeJSON(w, map[string]any{"AuthenticationResult": map[string]any{
		"AccessToken":  fmt.Sprintf("access-%d", f.issued),
		"ExpiresIn":    f.expiresIn,
		"RefreshToken": fmt.Sprintf("refresh-%d", f.issued),
	}})
}

func (f *fakeCognito) fail(w http.ResponseWriter, typ, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	writeJSON(w, map[string]string{"__type": typ, "message": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

func newSource(t *testing.T, hc *http.Client, password string) *TokenSource {
	t.Helper()
	s, err := NewTokenSource(Config{
		Region: "eu-central-1", UserPoolID: testPoolID, ClientID: testClientID,
		Username: testUser, Password: password, Endpoint: "https://cognito.test/",
	}, hc)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSignInWithSRP(t *testing.T) {
	fake := newFakeCognito(t)
	srv := testhttp.Client(fake)

	s := newSource(t, srv, testPassword)
	tok, err := s.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "access-1" {
		t.Fatalf("token = %q, want access-1", tok)
	}
	// Cached on the second call.
	if tok, _ = s.Token(context.Background()); tok != "access-1" || fake.signIns != 1 {
		t.Fatalf("expected cached token, got %q after %d sign-ins", tok, fake.signIns)
	}
}

func TestSignInWrongPassword(t *testing.T) {
	srv := testhttp.Client(newFakeCognito(t))

	_, err := newSource(t, srv, "wrong-password").Token(context.Background())
	var ce *Error
	if !errors.As(err, &ce) || ce.Type != "NotAuthorizedException" {
		t.Fatalf("err = %v, want NotAuthorizedException", err)
	}
	if strings.Contains(err.Error(), "wrong-password") {
		t.Fatal("error message leaks the password")
	}
}

func TestRefreshBeforeExpiry(t *testing.T) {
	fake := newFakeCognito(t)
	srv := testhttp.Client(fake)

	s := newSource(t, srv, testPassword)
	now := time.Now()
	s.now = func() time.Time { return now }
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Inside the refresh margin: the refresh token is used, no new sign-in.
	now = now.Add(time.Hour - refreshMargin + time.Second)
	tok, err := s.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "access-2" || fake.refreshCalls != 1 || fake.signIns != 1 {
		t.Fatalf("tok=%q refreshCalls=%d signIns=%d", tok, fake.refreshCalls, fake.signIns)
	}
}

func TestRejectedRefreshFallsBackToSignIn(t *testing.T) {
	fake := newFakeCognito(t)
	srv := testhttp.Client(fake)

	s := newSource(t, srv, testPassword)
	if _, err := s.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.rejectRefresh = true
	s.Invalidate()
	tok, err := s.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fake.refreshCalls != 1 || fake.signIns != 2 || tok != "access-2" {
		t.Fatalf("tok=%q refreshCalls=%d signIns=%d", tok, fake.refreshCalls, fake.signIns)
	}
}

func TestInvalidPoolID(t *testing.T) {
	s, err := NewTokenSource(Config{
		Region: "eu-central-1", UserPoolID: "nounderscore", ClientID: testClientID,
		Username: testUser, Password: testPassword, Endpoint: "http://127.0.0.1:1",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Token(context.Background()); err == nil || !strings.Contains(err.Error(), "user pool ID") {
		t.Fatalf("err = %v", err)
	}
}

func TestFormatTimestamp(t *testing.T) {
	got := formatTimestamp(time.Date(2026, 9, 5, 3, 4, 5, 0, time.FixedZone("CEST", 7200)))
	if want := "Sat Sep 5 01:04:05 UTC 2026"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestPadHex(t *testing.T) {
	for in, want := range map[int64]string{0x1: "01", 0x7f: "7f", 0x80: "0080", 0xabc: "0abc", 0x8abc: "008abc"} {
		if got := padHex(big.NewInt(in)); got != want {
			t.Errorf("padHex(%#x) = %q, want %q", in, got, want)
		}
	}
}
