output "api_endpoint" {
  description = "API Gateway endpoint URL"
  value       = module.api.api_endpoint
}

output "custom_domain_url" {
  description = "Custom domain URL for the redirect service"
  value       = "https://${var.domain_name}"
}

output "admin_domain_url" {
  description = "Admin frontend URL"
  value       = "https://${local.admin_domain}"
}

output "admin_bucket_name" {
  description = "S3 bucket for admin frontend"
  value       = module.frontend.bucket_name
}

output "admin_distribution_id" {
  description = "CloudFront distribution ID for admin frontend"
  value       = module.frontend.distribution_id
}

output "user_pool_id" {
  description = "Cognito User Pool ID"
  value       = module.auth.user_pool_id
}

output "user_pool_client_id" {
  description = "Cognito User Pool Client ID"
  value       = module.auth.user_pool_client_id
}

output "accent_color" {
  description = "Accent color for the admin frontend"
  value       = var.accent_color
}

output "links_table_name" {
  description = "DynamoDB table name for links"
  value       = module.data.links_table_name
}

output "stats_table_name" {
  description = "DynamoDB table name for redirect statistics"
  value       = module.data.stats_table_name
}
