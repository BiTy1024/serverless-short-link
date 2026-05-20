output "state_bucket_name" {
  description = "S3 bucket holding Terraform state"
  value       = aws_s3_bucket.state.id
}

output "lock_table_name" {
  description = "DynamoDB table used for state locking"
  value       = aws_dynamodb_table.locks.id
}

output "backend_config_snippet" {
  description = "Paste into terraform/backend.hcl for the root module"
  value       = <<-EOT
    bucket         = "${aws_s3_bucket.state.id}"
    key            = "short-link/terraform.tfstate"
    region         = "${var.aws_region}"
    dynamodb_table = "${aws_dynamodb_table.locks.id}"
    encrypt        = true
  EOT
}
