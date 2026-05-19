output "function_name" {
  value = module.function.lambda_function_name
}

output "function_arn" {
  value = module.function.lambda_function_arn
}

output "invoke_arn" {
  value = module.function.lambda_function_invoke_arn
}
