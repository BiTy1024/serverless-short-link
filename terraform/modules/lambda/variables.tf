variable "function_name" {
  type        = string
  description = "Lambda function name"
}

variable "description" {
  type        = string
  description = "Function description"
}

variable "binary_path" {
  type        = string
  description = "Path to the compiled linux/arm64 bootstrap binary"
}

variable "environment_variables" {
  type        = map(string)
  description = "Lambda environment variables"
  default     = {}
}

variable "iam_statements" {
  type = list(object({
    effect    = string
    actions   = list(string)
    resources = list(string)
  }))
  description = "Inline IAM policy statements attached to the Lambda execution role"
  default     = []
}
