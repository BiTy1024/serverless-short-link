#!/bin/bash
set -e

IAC="sam"
CONFIG=""

while [[ $# -gt 0 ]]; do
  case $1 in
    --iac)
      IAC="$2"
      shift 2
      ;;
    --iac=*)
      IAC="${1#*=}"
      shift
      ;;
    --config)
      CONFIG="$2"
      shift 2
      ;;
    --config=*)
      CONFIG="${1#*=}"
      shift
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: ./deploy.sh [--iac=sam|terraform] [--config <file>]"
      echo "  SAM:       --config <samconfig.toml>      (default: samconfig.toml)"
      echo "  Terraform: --config <envs/myenv.tfvars>   (required)"
      exit 1
      ;;
  esac
done

case "$IAC" in
  sam|terraform) ;;
  *) echo "Error: --iac must be 'sam' or 'terraform' (got '$IAC')"; exit 1 ;;
esac

deploy_sam() {
  local sam_config="${CONFIG:-samconfig.toml}"

  if [[ ! -f "$sam_config" ]]; then
    echo "Error: config file '$sam_config' not found"
    exit 1
  fi

  STACK_NAME=$(grep -m1 'stack_name' "$sam_config" | sed 's/.*= *"\(.*\)"/\1/')
  REGION=$(grep -m1 'region' "$sam_config" | sed 's/.*= *"\(.*\)"/\1/')

  echo "Using SAM config: $sam_config (stack=$STACK_NAME, region=$REGION)"

  echo "=== Building backend ==="
  sam build

  echo "=== Deploying backend ==="
  local deploy_log
  deploy_log=$(mktemp)
  sam deploy --config-file "$sam_config" 2>&1 | tee "$deploy_log" || true
  if grep -q "No changes to deploy" "$deploy_log"; then
    echo "=== No backend changes, continuing with frontend ==="
  else
    echo "=== Backend deployed ==="
  fi
  rm -f "$deploy_log"

  echo "=== Reading stack outputs ==="
  local outputs
  outputs=$(aws cloudformation describe-stacks \
    --stack-name "$STACK_NAME" \
    --query 'Stacks[0].Outputs' \
    --output json --region "$REGION")

  read_sam_output() {
    echo "$outputs" | python3 -c "import sys,json; print(next(o['OutputValue'] for o in json.load(sys.stdin) if o['OutputKey']=='$1'))"
  }

  BUCKET=$(read_sam_output AdminBucketName)
  DIST_ID=$(read_sam_output AdminDistributionId)
  API_BASE=$(read_sam_output CustomDomainUrl)
  USER_POOL_ID=$(read_sam_output UserPoolId)
  CLIENT_ID=$(read_sam_output UserPoolClientId)
  ACCENT_COLOR=$(read_sam_output AccentColor)
  ADMIN_URL=$(read_sam_output AdminDomainUrl)
}

deploy_terraform() {
  if [[ -z "$CONFIG" ]]; then
    echo "Error: --config <envs/*.tfvars> is required for Terraform"
    exit 1
  fi

  local tfvars_abs
  tfvars_abs=$(cd "$(dirname "$CONFIG")" && pwd)/$(basename "$CONFIG")

  if [[ ! -f "$tfvars_abs" ]]; then
    echo "Error: tfvars file '$CONFIG' not found"
    exit 1
  fi

  if [[ ! -f terraform/backend.hcl ]]; then
    echo "Error: terraform/backend.hcl not found. Run the bootstrap step first."
    echo "       See terraform/README.md."
    exit 1
  fi

  echo "Using Terraform config: $tfvars_abs"

  echo "=== terraform init ==="
  (cd terraform && terraform init -backend-config=backend.hcl -reconfigure)

  echo "=== terraform apply ==="
  (cd terraform && terraform apply -var-file="$tfvars_abs" -auto-approve)

  echo "=== Reading terraform outputs ==="
  local outputs
  outputs=$(cd terraform && terraform output -json)

  read_tf_output() {
    echo "$outputs" | python3 -c "import sys,json; print(json.load(sys.stdin)['$1']['value'])"
  }

  BUCKET=$(read_tf_output admin_bucket_name)
  DIST_ID=$(read_tf_output admin_distribution_id)
  API_BASE=$(read_tf_output custom_domain_url)
  USER_POOL_ID=$(read_tf_output user_pool_id)
  CLIENT_ID=$(read_tf_output user_pool_client_id)
  ACCENT_COLOR=$(read_tf_output accent_color)
  ADMIN_URL=$(read_tf_output admin_domain_url)
  REGION=$(grep -m1 '^aws_region' "$tfvars_abs" | sed 's/.*= *"\(.*\)"/\1/')
  REGION="${REGION:-eu-central-1}"
}

publish_frontend() {
  echo "=== Generating frontend/.env ==="
  cat > frontend/.env <<EOL
VITE_API_BASE=$API_BASE
VITE_USER_POOL_ID=$USER_POOL_ID
VITE_USER_POOL_CLIENT_ID=$CLIENT_ID
VITE_ACCENT_COLOR="$ACCENT_COLOR"
EOL

  echo "=== Building frontend ==="
  (cd frontend && npm run build)

  echo "=== Uploading to S3: $BUCKET ==="
  aws s3 sync frontend/dist/ "s3://$BUCKET" --delete --region "$REGION"

  echo "=== Invalidating CloudFront cache: $DIST_ID ==="
  aws cloudfront create-invalidation \
    --distribution-id "$DIST_ID" \
    --paths "/*" > /dev/null

  echo "=== Done ==="
  echo "Admin frontend: $ADMIN_URL"
}

if [[ "$IAC" == "sam" ]]; then
  deploy_sam
else
  deploy_terraform
fi

publish_frontend
