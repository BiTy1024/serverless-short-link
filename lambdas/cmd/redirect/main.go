// Command redirect is the Lambda behind every non-API route: it resolves the
// slug on the requested domain and answers with a 301.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/domain"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/env"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/redirect"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "redirect"))

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	defaultURL := env.Require("DEFAULT_REDIRECT_URL")
	h := &redirect.Handler{
		Store: &store.Dynamo{
			Client:     dynamodb.NewFromConfig(cfg),
			LinksTable: env.Require("LINKS_TABLE_NAME"),
			StatsTable: env.Require("STATS_TABLE_NAME"),
		},
		Domains:            domain.Static{FallbackURL: defaultURL},
		DefaultRedirectURL: defaultURL,
		AdminOrigin:        env.Require("ADMIN_ORIGIN"),
		Now:                time.Now,
	}
	lambda.Start(h.Handle)
}
