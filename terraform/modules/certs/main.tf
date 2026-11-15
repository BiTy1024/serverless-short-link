# Certificates of the primary domain: a regional one for the API custom
# domain and a us-east-1 one for the admin frontend's CloudFront alias.
#
# This module only requests the certificates (and, in route53 mode, writes
# their validation records). Waiting for validation happens outside it, so
# `terraform apply -target=module.certs` returns at once with the records a
# person sets at an external DNS host.

terraform {
  required_providers {
    aws = {
      source                = "hashicorp/aws"
      configuration_aliases = [aws.us_east_1]
    }
  }
}

locals {
  admin_domain = "admin.${var.primary_domain}"
  route53      = var.dns_mode == "route53"
}

resource "aws_acm_certificate" "api" {
  domain_name       = var.primary_domain
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true

    precondition {
      condition     = !local.route53 || var.hosted_zone_id != ""
      error_message = "primary_hosted_zone_id is required when primary_dns_mode is \"route53\"."
    }
  }
}

resource "aws_acm_certificate" "admin" {
  provider = aws.us_east_1

  domain_name       = local.admin_domain
  validation_method = "DNS"

  lifecycle {
    create_before_destroy = true
  }
}

locals {
  api_validation_records = [
    for o in aws_acm_certificate.api.domain_validation_options : {
      name  = o.resource_record_name
      type  = o.resource_record_type
      value = o.resource_record_value
    }
  ]
  admin_validation_records = [
    for o in aws_acm_certificate.admin.domain_validation_options : {
      name  = o.resource_record_name
      type  = o.resource_record_type
      value = o.resource_record_value
    }
  ]
}

resource "aws_route53_record" "api_validation" {
  for_each = local.route53 ? {
    for o in aws_acm_certificate.api.domain_validation_options : o.domain_name => o
  } : {}

  zone_id         = var.hosted_zone_id
  name            = each.value.resource_record_name
  type            = each.value.resource_record_type
  records         = [each.value.resource_record_value]
  ttl             = 60
  allow_overwrite = true
}

resource "aws_route53_record" "admin_validation" {
  for_each = local.route53 ? {
    for o in aws_acm_certificate.admin.domain_validation_options : o.domain_name => o
  } : {}

  zone_id         = var.hosted_zone_id
  name            = each.value.resource_record_name
  type            = each.value.resource_record_type
  records         = [each.value.resource_record_value]
  ttl             = 60
  allow_overwrite = true
}
