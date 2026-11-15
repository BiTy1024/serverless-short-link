data "aws_caller_identity" "current" {}

locals {
  admin_domain       = module.certs.admin_domain
  admin_origin       = "https://${local.admin_domain}"
  primary_route53    = var.primary_dns_mode == "route53"
  domains_path       = "/${var.stack_name}/domains"
  domains_param_arn  = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter${local.domains_path}"
  acm_cert_arns      = "arn:aws:acm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:certificate/*"
  apigateway_domains = "arn:aws:apigateway:${var.aws_region}::/domainnames"
  # Tag key the domains Lambda puts on the certificates and custom domains
  # it creates; it only deletes those that carry it with this stack's name.
  stack_tag = "short-link-stack"
  # Conditions for actions that set tags: exactly the stack tag, with this
  # stack's name.
  stack_tag_request = [
    {
      test     = "StringEquals"
      variable = "aws:RequestTag/${local.stack_tag}"
      values   = [var.stack_name]
    },
    {
      test     = "ForAllValues:StringEquals"
      variable = "aws:TagKeys"
      values   = [local.stack_tag]
    },
  ]
}

module "data" {
  source     = "./modules/data"
  stack_name = var.stack_name
}

module "auth" {
  source     = "./modules/auth"
  stack_name = var.stack_name
}

# --- Certificates of the primary domain ---
# `terraform apply -target=module.certs` requests them without waiting for
# validation; see README for the two-phase deployment with external DNS.

module "certs" {
  source = "./modules/certs"

  providers = {
    aws.us_east_1 = aws.us_east_1
  }

  primary_domain = var.primary_domain
  dns_mode       = var.primary_dns_mode
  hosted_zone_id = var.primary_hosted_zone_id
}

resource "aws_acm_certificate_validation" "api" {
  certificate_arn         = module.certs.api_certificate_arn
  validation_record_fqdns = local.primary_route53 ? module.certs.api_validation_fqdns : null
}

resource "aws_acm_certificate_validation" "admin" {
  provider = aws.us_east_1

  certificate_arn         = module.certs.admin_certificate_arn
  validation_record_fqdns = local.primary_route53 ? module.certs.admin_validation_fqdns : null
}

# --- Lambdas ---

module "lambda_redirect" {
  source = "./modules/lambda"

  function_name = "${var.stack_name}-redirect"
  description   = "Handles URL redirects based on DynamoDB lookup"
  binary_path   = "${path.module}/../lambdas/build/redirect/bootstrap"

  environment_variables = {
    LINKS_TABLE_NAME       = module.data.links_table_name
    DEFAULT_REDIRECT_URL   = var.default_redirect_url
    STATS_TABLE_NAME       = module.data.stats_table_name
    ADMIN_ORIGIN           = local.admin_origin
    DOMAINS_PARAMETER_PATH = local.domains_path
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
    {
      effect    = "Allow"
      actions   = ["ssm:GetParametersByPath"]
      resources = [local.domains_param_arn]
    },
  ]
}

module "lambda_links" {
  source = "./modules/lambda"

  function_name = "${var.stack_name}-links-api"
  description   = "CRUD API for managing short links"
  binary_path   = "${path.module}/../lambdas/build/links/bootstrap"

  environment_variables = {
    LINKS_TABLE_NAME       = module.data.links_table_name
    DOMAINS_PARAMETER_PATH = local.domains_path
  }

  iam_statements = [
    {
      effect = "Allow"
      actions = [
        "dynamodb:PutItem",
        "dynamodb:GetItem",
        "dynamodb:DeleteItem",
        "dynamodb:Query",
        "dynamodb:Scan",
        "dynamodb:UpdateItem",
      ]
      resources = [module.data.links_table_arn]
    },
    {
      effect    = "Allow"
      actions   = ["ssm:GetParameter"]
      resources = ["${local.domains_param_arn}/*"]
    },
  ]
}

module "lambda_stats" {
  source = "./modules/lambda"

  function_name = "${var.stack_name}-stats-api"
  description   = "API for querying click statistics"
  binary_path   = "${path.module}/../lambdas/build/stats/bootstrap"

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
      actions   = ["dynamodb:Query"]
      resources = [module.data.stats_by_domain_index_arn]
    },
    {
      effect    = "Allow"
      actions   = ["dynamodb:Query", "dynamodb:Scan"]
      resources = [module.data.links_table_arn]
    },
  ]
}

module "lambda_domains" {
  source = "./modules/lambda"

  function_name = "${var.stack_name}-domains-api"
  description   = "Settings API for secondary domains and their periodic setup check"
  binary_path   = "${path.module}/../lambdas/build/domains/bootstrap"
  # A delete waits for API Gateway to release the certificate.
  timeout = 60

  environment_variables = {
    DOMAINS_PARAMETER_PATH = local.domains_path
    STACK_NAME             = var.stack_name
    API_ID                 = module.api.api_id
    # The literal stage name: the stage depends on the routes, which depend
    # on this function, so referencing the stage would form a cycle.
    API_STAGE        = "$default"
    LINKS_TABLE_NAME = module.data.links_table_name
    ROUTE53_ZONE_IDS = join(",", var.route53_zone_ids)
    ADMIN_DOMAIN     = local.admin_domain
  }

