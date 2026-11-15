output "api_id" {
  value = aws_apigatewayv2_api.this.id
}

output "api_endpoint" {
  description = "Default execute-api endpoint"
  value       = aws_apigatewayv2_api.this.api_endpoint
}

output "target_domain_name" {
  description = "Regional API Gateway domain the primary domain points at"
  value       = aws_apigatewayv2_domain_name.this.domain_name_configuration[0].target_domain_name
}
