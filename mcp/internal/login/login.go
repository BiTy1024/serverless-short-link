// Package login implements `shortlink-mcp login`: it asks for username and
// password, signs in to the MCP app client and prints the refresh token.
package login

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/cognito"
)

// Prompter reads the credentials from a person.
type Prompter interface {
	// ReadLine shows prompt and reads a line with echo.
	ReadLine(prompt string) (string, error)
	// ReadSecret shows prompt and reads a line without echo.
	ReadSecret(prompt string) (string, error)
}

// Run signs in and writes only the refresh token, followed by a newline, to
// stdout, so the output can be piped into a secret store. The hint on what
// to do with it goes to stderr. The token is never written to a file.
func Run(ctx context.Context, c cognito.Client, p Prompter, stdout, stderr io.Writer) error {
	username, err := p.ReadLine("Username (email): ")
	if err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return errors.New("username is required")
	}
	password, err := p.ReadSecret("Password: ")
	if err != nil {
		return err
	}
	if password == "" {
		return errors.New("password is required")
	}

	token, err := c.Login(ctx, username, password)
	if err != nil {
		return err
	}
	fmt.Fprintln(stderr, "Signed in. Store the refresh token below with `secret set SHORTLINK_REFRESH_TOKEN`; it is valid for 90 days.")
	_, err = fmt.Fprintln(stdout, token)
	return err
}
