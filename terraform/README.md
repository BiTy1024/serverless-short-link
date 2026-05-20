# Terraform Deployment

Alternative to the SAM deploy path. Provisions the same infrastructure (Lambda, HTTP API, DynamoDB, Cognito, S3, CloudFront, Route53) so `deploy.sh --iac=terraform` can replace `deploy.sh` (SAM).

## Prerequisites

- Terraform >= 1.6
- AWS CLI configured for the target account
- Route53 hosted zone for the parent domain
- ACM certificate for `var.domain_name` **in the same region** (us-east-1 cert for CloudFront is created automatically)

## One-time: bootstrap remote state

The root module uses an S3 backend with DynamoDB locking. Provision those once:

```bash
cd terraform/bootstrap
terraform init
terraform apply -var "state_bucket_name=YOUR-GLOBALLY-UNIQUE-BUCKET"
# Note the backend_config_snippet output
```

Copy the snippet output into `terraform/backend.hcl` (gitignored), or pass the values via `-backend-config=...` on `terraform init`.

## Deploy

```bash
cd terraform
cp backend.example.hcl backend.hcl     # fill in real values
cp envs/example.tfvars envs/myenv.tfvars  # fill in real values

terraform init -backend-config=backend.hcl
terraform apply -var-file=envs/myenv.tfvars
```

The repo-root `deploy.sh` wraps this and also handles the frontend build + S3 sync + CloudFront invalidation:

```bash
./deploy.sh --iac=terraform --config=envs/myenv.tfvars
```

## Resource mapping vs. SAM

See `issues/12_plan.md` for the full SAM → Terraform resource table. Notable difference: the us-east-1 cert for CloudFront uses a `provider "aws" { alias = "us_east_1" }` instead of the custom-resource Lambda the SAM template needs.

## Layout

```
terraform/
├── bootstrap/        # one-time: S3 state bucket + DynamoDB lock table
├── modules/
│   ├── data/         # DynamoDB tables
│   ├── auth/         # Cognito pool + groups + client
│   ├── lambda/       # reusable Lambda wrapper (terraform-aws-modules/lambda/aws)
│   ├── api/          # HTTP API + JWT authorizer + custom domain
│   └── frontend/     # S3 + CloudFront + us-east-1 cert + Route53
├── envs/             # per-environment .tfvars files
├── main.tf           # module composition
├── variables.tf
├── outputs.tf
└── versions.tf       # provider + backend config
```

## Destroying

`prevent_destroy` is set on DynamoDB tables and the admin S3 bucket — matching the SAM `DeletionPolicy: Retain` on those resources. Remove those lifecycle blocks (or run a state-only delete) before `terraform destroy` if you really want a full teardown.
