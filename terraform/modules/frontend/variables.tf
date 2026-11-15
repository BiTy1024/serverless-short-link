variable "stack_name" {
  type        = string
  description = "Resource name prefix"
}

variable "admin_domain" {
  type        = string
  description = "Fully qualified admin domain (admin.<primary domain>)"
}

variable "certificate_arn" {
  type        = string
  description = "ARN of the validated us-east-1 certificate for admin_domain"
}

variable "dns_mode" {
  type        = string
  description = "external or route53; route53 writes the alias record for admin_domain"
}

variable "hosted_zone_id" {
  type        = string
  description = "Route 53 hosted zone id; only used in route53 mode"
  default     = ""
}
