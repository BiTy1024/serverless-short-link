// Command shortlink-mcp is an MCP server over stdio that manages short links
// through the short-link service's HTTP management API.
//
// Configuration comes from the environment; see config.go. Logs go to
// stderr, since stdout carries the MCP protocol. Neither tokens nor the
// password are ever logged.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/BiTy1024/serverless-short-link/mcp/internal/cognito"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/shortlink"
	"github.com/BiTy1024/serverless-short-link/mcp/internal/tools"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	log.SetFlags(0)
	log.SetPrefix("shortlink-mcp: ")
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := configFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	tokens, err := cognito.NewTokenSource(cognito.Config{
		Region:     cfg.Region,
		UserPoolID: cfg.UserPoolID,
		ClientID:   cfg.ClientID,
		Username:   cfg.Username,
		Password:   cfg.Password,
	}, nil)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, &mcp.StdioTransport{})
}
