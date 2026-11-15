# Zips the prebuilt bootstrap binary; building it is not Terraform's job
# (see lambdas/Makefile, run by deploy.sh).
data "archive_file" "package" {
  type             = "zip"
  source_file      = var.binary_path
  output_path      = "${var.binary_path}.zip"
  output_file_mode = "0755"
}

module "function" {
  source  = "terraform-aws-modules/lambda/aws"
  version = "~> 7.0"

  function_name = var.function_name
  description   = var.description
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  architectures = ["arm64"]
  timeout       = var.timeout
  memory_size   = 256

  create_package         = false
  local_existing_package = data.archive_file.package.output_path

  environment_variables = var.environment_variables

  attach_policy_statements = length(var.iam_statements) > 0
  policy_statements = {
    for idx, stmt in var.iam_statements :
    "stmt${idx}" => {
      effect    = stmt.effect
      actions   = stmt.actions
      resources = stmt.resources
      condition = stmt.conditions
    }
  }

  cloudwatch_logs_retention_in_days = 7
}
