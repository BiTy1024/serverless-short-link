variable "aws_region" {
  type        = string
  description = "AWS region for the primary stack"
  default     = "eu-central-1"
}

variable "stack_name" {
  type        = string
  description = "Resource name prefix"
}

variable "primary_domain" {
  type        = string
  description = "Primary domain: serves the management API and short links (e.g. short.example.de); the admin frontend is served at admin.<primary_domain>"
}

variable "primary_dns_mode" {
  type        = string
  description = "How the primary domain's DNS records are set: external (by hand at a third-party DNS host) or route53 (written to primary_hosted_zone_id)"

  validation {
    condition     = contains(["external", "route53"], var.primary_dns_mode)
    error_message = "primary_dns_mode must be \"external\" or \"route53\"."
  }
}

variable "primary_hosted_zone_id" {
  type        = string
  description = "Route 53 hosted zone of the primary domain; required in route53 mode, ignored in external mode"
  default     = ""
}

variable "route53_zone_ids" {
  type        = list(string)
  description = "Route 53 hosted zones secondary domains may use in route53 mode; the domains Lambda may write records only there. Empty disables route53 mode for secondary domains."
  default     = []
}

variable "default_redirect_url" {
  type        = string
  description = "Fallback URL of the primary domain, and of every domain while the domain settings cannot be read"
}

variable "accent_color" {
  type        = string
  description = "Accent color for the admin frontend (hex)"
  default     = "#2A1058"
}
