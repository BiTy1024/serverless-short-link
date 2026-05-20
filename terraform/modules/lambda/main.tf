module "function" {
  source  = "terraform-aws-modules/lambda/aws"
  version = "~> 7.0"

  function_name = var.function_name
  description   = var.description
  handler       = "handler.lambda_handler"
  runtime       = "python3.12"
  architectures = ["arm64"]
  timeout       = 10
  memory_size   = 256

  source_path = [{
    path             = var.source_dir
    pip_requirements = true
  }]

  environment_variables = merge(
    {
      POWERTOOLS_SERVICE_NAME = var.service_name
      POWERTOOLS_LOG_LEVEL    = "INFO"
    },
    var.environment_variables,
  )

  attach_policy_statements = length(var.iam_statements) > 0
  policy_statements = {
    for idx, stmt in var.iam_statements :
    "stmt${idx}" => {
      effect    = stmt.effect
      actions   = stmt.actions
      resources = stmt.resources
    }
  }

  cloudwatch_logs_retention_in_days = 7
}
