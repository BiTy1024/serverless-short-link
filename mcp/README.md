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

The API sits behind an API Gateway JWT authorizer for the Cognito web app
client. The server signs in as a Cognito user with `USER_SRP_AUTH` (the flow
the admin frontend uses, so the existing app client needs no change), keeps the
access token in memory, renews it with `REFRESH_TOKEN_AUTH` two minutes before
it expires, and signs in again if the refresh token is rejected. The password
is never sent to Cognito, and neither tokens nor the password are logged.

The user's Cognito group decides what works: `viewer` can use the read tools,
`admin` all tools. A user still in `FORCE_CHANGE_PASSWORD` state must sign in
once through the admin frontend first.

## Configuration

| Variable | Required | Value |
|---|---|---|
| `SHORTLINK_API_BASE` | yes | base URL of the API, e.g. `https://go.example.com` |
| `SHORTLINK_COGNITO_USER_POOL_ID` | yes | user pool ID, `<region>_<id>` (Terraform output `user_pool_id`) |
| `SHORTLINK_COGNITO_CLIENT_ID` | yes | web app client ID (Terraform output `user_pool_client_id`) |
| `SHORTLINK_COGNITO_REGION` | no | defaults to the region in the user pool ID |
| `SHORTLINK_USERNAME` | yes | the user's email address |
| `SHORTLINK_PASSWORD` | yes | the user's password |

The user pool ID is needed in addition to the client ID because SRP mixes the
pool name into the password proof.

## Build

```sh
cd mcp
go build -o shortlink-mcp ./cmd/shortlink-mcp
go test ./...
```

## Register in Claude Code

Keep the username and password in the local secret broker, stored under the
same names as the environment variables; `secret run` injects each secret as
an environment variable of that name into the server process only:

```sh
secret set SHORTLINK_USERNAME
secret set SHORTLINK_PASSWORD
```

The non-secret settings go into the registration directly:

```sh
claude mcp add shortlink \
  -e SHORTLINK_API_BASE=https://go.example.com \
  -e SHORTLINK_COGNITO_USER_POOL_ID=<region>_<pool-id> \
  -e SHORTLINK_COGNITO_CLIENT_ID=<app-client-id> \
  -- secret run SHORTLINK_USERNAME,SHORTLINK_PASSWORD -- /absolute/path/to/shortlink-mcp
```

To manage more than one deployment, store the credentials under other names
and register a second server; the names given to `secret run` must match the
variable names the server reads.

## Layout

- `cmd/shortlink-mcp` — entry point and environment configuration
- `internal/shortlink` — the API client; every path and request/response shape lives in `client.go`
- `internal/cognito` — SRP sign-in and token refresh
- `internal/tools` — MCP tool definitions
- `internal/shortlink/fakeapi`, `internal/testhttp` — in-process fakes for tests
