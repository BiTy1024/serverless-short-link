// Package cognito obtains Cognito user-pool tokens for the MCP app client.
//
// A person signs in once with Login (USER_PASSWORD_AUTH) and stores the
// refresh token. TokenSource then exchanges that refresh token for access
// tokens with REFRESH_TOKEN_AUTH.
//
// Both calls go to the public Cognito Identity Provider JSON API. InitiateAuth
// is unauthenticated for app clients without a secret, so no AWS credentials
// are involved.
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

// Client identifies the Cognito app client.
type Client struct {
	Region   string
	ClientID string
	// Endpoint overrides https://cognito-idp.<Region>.amazonaws.com/.
	Endpoint string
	// HTTP is the client used for requests; nil means a 30s-timeout client.
	HTTP *http.Client
}

// ErrRefreshTokenRejected means the stored refresh token has expired or was
// revoked, and a person has to sign in again.
var ErrRefreshTokenRejected = errors.New("refresh token expired or revoked: run `shortlink-mcp login` and store the new token with `secret set SHORTLINK_REFRESH_TOKEN`")

// Login signs in with username and password (USER_PASSWORD_AUTH) and returns
// the refresh token.
func (c Client) Login(ctx context.Context, username, password string) (string, error) {
	var resp authResponse
	err := c.call(ctx, "InitiateAuth", map[string]any{
		"AuthFlow": "USER_PASSWORD_AUTH",
		"ClientId": c.ClientID,
		"AuthParameters": map[string]string{
			"USERNAME": username,
			"PASSWORD": password,
		},
	}, &resp)
	if err != nil {
		return "", err
	}
	if resp.AuthenticationResult == nil {
		if resp.ChallengeName == "NEW_PASSWORD_REQUIRED" {
			return "", errors.New("cognito: the user must set a new password first; sign in once through the admin frontend")
		}
		return "", fmt.Errorf("cognito: challenge %q is not supported", resp.ChallengeName)
	}
	if resp.AuthenticationResult.RefreshToken == "" {
		return "", errors.New("cognito: sign-in returned no refresh token")
	}
	return resp.AuthenticationResult.RefreshToken, nil
}

// TokenSource hands out access tokens obtained with a refresh token, cached
// until refreshMargin before they expire. Safe for concurrent use.
type TokenSource struct {
	client       Client
	refreshToken string
	now          func() time.Time

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

// NewTokenSource returns a TokenSource for the given refresh token.
func NewTokenSource(c Client, refreshToken string) (*TokenSource, error) {
	if c.Region == "" || c.ClientID == "" || refreshToken == "" {
		return nil, errors.New("cognito: region, client ID and refresh token are required")
	}
	return &TokenSource{client: c, refreshToken: refreshToken, now: time.Now}, nil
}

// Token returns a valid access token.
func (s *TokenSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessToken != "" && s.now().Add(refreshMargin).Before(s.expiresAt) {
		return s.accessToken, nil
	}
	var resp authResponse
	err := s.client.call(ctx, "InitiateAuth", map[string]any{
		"AuthFlow":       "REFRESH_TOKEN_AUTH",
		"ClientId":       s.client.ClientID,
		"AuthParameters": map[string]string{"REFRESH_TOKEN": s.refreshToken},
	}, &resp)
	if err != nil {
		var ce *Error
		if errors.As(err, &ce) && ce.Type == "NotAuthorizedException" {
			return "", ErrRefreshTokenRejected
		}
		return "", err
	}
	if resp.AuthenticationResult == nil || resp.AuthenticationResult.AccessToken == "" {
		return "", fmt.Errorf("cognito: refresh returned no access token (challenge %q)", resp.ChallengeName)
	}
	s.accessToken = resp.AuthenticationResult.AccessToken
	s.expiresAt = s.now().Add(time.Duration(resp.AuthenticationResult.ExpiresIn) * time.Second)
	return s.accessToken, nil
}

// Invalidate drops the cached access token.
func (s *TokenSource) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessToken = ""
	s.expiresAt = time.Time{}
}

type authResult struct {
	AccessToken  string `json:"AccessToken"`
	ExpiresIn    int    `json:"ExpiresIn"`
	RefreshToken string `json:"RefreshToken"`
}

type authResponse struct {
	AuthenticationResult *authResult `json:"AuthenticationResult"`
	ChallengeName        string      `json:"ChallengeName"`
}

// Error is an error response from Cognito. It never contains request data.
type Error struct {
	Type    string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("cognito: %s: %s", e.Type, e.Message) }

func (c Client) call(ctx context.Context, op string, in, out any) error {
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/", c.Region)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.1")
	req.Header.Set("X-Amz-Target", "AWSCognitoIdentityProviderService."+op)
	resp, err := hc.Do(req)
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
		if e.Type == "InvalidParameterException" {
			e.Message += " (check that SHORTLINK_COGNITO_CLIENT_ID is the MCP app client, which allows USER_PASSWORD_AUTH and REFRESH_TOKEN_AUTH)"
		}
		return &Error{Type: e.Type, Message: e.Message}
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("cognito %s: decoding response: %w", op, err)
	}
	return nil
}
