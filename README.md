# Serverless Short Link Service

Serverless URL shortener with admin dashboard. For tracking usage of links, creating readable links and flexible integration with other websites while controlling the target. Built with Python, React, and DynamoDB on AWS — deployable via **SAM or Terraform**.

**Live demo**: [admin.short.bookpass.de](https://admin.short.bookpass.de)
View-only login: `demo@short.bookpass.de` / `demo1234`


## Setup

Pick one of two deployment paths. Both provision equivalent infrastructure.

### Option A — SAM (default)

```bash
cp samconfig.example.toml samconfig.toml  # Fill in your values
./deploy.sh                               # Deploys backend + frontend
```

**Prerequisites**: AWS CLI, SAM CLI, Python 3.12, Node.js

### Option B — Terraform

```bash
# One-time: bootstrap the remote state backend (S3 + DynamoDB lock)
cd terraform/bootstrap
terraform init && terraform apply -var "state_bucket_name=YOUR-UNIQUE-BUCKET"
# copy the printed backend_config_snippet into terraform/backend.hcl
cd ../..

cp terraform/envs/example.tfvars terraform/envs/myenv.tfvars  # fill in values
./deploy.sh --iac=terraform --config=terraform/envs/myenv.tfvars
```

**Prerequisites**: AWS CLI, Terraform >= 1.6, Python 3.12, Node.js

See [`terraform/README.md`](terraform/README.md) for the resource layout and trade-offs vs. SAM.

## API

All `/api/*` endpoints require `Authorization: Bearer <token>`. Redirects are public.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/links` | List all links |
| POST | `/api/links` | Create link (admin) |
| PUT | `/api/links/{path}` | Update link (admin) |
| DELETE | `/api/links/{path}` | Delete link (admin) |
| GET | `/api/stats` | Click stats overview |
| GET | `/api/stats/{path}` | Stats for one link |

Stats support `?days=N`, `?from=YYYY-MM-DD&to=YYYY-MM-DD`, and `?linked_only=true`.

## Architecture

```
                        ┌──────────────────────────────────────────────┐
                        │           API Gateway (HTTP API)             │
                        │           short.bookpass.de                  │
                        └────┬──────────┬──────────┬───────────────────┘
                             ▼          ▼          ▼
                    ┌───────────┐ ┌──────────┐ ┌───────────┐
                    │ Redirect  │ │  Links   │ │   Stats   │
                    │  Lambda   │ │  Lambda  │ │  Lambda   │
                    │ (public)  │ │  (auth)  │ │  (auth)   │
                    └─────┬─────┘ └────┬─────┘ └─────┬─────┘
                          ▼            ▼             ▼
                    ┌──────────────────────────────────────┐
                    │             DynamoDB                 │
                    │    LinksTable  │  RedirectStatsTable │
                    └──────────────────────────────────────┘

  ┌─────────────────────┐              ┌───────────────────┐
  │   CloudFront + S3   │              │  Cognito (JWT)    │
  │ admin.HOSTEDZONE    │              │  roles            │
  │  React + Tailwind   │              └───────────────────┘
  └─────────────────────┘
```

## Screenshots

### Login
![Login](assets/login.png)
### Dashbord
![Dashboard](assets/dashboard.png)
### Link Managment
![Links](assets/links.png)
### Link stats
![Link Detail](assets/link-detail.png)