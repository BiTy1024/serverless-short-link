#!/bin/bash
set -e

STACK_NAME="example-redirect-service"
REGION="eu-central-1"

echo "=== Building backend ==="
sam build

echo "=== Deploying backend ==="
if sam deploy 2>&1 | tee /dev/stderr | grep -q "No changes to deploy"; then
  echo "=== No backend changes, skipping ==="
else
  echo "=== Backend deployed ==="
fi

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

echo "=== Generating frontend/.env ==="
cat > frontend/.env <<EOL
VITE_API_BASE=$API_BASE
VITE_USER_POOL_ID=$USER_POOL_ID
VITE_USER_POOL_CLIENT_ID=$CLIENT_ID
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
