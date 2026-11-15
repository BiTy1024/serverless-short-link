// Command shortlink-mcp is an MCP server over stdio that manages short links
// through the short-link service's HTTP management API.
//
//	shortlink-mcp         run the MCP server (configuration: see config.go)
//	shortlink-mcp login   sign in once and print a refresh token
//
// Logs go to stderr, since stdout carries the MCP protocol. Tokens and
// passwords are never logged.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/cognito"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/login"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/shortlink"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/tools"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetFlags(0)
	log.SetPrefix("shortlink-mcp: ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch {
	case len(os.Args) > 1 && os.Args[1] == "login":
		err = runLogin(ctx, os.Args[2:])
	case len(os.Args) > 1:
		err = fmt.Errorf("unknown argument %q; usage: shortlink-mcp [login]", os.Args[1])
	default:
		err = runServer(ctx)
	}
	if err != nil {
		stop()
		log.Fatal(err)
	}
}

func runServer(ctx context.Context) error {
	cfg, err := serverConfigFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	tokens, err := cognito.NewTokenSource(cognito.Client{Region: cfg.Region, ClientID: cfg.ClientID}, cfg.RefreshToken)
	if err != nil {
		return err
	}
	client := shortlink.NewClient(cfg.APIBase, tokens, nil)

	server := mcp.NewServer(&mcp.Implementation{Name: "shortlink", Version: version}, &mcp.ServerOptions{
		Instructions: tools.Instructions,
	})
	if err := tools.Register(server, client); err != nil {
		return fmt.Errorf("registering tools: %w", err)
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}

func runLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	cfg := loginConfig{}
	fs.StringVar(&cfg.Region, "region", os.Getenv(envRegion), "Cognito region (default $"+envRegion+")")
	fs.StringVar(&cfg.ClientID, "client-id", os.Getenv(envClientID), "MCP app client ID (default $"+envClientID+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	tty, err := login.OpenTTY()
	if err != nil {
		return err
	}
	defer tty.Close()
	return login.Run(ctx, cognito.Client{Region: cfg.Region, ClientID: cfg.ClientID}, tty, os.Stdout, os.Stderr)
}
