resource "aws_cognito_user_pool" "users" {
  name = "${var.stack_name}-users"

  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  admin_create_user_config {
    allow_admin_create_user_only = true
  }

  password_policy {
    minimum_length = 8
  }
}

resource "aws_cognito_user_group" "admin" {
  name         = "admin"
  user_pool_id = aws_cognito_user_pool.users.id
  description  = "Full access to all API operations"
}

resource "aws_cognito_user_group" "viewer" {
  name         = "viewer"
  user_pool_id = aws_cognito_user_pool.users.id
  description  = "Read-only access to links and statistics"
}

resource "aws_cognito_user_pool_client" "web" {
  name         = "${var.stack_name}-web"
  user_pool_id = aws_cognito_user_pool.users.id

  generate_secret = false

  explicit_auth_flows = [
    "ALLOW_USER_SRP_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_ADMIN_USER_PASSWORD_AUTH",
  ]
}

# App client for the MCP server. A person signs in once with
# USER_PASSWORD_AUTH (`shortlink-mcp login`) and stores the refresh token; the
# server then only uses REFRESH_TOKEN_AUTH. Rotation stays off so the stored
# refresh token remains valid for its full 90 days.
resource "aws_cognito_user_pool_client" "mcp" {
  name         = "${var.stack_name}-mcp"
  user_pool_id = aws_cognito_user_pool.users.id

  generate_secret = false

  explicit_auth_flows = [
    "ALLOW_USER_PASSWORD_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
  ]

  access_token_validity  = 60
  id_token_validity      = 60
  refresh_token_validity = 90

  token_validity_units {
    access_token  = "minutes"
    id_token      = "minutes"
    refresh_token = "days"
  }

  refresh_token_rotation {
    feature = "DISABLED"
  }
}
