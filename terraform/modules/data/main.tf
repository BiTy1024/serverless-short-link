resource "aws_dynamodb_table" "links" {
  name         = "${var.stack_name}-links"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "short_path"

  attribute {
    name = "short_path"
    type = "S"
  }

  tags = {
    Name = "${var.stack_name}-links"
  }

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_dynamodb_table" "stats" {
  name         = "${var.stack_name}-redirect-stats"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "redirect_path"
  range_key    = "timestamp"

  attribute {
    name = "redirect_path"
    type = "S"
  }

  attribute {
    name = "timestamp"
    type = "S"
  }

  tags = {
    Name = "${var.stack_name}-redirect-stats"
  }

  lifecycle {
    prevent_destroy = true
  }
}
