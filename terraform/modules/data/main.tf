# Links are keyed by (domain, slug): a slug is unique per domain, not globally.
resource "aws_dynamodb_table" "links" {
  name         = "${var.stack_name}-links"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "domain"
  range_key    = "slug"

  attribute {
    name = "domain"
    type = "S"
  }

  attribute {
    name = "slug"
    type = "S"
  }

  tags = {
    Name = "${var.stack_name}-links"
  }

  lifecycle {
    prevent_destroy = true
  }
}

# One row per click, keyed by link_key = "<domain>#<slug>" and timestamp.
# The by-domain index serves the per-domain stats overview as a Query; the
# all-domains overview scans the table.
resource "aws_dynamodb_table" "stats" {
  name         = "${var.stack_name}-redirect-stats"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "link_key"
  range_key    = "timestamp"

  attribute {
    name = "link_key"
    type = "S"
  }

  attribute {
    name = "timestamp"
    type = "S"
  }

  attribute {
    name = "domain"
    type = "S"
  }

  global_secondary_index {
    name            = local.stats_by_domain_index
    hash_key        = "domain"
    range_key       = "timestamp"
    projection_type = "ALL"
  }

  tags = {
    Name = "${var.stack_name}-redirect-stats"
  }

  lifecycle {
    prevent_destroy = true
  }
}

locals {
  # Must match store.StatsByDomainIndex in the Go code.
  stats_by_domain_index = "by-domain"
}
