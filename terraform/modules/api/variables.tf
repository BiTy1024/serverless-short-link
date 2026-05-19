variable "stack_name" {
  type        = string
  description = "Resource name prefix"
}

variable "aws_region" {
  type        = string
  description = "AWS region (used for the Cognito JWT issuer URL)"
}

variable "domain_name" {
  type        = string
  description = "Custom domain for the redirect service"
}

variable "hosted_zone_id" {
  type        = string
  description = "Route53 hosted zone id"
}

variable "admin_origin" {
  type        = string
  description = "Origin allowed by CORS (https://admin.<domain>)"
}

variable "user_pool_id" {
  type        = string
  description = "Cognito User Pool ID for the JWT authorizer issuer"
}

variable "user_pool_client_id" {
  type        = string
  description = "Cognito User Pool Client ID used as JWT audience"
}

variable "redirect_lambda_invoke_arn" {
  type = string
}

variable "redirect_lambda_name" {
  type = string
}

variable "links_lambda_invoke_arn" {
  type = string
}

variable "links_lambda_name" {
  type = string
}

variable "stats_lambda_invoke_arn" {
  type = string
}

variable "stats_lambda_name" {
  type = string
}
