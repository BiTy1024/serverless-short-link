variable "primary_domain" {
  type        = string
  description = "Primary domain; the API is served there and the admin frontend at admin.<primary_domain>"
}

variable "dns_mode" {
  type        = string
  description = "external (records are set by hand) or route53 (records are written to hosted_zone_id)"
}

variable "hosted_zone_id" {
  type        = string
  description = "Route 53 hosted zone for the validation records; only used in route53 mode"
  default     = ""
}
