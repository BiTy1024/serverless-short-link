output "links_table_name" {
  value = aws_dynamodb_table.links.name
}

output "links_table_arn" {
  value = aws_dynamodb_table.links.arn
}

output "stats_table_name" {
  value = aws_dynamodb_table.stats.name
}

output "stats_table_arn" {
  value = aws_dynamodb_table.stats.arn
}
