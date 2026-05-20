output "bucket_name" {
  value = aws_s3_bucket.admin.id
}

output "distribution_id" {
  value = aws_cloudfront_distribution.admin.id
}

output "distribution_domain_name" {
  value = aws_cloudfront_distribution.admin.domain_name
}

output "certificate_arn" {
  value = aws_acm_certificate.admin.arn
}
