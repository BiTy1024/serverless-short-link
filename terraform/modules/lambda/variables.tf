variable "function_name" {
  type        = string
  description = "Lambda function name"
}

variable "description" {
  type        = string
  description = "Function description"
}

variable "source_dir" {
  type        = string
  description = "Directory containing handler.py and requirements.txt"
}

variable "service_name" {
  type        = string
  description = "POWERTOOLS_SERVICE_NAME env var"
}

variable "environment_variables" {
  type        = map(string)
  description = "Additional Lambda environment variables (merged with Powertools defaults)"
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