  iam_statements = concat([
    {
      effect = "Allow"
      actions = [
        "ssm:GetParametersByPath",
        "ssm:GetParameter",
        "ssm:PutParameter",
        "ssm:DeleteParameter",
      ]
      resources = [local.domains_param_arn, "${local.domains_param_arn}/*"]
    },
    {
      effect    = "Allow"
      actions   = ["dynamodb:Query"]
      resources = [module.data.links_table_arn]
    },
    {
      # RequestCertificate has no resource ARN before the certificate exists.
      # The request must carry exactly the stack tag.
      effect    = "Allow"
      actions   = ["acm:RequestCertificate"]
      resources = ["*"]
      conditions = concat(local.stack_tag_request, [{
        test     = "StringEquals"
        variable = "acm:ValidationMethod"
        values   = ["DNS"]
      }])
    },
    {
      # Tagging is only needed for the tags of RequestCertificate. Limited to
      # the stack tag with this stack's name, so the tag condition on
      # DeleteCertificate cannot be met by tagging a foreign certificate
      # with another stack's name.
      effect     = "Allow"
      actions    = ["acm:AddTagsToCertificate"]
      resources  = [local.acm_cert_arns]
      conditions = local.stack_tag_request
    },
    {
      effect = "Allow"
      actions = [
        "acm:DescribeCertificate",
        "acm:ListTagsForCertificate",
      ]
      resources = [local.acm_cert_arns]
    },
    {
      effect    = "Allow"
      actions   = ["acm:DeleteCertificate"]
      resources = [local.acm_cert_arns]
      conditions = [{
        test     = "StringEquals"
        variable = "aws:ResourceTag/${local.stack_tag}"
        values   = [var.stack_name]
      }]
    },
    {
      effect    = "Allow"
      actions   = ["apigateway:GET", "apigateway:POST", "apigateway:DELETE"]
      resources = [local.apigateway_domains, "${local.apigateway_domains}/*"]
    },
    {
      # CreateDomainName and DeleteDomainName attach and detach the
      # certificate under these actions.
      effect    = "Allow"
      actions   = ["apigateway:AddCertificateToDomain", "apigateway:RemoveCertificateFromDomain"]
      resources = ["${local.apigateway_domains}/*"]
    },
    {
      # Tags given to CreateDomainName; only the stack tag. API Gateway
      # authorizes them as apigateway:PUT on the tags resource.
      effect     = "Allow"
      actions    = ["apigateway:POST", "apigateway:PUT"]
      resources  = ["arn:aws:apigateway:${var.aws_region}::/tags/*"]
      conditions = local.stack_tag_request
    },
    ], length(var.route53_zone_ids) == 0 ? [] : [
    {
      effect    = "Allow"
      actions   = ["route53:ChangeResourceRecordSets", "route53:ListResourceRecordSets"]
      resources = [for id in var.route53_zone_ids : "arn:aws:route53:::hostedzone/${id}"]
    },
  ])
}

# --- Periodic check of pending secondary domains ---

resource "aws_cloudwatch_event_rule" "domains_check" {
  name                = "${var.stack_name}-domains-check"
  description         = "Advances pending secondary domains"
  schedule_expression = "rate(5 minutes)"
}

resource "aws_cloudwatch_event_target" "domains_check" {
  rule = aws_cloudwatch_event_rule.domains_check.name
  arn  = module.lambda_domains.function_arn
}

resource "aws_lambda_permission" "domains_check" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = module.lambda_domains.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.domains_check.arn
}

# --- API ---

module "api" {
  source = "./modules/api"

  stack_name          = var.stack_name
  aws_region          = var.aws_region
  domain_name         = var.primary_domain
  certificate_arn     = aws_acm_certificate_validation.api.certificate_arn
  dns_mode            = var.primary_dns_mode
  hosted_zone_id      = var.primary_hosted_zone_id
  admin_origin        = local.admin_origin
  user_pool_id        = module.auth.user_pool_id
  user_pool_client_id = module.auth.user_pool_client_id

  additional_jwt_audiences = [module.auth.mcp_client_id]

  redirect_lambda_invoke_arn = module.lambda_redirect.invoke_arn
  redirect_lambda_name       = module.lambda_redirect.function_name

  links_lambda_invoke_arn = module.lambda_links.invoke_arn
  links_lambda_name       = module.lambda_links.function_name

  stats_lambda_invoke_arn = module.lambda_stats.invoke_arn
  stats_lambda_name       = module.lambda_stats.function_name

  domains_lambda_invoke_arn = module.lambda_domains.invoke_arn
  domains_lambda_name       = module.lambda_domains.function_name
}

module "frontend" {
  source = "./modules/frontend"

  stack_name      = var.stack_name
  admin_domain    = local.admin_domain
  certificate_arn = aws_acm_certificate_validation.admin.certificate_arn
  dns_mode        = var.primary_dns_mode
  hosted_zone_id  = var.primary_hosted_zone_id
}

# --- Setting of the primary domain ---
# The redirect serves only domains with an active setting. The primary's
# setting is owned by Terraform; the domains API refuses to change it.

locals {
  primary_validation_records = concat(module.certs.api_validation_records, module.certs.admin_validation_records)
  # The records a person sets at an external DNS host once phase 2 is done.
  primary_dns_records = [
    { name = var.primary_domain, type = "CNAME", value = module.api.target_domain_name },
    { name = local.admin_domain, type = "CNAME", value = module.frontend.distribution_domain_name },
  ]
}

resource "aws_ssm_parameter" "primary_domain" {
  name = "${local.domains_path}/${var.primary_domain}"
  type = "String"
  tier = "Standard"
  value = jsonencode({
    domain             = var.primary_domain
    role               = "primary"
    dns_mode           = var.primary_dns_mode
    hosted_zone_id     = var.primary_hosted_zone_id
    fallback_url       = var.default_redirect_url
    status             = "active"
    cert_arn           = aws_acm_certificate_validation.api.certificate_arn
    target_domain      = module.api.target_domain_name
    validation_records = local.primary_validation_records
    dns_records        = local.primary_route53 ? [] : local.primary_dns_records
  })
}
