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

echo "=== Building frontend ==="
cd frontend
npm run build
cd ..

echo "=== Getting stack outputs ==="
BUCKET=$(aws cloudformation describe-stacks \
  --stack-name "$STACK_NAME" \
  --query 'Stacks[0].Outputs[?OutputKey==`AdminBucketName`].OutputValue' \
  --output text --region "$REGION")

DIST_ID=$(aws cloudformation describe-stacks \
  --stack-name "$STACK_NAME" \
  --query 'Stacks[0].Outputs[?OutputKey==`AdminDistributionId`].OutputValue' \
  --output text --region "$REGION")

echo "=== Uploading to S3: $BUCKET ==="
aws s3 sync frontend/dist/ "s3://$BUCKET" --delete --region "$REGION"

echo "=== Invalidating CloudFront cache: $DIST_ID ==="
aws cloudfront create-invalidation \
  --distribution-id "$DIST_ID" \
  --paths "/*" > /dev/null

echo "=== Done ==="
echo "Admin frontend: https://admin.$(aws cloudformation describe-stacks \
  --stack-name "$STACK_NAME" \
  --query 'Stacks[0].Outputs[?OutputKey==`AdminDomainUrl`].OutputValue' \
  --output text --region "$REGION" | sed 's|https://||')"
