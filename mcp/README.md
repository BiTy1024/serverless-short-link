# shortlink-mcp

An [MCP](https://modelcontextprotocol.io) server that manages short links
through the service's HTTP management API (`/api/links`, `/api/stats`). It is a
thin client: validation, storage and permissions stay in the API. It runs
locally over stdio.

## Tools

| Tool | API call | Notes |
|---|---|---|
| `list_links` | `GET /api/links` | substring filter `contains`, paging with `limit`/`offset` (applied client-side) |
| `get_link` | `GET /api/links` | the API has no single-link endpoint; picks the slug from the list |
| `create_link` | `POST /api/links` | `admin` only; 409 if the slug exists |
| `update_link` | `PUT /api/links/{slug}` | `admin` only; replaces the target URL |
| `delete_link` | `DELETE /api/links/{slug}` | `admin` only; marked destructive; click stats are kept |
| `get_stats_overview` | `GET /api/stats` | `linked_only` defaults to `true`, as in the admin frontend |
| `get_link_stats` | `GET /api/stats/{slug}` | optional `days`, or `from`/`to` as `YYYY-MM-DD` |

Slug input is validated with the API's own rules before any request: 1–64
characters of `a-z`, `0-9` and single hyphens, starting and ending with a
letter or digit, and not one of `api`, `admin`, `health`, `status`.

## Authentication

The API sits behind an API Gateway JWT authorizer. The MCP server uses its own
Cognito app client (Terraform output `mcp_client_id`), which the authorizer
accepts alongside the admin frontend's client. That client allows
`USER_PASSWORD_AUTH` and `REFRESH_TOKEN_AUTH`, issues 60-minute access tokens
and 90-day refresh tokens, and does not rotate refresh tokens.

1. A person runs `shortlink-mcp login` once. It prompts for username and
   password on the terminal (the password without echo, via `stty -echo`),
   signs in and prints only the refresh token to stdout.
2. The refresh token goes into the secret broker as `SHORTLINK_REFRESH_TOKEN`.
3. The server exchanges it for access tokens, keeps the current one in memory
   and renews it two minutes before it expires. It never sees the password.

The refresh token expires after **90 days**, or earlier when it is revoked
(for example by a global sign-out). Tool calls then fail with "refresh token
expired or revoked: run `shortlink-mcp login`"; repeat steps 1 and 2 and
restart the server.

The user's Cognito group decides what works: `viewer` can use the read tools,
`admin` all tools. A user still in `FORCE_CHANGE_PASSWORD` state must sign in
once through the admin frontend first. Tokens and passwords are never logged,
and `login` never writes the token to a file.

## Configuration

| Variable | Used by | Value |
|---|---|---|
| `SHORTLINK_API_BASE` | server | base URL of the API, e.g. `https://go.example.com` |
| `SHORTLINK_COGNITO_REGION` | server, login | region of the user pool, e.g. `eu-central-1` |
| `SHORTLINK_COGNITO_CLIENT_ID` | server, login | MCP app client ID (Terraform output `mcp_client_id`) |
| `SHORTLINK_REFRESH_TOKEN` | server | refresh token printed by `shortlink-mcp login` |

`login` also takes `-region` and `-client-id` flags instead of the variables.

## Build

```sh
cd mcp
go build -o shortlink-mcp ./cmd/shortlink-mcp
go test ./...
```

## Sign in and register in Claude Code

Get a refresh token and store it in the local secret broker (every 90 days):

```sh
shortlink-mcp login -region <region> -client-id <mcp-client-id>
secret set SHORTLINK_REFRESH_TOKEN   # paste the token printed above
```

Register the server; `secret run` injects the secret as the environment
variable of the same name into the server process only, and the non-secret
settings go into the registration directly:

```sh
claude mcp add shortlink \
  -e SHORTLINK_API_BASE=https://go.example.com \
  -e SHORTLINK_COGNITO_REGION=<region> \
  -e SHORTLINK_COGNITO_CLIENT_ID=<mcp-client-id> \
  -- secret run SHORTLINK_REFRESH_TOKEN -- /absolute/path/to/shortlink-mcp
```

After a re-login only the secret changes; the registration stays as it is.

## Layout

- `cmd/shortlink-mcp` — entry point and environment configuration
- `internal/shortlink` — the API client; every path and request/response shape lives in `client.go`
- `internal/cognito` — password sign-in and refresh-token exchange
- `internal/login` — the `login` subcommand and its terminal prompts
- `internal/tools` — MCP tool definitions
- `internal/shortlink/fakeapi`, `internal/testhttp` — in-process fakes for tests
