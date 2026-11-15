// Command domains is the Lambda behind the /api/domains routes. EventBridge
// also invokes it every few minutes to advance pending domains.
package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/BiTy1024/serverless-short-link/lambdas/internal/awsdomains"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/domains"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/env"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/settings"
	"github.com/BiTy1024/serverless-short-link/lambdas/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "domains"))

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		panic(err)
	}
	h := &domains.Handler{Manager: &domains.Manager{
		Settings: &settings.Store{Client: ssm.NewFromConfig(cfg), Path: env.Require("DOMAINS_PARAMETER_PATH")},
		Certs:    &awsdomains.Certs{Client: acm.NewFromConfig(cfg), Stack: env.Require("STACK_NAME")},
		Gateway: &awsdomains.Gateway{
			Client: apigatewayv2.NewFromConfig(cfg),
			APIID:  env.Require("API_ID"),
			Stage:  env.Require("API_STAGE"),
			Stack:  env.Require("STACK_NAME"),
		},
		Zones:        &awsdomains.Zones{Client: route53.NewFromConfig(cfg)},
		Resolver:     net.DefaultResolver,
		Links:        &store.Dynamo{Client: dynamodb.NewFromConfig(cfg), LinksTable: env.Require("LINKS_TABLE_NAME")},
		Route53Zones: splitList(env.Require("ROUTE53_ZONE_IDS")),
		AdminDomain:  env.Require("ADMIN_DOMAIN"),
		Now:          time.Now,
		Sleep:        time.Sleep,
	}}
	lambda.Start(h.Invoke)
}

// splitList splits a comma-separated list, dropping empty entries.
func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
