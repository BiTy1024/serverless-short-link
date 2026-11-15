#!/bin/bash
set -e

CONFIG=""

while [[ $# -gt 0 ]]; do
  case $1 in
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
      echo "Usage: ./deploy.sh --config <terraform/envs/myenv.tfvars>"
      exit 1
      ;;
  esac
done

deploy_terraform() {
  if [[ -z "$CONFIG" ]]; then
    echo "Error: --config <terraform/envs/*.tfvars> is required"
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

  echo "=== Building Lambdas ==="
  make -C lambdas build

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

  echo "=== Installing frontend dependencies ==="
  (cd frontend && npm ci)

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

deploy_terraform

publish_frontend
