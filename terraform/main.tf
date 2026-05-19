locals {
  admin_domain = "admin.${var.domain_name}"
}

module "data" {
  source     = "./modules/data"
  stack_name = var.stack_name
}

module "auth" {
  source     = "./modules/auth"
  stack_name = var.stack_name
}

module "lambda_redirect" {
  source = "./modules/lambda"

  function_name = "${var.stack_name}-redirect"
  description   = "Handles URL redirects based on DynamoDB lookup"
  source_dir    = "${path.module}/../src/redirect"
  service_name  = "redirect"

  environment_variables = {
    LINKS_TABLE_NAME     = module.data.links_table_name
    DEFAULT_REDIRECT_URL = var.default_redirect_url
    STATS_TABLE_NAME     = module.data.stats_table_name
    ADMIN_ORIGIN         = "https://${local.admin_domain}"
  }

  iam_statements = [
    {
      effect    = "Allow"
      actions   = ["dynamodb:GetItem"]
      resources = [module.data.links_table_arn]
    },
    {
      effect    = "Allow"
      actions   = ["dynamodb:PutItem"]
      resources = [module.data.stats_table_arn]
    },
  ]
}

module "lambda_links" {
  source = "./modules/lambda"

  function_name = "${var.stack_name}-links-api"
  description   = "CRUD API for managing short links"
  source_dir    = "${path.module}/../src/links"
  service_name  = "links"

  environment_variables = {
    LINKS_TABLE_NAME = module.data.links_table_name
  }

  iam_statements = [
    {
      effect = "Allow"
      actions = [
        "dynamodb:PutItem",
        "dynamodb:GetItem",
        "dynamodb:DeleteItem",
        "dynamodb:Scan",
        "dynamodb:UpdateItem",
      ]
      resources = [module.data.links_table_arn]
    },
  ]
}

module "lambda_stats" {
  source = "./modules/lambda"

  function_name = "${var.stack_name}-stats-api"
  description   = "API for querying click statistics"
  source_dir    = "${path.module}/../src/stats"
  service_name  = "stats"

  environment_variables = {
    STATS_TABLE_NAME = module.data.stats_table_name
    LINKS_TABLE_NAME = module.data.links_table_name
  }

  iam_statements = [
    {
      effect    = "Allow"
      actions   = ["dynamodb:Query", "dynamodb:Scan"]
      resources = [module.data.stats_table_arn]
    },
    {
      effect    = "Allow"
      actions   = ["dynamodb:Scan"]
      resources = [module.data.links_table_arn]
    },
  ]
}

module "api" {
  source = "./modules/api"

  stack_name          = var.stack_name
  aws_region          = var.aws_region
  domain_name         = var.domain_name
  hosted_zone_id      = var.hosted_zone_id
  admin_origin        = "https://${local.admin_domain}"
  user_pool_id        = module.auth.user_pool_id
  user_pool_client_id = module.auth.user_pool_client_id

  redirect_lambda_invoke_arn = module.lambda_redirect.invoke_arn
  redirect_lambda_name       = module.lambda_redirect.function_name

  links_lambda_invoke_arn = module.lambda_links.invoke_arn
  links_lambda_name       = module.lambda_links.function_name

  stats_lambda_invoke_arn = module.lambda_stats.invoke_arn
  stats_lambda_name       = module.lambda_stats.function_name
}

module "frontend" {
  source = "./modules/frontend"

  providers = {
    aws.us_east_1 = aws.us_east_1
  }

  stack_name     = var.stack_name
  admin_domain   = local.admin_domain
  hosted_zone_id = var.hosted_zone_id
}
