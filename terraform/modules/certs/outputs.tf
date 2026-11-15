output "api_certificate_arn" {
  value = aws_acm_certificate.api.arn
}

output "admin_certificate_arn" {
  value = aws_acm_certificate.admin.arn
}

output "admin_domain" {
  value = local.admin_domain
}

output "api_validation_records" {
  description = "DNS validation records of the regional API certificate"
  value       = local.api_validation_records
}

output "admin_validation_records" {
  description = "DNS validation records of the us-east-1 admin certificate"
  value       = local.admin_validation_records
}

output "api_validation_fqdns" {
  description = "Validation record names written to Route 53; empty in external mode"
  value       = [for r in aws_route53_record.api_validation : r.fqdn]
}

output "admin_validation_fqdns" {
  description = "Validation record names written to Route 53; empty in external mode"
  value       = [for r in aws_route53_record.admin_validation : r.fqdn]
}
