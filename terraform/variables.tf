variable "aws_region" {
  type        = string
  description = "AWS region for the primary stack"
  default     = "eu-central-1"
}

variable "stack_name" {
  type        = string
  description = "Resource name prefix (mirrors SAM stack_name)"
}

variable "domain_name" {
  type        = string
  description = "Custom domain for the redirect service (e.g. short.example.de)"
}

variable "hosted_zone_id" {
  type        = string
  description = "Route53 hosted zone id for the parent domain"
}

variable "certificate_arn" {
  type        = string
  description = "ACM certificate ARN in var.aws_region for var.domain_name"
}

variable "default_redirect_url" {
  type        = string
  description = "Fallback URL when no mapping is found"
}

variable "accent_color" {
  type        = string
  description = "Accent color for the admin frontend (hex)"
  default     = "#2A1058"
}
