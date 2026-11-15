// Package cognito signs a user in to a Cognito user pool with USER_SRP_AUTH
// and keeps the resulting access token fresh with REFRESH_TOKEN_AUTH.
//
// It calls the public Cognito Identity Provider JSON API directly. InitiateAuth
// and RespondToAuthChallenge are unauthenticated operations for app clients
// without a secret, so no AWS credentials are involved.
package cognito

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// refreshMargin is how long before expiry a cached access token is renewed.
const refreshMargin = 2 * time.Minute

// Config identifies the user pool, app client and user.
type Config struct {
	Region     string
	UserPoolID string
	ClientID   string
	Username   string
	Password   string
	// Endpoint overrides https://cognito-idp.<Region>.amazonaws.com/.
	Endpoint string
}

// TokenSource hands out Cognito access tokens. It signs in on first use,
// renews with the refresh token shortly before expiry, and falls back to a
// full sign-in when the refresh token is rejected. Safe for concurrent use.
type TokenSource struct {
	cfg  Config
	http *http.Client
	now  func() time.Time

	mu           sync.Mutex
	accessToken  string
	refreshToken string
	expiresAt    time.Time
}

// NewTokenSource returns a TokenSource. A nil httpClient uses a client with
// a 30s timeout.
func NewTokenSource(cfg Config, httpClient *http.Client) (*TokenSource, error) {
	if cfg.Region == "" || cfg.UserPoolID == "" || cfg.ClientID == "" || cfg.Username == "" || cfg.Password == "" {
		return nil, errors.New("cognito: region, user pool ID, client ID, username and password are all required")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/", cfg.Region)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &TokenSource{cfg: cfg, http: httpClient, now: time.Now}, nil
}

// Token returns a valid access token.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessToken != "" && s.now().Add(refreshMargin).Before(s.expiresAt) {
		return s.accessToken, nil
	}
	if s.refreshToken != "" {
		err := s.refresh(ctx)
		if err == nil {
			return s.accessToken, nil
		}
		var ce *Error
		if !errors.As(err, &ce) || ce.Type != "NotAuthorizedException" {
			return "", err
		}
		// Refresh token expired or revoked: sign in again.
		s.refreshToken = ""
	}
	if err := s.signIn(ctx); err != nil {
		return "", err
	}
	return s.accessToken, nil
}

// Invalidate drops the cached access token; the refresh token is kept.
func (s *TokenSource) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessToken = ""
	s.expiresAt = time.Time{}
}

func (s *TokenSource) signIn(ctx context.Context) error {
	srp, err := newSRPClient(s.cfg.UserPoolID)
	if err != nil {
		return err
	}
	var init authResponse
	err = s.call(ctx, "InitiateAuth", map[string]any{
		"AuthFlow": "USER_SRP_AUTH",
		"ClientId": s.cfg.ClientID,
		"AuthParameters": map[string]string{
			"USERNAME": s.cfg.Username,
			"SRP_A":    srp.srpAHex(),
		},
	}, &init)
	if err != nil {
		return err
	}
	if init.ChallengeName != "PASSWORD_VERIFIER" {
		return unsupportedChallenge(init.ChallengeName)
	}

	responses, err := srp.passwordVerifier(init.ChallengeParameters, s.cfg.Password, s.now())
	if err != nil {
		return err
	}
	var final authResponse
	err = s.call(ctx, "RespondToAuthChallenge", map[string]any{
		"ChallengeName":      "PASSWORD_VERIFIER",
		"ClientId":           s.cfg.ClientID,
		"Session":            init.Session,
		"ChallengeResponses": responses,
	}, &final)
	if err != nil {
		return err
	}
	if final.AuthenticationResult == nil {
		return unsupportedChallenge(final.ChallengeName)
	}
	s.store(final.AuthenticationResult)
	return nil
}

func (s *TokenSource) refresh(ctx context.Context) error {
	var resp authResponse
	err := s.call(ctx, "InitiateAuth", map[string]any{
		"AuthFlow":       "REFRESH_TOKEN_AUTH",
		"ClientId":       s.cfg.ClientID,
		"AuthParameters": map[string]string{"REFRESH_TOKEN": s.refreshToken},
	}, &resp)
	if err != nil {
		return err
	}
	if resp.AuthenticationResult == nil {
		return unsupportedChallenge(resp.ChallengeName)
	}
	s.store(resp.AuthenticationResult)
	return nil
}

func (s *TokenSource) store(r *authResult) {
	s.accessToken = r.AccessToken
	s.expiresAt = s.now().Add(time.Duration(r.ExpiresIn) * time.Second)
	// REFRESH_TOKEN_AUTH returns a new refresh token only when rotation is on.
	if r.RefreshToken != "" {
		s.refreshToken = r.RefreshToken
	}
}

func unsupportedChallenge(name string) error {
	if name == "NEW_PASSWORD_REQUIRED" {
		return errors.New("cognito: the user must set a new password first; sign in once through the admin frontend")
	}
	if name == "" {
		return errors.New("cognito: response contained neither tokens nor a challenge")
	}
	return fmt.Errorf("cognito: challenge %s is not supported by this client", name)
}

type authResult struct {
	AccessToken  string `json:"AccessToken"`
	ExpiresIn    int    `json:"ExpiresIn"`
	RefreshToken string `json:"RefreshToken"`
}

type authResponse struct {
	AuthenticationResult *authResult       `json:"AuthenticationResult"`
	ChallengeName        string            `json:"ChallengeName"`
	ChallengeParameters  map[string]string `json:"ChallengeParameters"`
	Session              string            `json:"Session"`
}

// Error is an error response from Cognito. It never contains request data.
type Error struct {
	Type    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("cognito: %s: %s", e.Type, e.Message) }

func (s *TokenSource) call(ctx context.Context, op string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "AWSCognitoIdentityProviderService."+op)
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("cognito %s: %w", op, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("cognito %s: reading response: %w", op, err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Type    string `json:"__type"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(b, &e)
		// The type may carry a namespace prefix: "aws.cognito#NotAuthorizedException".
		if i := strings.LastIndexByte(e.Type, '#'); i >= 0 {
			e.Type = e.Type[i+1:]
		}
		if e.Type == "" {
			e.Type = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		if e.Type == "InvalidParameterException" && op == "InitiateAuth" {
			e.Message += " (check that the app client allows USER_SRP_AUTH and REFRESH_TOKEN_AUTH)"
		}
		return &Error{Type: e.Type, Message: e.Message}
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("cognito %s: decoding response: %w", op, err)
	}
	return nil
}
