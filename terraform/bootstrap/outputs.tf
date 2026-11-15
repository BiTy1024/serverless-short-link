output "state_bucket_name" {
  description = "S3 bucket holding Terraform state"
  value       = aws_s3_bucket.state.id
}

output "backend_config_snippet" {
  description = "Paste into terraform/backend.hcl for the root module"
  value       = <<-EOT
    bucket         = "${aws_s3_bucket.state.id}"
    key            = "short-link/terraform.tfstate"
    region         = "${var.aws_region}"
    use_lockfile   = true
    encrypt        = true
  EOT
}
