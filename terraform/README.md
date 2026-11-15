# Terraform Deployment

Provisions the whole stack: Lambdas, HTTP API, DynamoDB, Cognito, S3, CloudFront, the certificates of the primary domain, its SSM domain setting and, in route53 mode, its Route 53 records.

## Prerequisites

- Go and make, to build the Lambda binaries (`make -C lambdas build` from the repo root) before `plan`/`apply`
- Terraform >= 1.11
- AWS CLI configured for the target account
- In route53 mode: the Route 53 hosted zone of the primary domain. In external mode: access to the DNS host to set CNAME records by hand

## One-time: bootstrap remote state

The root module uses an S3 backend with S3 lock files (`use_lockfile`, Terraform >= 1.11). Provision the bucket once:

```bash
cd terraform/bootstrap
terraform init
terraform apply -var "state_bucket_name=YOUR-GLOBALLY-UNIQUE-BUCKET"
# Note the backend_config_snippet output
```

Copy the snippet output into `terraform/backend.hcl` (gitignored), or pass the values via `-backend-config=...` on `terraform init`.

An existing setup with a DynamoDB lock table switches in this order: replace `dynamodb_table` with `use_lockfile = true` in `backend.hcl` and run `terraform init -reconfigure` in `terraform/` first, then `terraform apply` in `terraform/bootstrap`, which destroys the lock table.

## Deploy

```bash
cd terraform
cp backend.example.hcl backend.hcl     # fill in real values
cp envs/example.tfvars envs/myenv.tfvars  # fill in real values

make -C ../lambdas build               # Lambda binaries Terraform zips
terraform init -backend-config=backend.hcl
terraform apply -var-file=envs/myenv.tfvars
```

With `primary_dns_mode = "external"`, run `terraform apply -var-file=envs/myenv.tfvars -target=module.certs` first and set the records from `terraform output certificate_validation_records`; the full apply waits for the certificates to validate. The step-by-step flow is in the repository README.

The repo-root `deploy.sh` wraps this and also handles the frontend build + S3 sync + CloudFront invalidation:

```bash
./deploy.sh --config=terraform/envs/myenv.tfvars
```

## Certificates

`modules/certs` requests both certificates of the primary domain: the regional one for the API custom domain and the us-east-1 one for CloudFront (through `provider "aws" { alias = "us_east_1" }`). In route53 mode it also writes their validation records. The `aws_acm_certificate_validation` resources that wait for validation live in the root module, so `-target=module.certs` returns without waiting.

Certificates of secondary domains are requested by the domains Lambda at runtime, tagged `short-link-stack = <stack_name>`; its IAM policy allows deleting only certificates with that tag.

## Layout

```
terraform/
├── bootstrap/        # one-time: S3 state bucket
├── modules/
│   ├── data/         # DynamoDB tables
│   ├── auth/         # Cognito pool + groups + client
│   ├── lambda/       # Lambda wrapper: zips a prebuilt bootstrap binary (terraform-aws-modules/lambda/aws)
│   ├── certs/        # certificates of the primary domain (+ Route 53 validation records)
│   ├── api/          # HTTP API + JWT authorizer + primary custom domain
│   └── frontend/     # S3 + CloudFront (+ Route 53 alias)
├── envs/             # per-environment .tfvars files
├── main.tf           # module composition
├── variables.tf
├── outputs.tf
└── versions.tf       # provider + backend config
```

## Destroying

`prevent_destroy` is set on DynamoDB tables and the admin S3 bucket. Remove those lifecycle blocks (or run a state-only delete) before `terraform destroy` if you really want a full teardown.
