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
  description = "Primary domain, the API's custom domain"
}

variable "certificate_arn" {
  type        = string
  description = "ARN of the validated regional certificate for domain_name"
}

variable "dns_mode" {
  type        = string
  description = "external or route53; route53 writes the alias record for domain_name"
}

variable "hosted_zone_id" {
  type        = string
  description = "Route 53 hosted zone id; only used in route53 mode"
  default     = ""
}

variable "admin_origin" {
  type        = string
  description = "Origin allowed by CORS (https://admin.<primary domain>)"
}

variable "user_pool_id" {
  type        = string
  description = "Cognito User Pool ID for the JWT authorizer issuer"
}

variable "user_pool_client_id" {
  type        = string
  description = "Cognito User Pool Client ID used as JWT audience"
}

variable "additional_jwt_audiences" {
  type        = list(string)
  description = "Further Cognito app client IDs whose tokens the API accepts"
  default     = []
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

variable "domains_lambda_invoke_arn" {
  type = string
}

variable "domains_lambda_name" {
  type = string
}
