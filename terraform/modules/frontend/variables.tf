variable "stack_name" {
  type        = string
  description = "Resource name prefix"
}

variable "admin_domain" {
  type        = string
  description = "Fully qualified admin domain (admin.<domain>)"
}

variable "hosted_zone_id" {
  type        = string
  description = "Route53 hosted zone id used for the cert validation + alias record"
}
