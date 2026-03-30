#!/bin/bash
set -e

SAM_CONFIG="samconfig.toml"

while [[ $# -gt 0 ]]; do
  case $1 in
    --config)
      SAM_CONFIG="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: ./deploy.sh [--config <samconfig-file>]"
      exit 1
      ;;
  esac
done

if [[ ! -f "$SAM_CONFIG" ]]; then
  echo "Error: config file '$SAM_CONFIG' not found"
  exit 1
fi

STACK_NAME=$(grep -m1 'stack_name' "$SAM_CONFIG" | sed 's/.*= *"\(.*\)"/\1/')
REGION=$(grep -m1 'region' "$SAM_CONFIG" | sed 's/.*= *"\(.*\)"/\1/')

echo "Using config: $SAM_CONFIG (stack=$STACK_NAME, region=$REGION)"

echo "=== Building backend ==="
sam build

echo "=== Deploying backend ==="
DEPLOY_LOG=$(mktemp)
sam deploy --config-file "$SAM_CONFIG" 2>&1 | tee "$DEPLOY_LOG" || true
if grep -q "No changes to deploy" "$DEPLOY_LOG"; then
  echo "=== No backend changes, continuing with frontend ==="
else
  echo "=== Backend deployed ==="
fi
rm -f "$DEPLOY_LOG"

echo "=== Getting stack outputs ==="
OUTPUTS=$(aws cloudformation describe-stacks \
  --stack-name "$STACK_NAME" \
  --query 'Stacks[0].Outputs' \
  --output json --region "$REGION")

get_output() {
  echo "$OUTPUTS" | python3 -c "import sys,json; print(next(o['OutputValue'] for o in json.load(sys.stdin) if o['OutputKey']=='$1'))"
}

BUCKET=$(get_output AdminBucketName)
DIST_ID=$(get_output AdminDistributionId)
API_BASE=$(get_output CustomDomainUrl)
USER_POOL_ID=$(get_output UserPoolId)
CLIENT_ID=$(get_output UserPoolClientId)
ACCENT_COLOR=$(get_output AccentColor)

echo "=== Generating frontend/.env ==="
cat > frontend/.env <<EOL
VITE_API_BASE=$API_BASE
VITE_USER_POOL_ID=$USER_POOL_ID
VITE_USER_POOL_CLIENT_ID=$CLIENT_ID
VITE_ACCENT_COLOR="$ACCENT_COLOR"
EOL

echo "=== Building frontend ==="
cd frontend
npm run build
cd ..

echo "=== Uploading to S3: $BUCKET ==="
aws s3 sync frontend/dist/ "s3://$BUCKET" --delete --region "$REGION"

echo "=== Invalidating CloudFront cache: $DIST_ID ==="
aws cloudfront create-invalidation \
  --distribution-id "$DIST_ID" \
  --paths "/*" > /dev/null

echo "=== Done ==="
ADMIN_URL=$(get_output AdminDomainUrl)
echo "Admin frontend: $ADMIN_URL"
