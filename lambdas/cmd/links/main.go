// Command links is the Lambda behind the /api/links routes.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/env"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/links"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "links"))

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	h := &links.Handler{
		Store: &store.Dynamo{
			Client:     dynamodb.NewFromConfig(cfg),
			LinksTable: env.Require("LINKS_TABLE_NAME"),
		},
		Now: time.Now,
	}
	lambda.Start(h.Handle)
}
