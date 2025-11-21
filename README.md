# Redirect Service (AWS SAM)

A lightweight serverless redirect service for `short.panelretter.de` using AWS Lambda, API Gateway, and S3-based configuration.

## Architecture

- **Route53** → API Gateway Custom Domain
- **API Gateway HTTP API** → Lambda function
- **Lambda** loads redirect mappings from **S3**
- Returns **301 redirects** based on URL path

## Features

- S3-based redirect configuration (JSON)
- In-memory caching for performance
- ARM64 Lambda for cost efficiency
- Custom domain with TLS
- Automatic CloudWatch logging
- Update redirects without redeployment

## Prerequisites

- AWS CLI configured
- AWS SAM CLI installed
- Python 3.12
- An ACM certificate for `short.panelretter.de`
- Route53 hosted zone for `panelretter.de`
- S3 bucket for redirect mappings

## Project Structure

```
.
├── template.yaml              # SAM template
├── samconfig.toml            # SAM deployment config (git ignored)
├── samconfig.example.toml    # Example config with placeholders
├── redirects.json            # Example redirect mappings
└── src/
    ├── handler.py            # Lambda function code
    └── requirements.txt      # Python dependencies
```

## Manual Deployment Guide

### Prerequisites

Install required tools:

```bash
# Install AWS CLI
brew install awscli

# Install AWS SAM CLI
brew tap aws/tap
brew install aws-sam-cli

# Configure AWS credentials
aws configure
```

### Step 1: Create S3 Bucket for Redirects

```bash
# Create bucket (use a unique name)
aws s3 mb s3://panelretter-redirects --region eu-central-1

# Enable versioning (recommended)
aws s3api put-bucket-versioning \
  --bucket panelretter-redirects \
  --versioning-configuration Status=Enabled \
  --region eu-central-1

# Upload redirect mappings
aws s3 cp redirects.json s3://panelretter-redirects/redirects.json
```

**Important**: Note your bucket name - you'll need it later.

### Step 2: Request ACM Certificate

```bash
# Request certificate for your custom domain
aws acm request-certificate \
  --domain-name short.panelretter.de \
  --validation-method DNS \
  --region eu-central-1
```

This command returns a `CertificateArn`. Save it.

**Validate the certificate:**

1. Get validation records:
```bash
aws acm describe-certificate \
  --certificate-arn YOUR_CERTIFICATE_ARN \
  --region eu-central-1 \
  --query 'Certificate.DomainValidationOptions[0].ResourceRecord'
```

2. Add the CNAME record to Route53:
```bash
aws route53 change-resource-record-sets \
  --hosted-zone-id YOUR_HOSTED_ZONE_ID \
  --change-batch '{
    "Changes": [{
      "Action": "CREATE",
      "ResourceRecordSet": {
        "Name": "_xxx.short.panelretter.de",
        "Type": "CNAME",
        "TTL": 300,
        "ResourceRecords": [{"Value": "_yyy.acm-validations.aws."}]
      }
    }]
  }'
```

3. Wait for validation (usually 5-10 minutes):
```bash
aws acm wait certificate-validated \
  --certificate-arn YOUR_CERTIFICATE_ARN \
  --region eu-central-1
```

### Step 3: Get Route53 Hosted Zone ID

```bash
aws route53 list-hosted-zones-by-name \
  --dns-name panelretter.de \
  --query 'HostedZones[0].Id' \
  --output text
```

This returns something like `/hostedzone/Z1234567890ABC`. Save the ID part (`Z1234567890ABC`).

### Step 4: Configure Deployment

Copy the example config:

```bash
cp samconfig.example.toml samconfig.toml
```

Edit `samconfig.toml` with your actual values:

```toml
parameter_overrides = [
    "DomainName=short.panelretter.de",
    "HostedZoneId=Z1234567890ABC",                          # From Step 3
    "CertificateArn=arn:aws:acm:...",                       # From Step 2
    "RedirectBucket=panelretter-redirects",                 # From Step 1
    "RedirectKey=redirects.json",
    "DefaultRedirectUrl=https://panelretter.de"
]
```

### Step 5: Build and Deploy

```bash
# Build the Lambda function
sam build

# Deploy with guided prompts
sam deploy --guided
```

**During guided deployment, confirm:**
- Stack name: `pr-redirect-service`
- AWS Region: `eu-central-1`
- Confirm changes before deploy: `Y`
- Allow SAM CLI IAM role creation: `Y`
- Disable rollback: `N`
- Save arguments to config: `Y`

**Deployment takes 5-10 minutes.**

### Step 6: Verify Deployment

Check stack status:
```bash
aws cloudformation describe-stacks \
  --stack-name pr-redirect-service \
  --query 'Stacks[0].StackStatus'
```

Get outputs:
```bash
aws cloudformation describe-stacks \
  --stack-name pr-redirect-service \
  --query 'Stacks[0].Outputs'
```

### Step 7: Test Your Redirects

```bash
# Test with curl
curl -I https://short.panelretter.de/mieter

# Should return:
# HTTP/2 301
# location: https://panelretter.de/blogs/blog/unser-musterschreiben-fur-mieter
```

Test in browser:
- `https://short.panelretter.de/` → redirects to `https://panelretter.de`
- `https://short.panelretter.de/mieter` → redirects to Mieter blog post
- `https://short.panelretter.de/unknown` → redirects to default URL

## Redirect Configuration

Edit `redirects.json`:

```json
{
  "/": "https://panelretter.de",
  "/mieter": "https://panelretter.de/blogs/blog/unser-musterschreiben-fur-mieter",
  "/solarstory": "https://google.com"
}
```

### Update Redirects

To update redirects **without redeploying Lambda**:

```bash
aws s3 cp redirects.json s3://your-redirect-bucket/redirects.json
```

Lambda will reload the file on the next cold start.

## Updating Redirects (No Redeployment Needed)

To add/modify/remove redirects:

1. Edit `redirects.json` locally
2. Upload to S3:
```bash
aws s3 cp redirects.json s3://panelretter-redirects/redirects.json
```
3. Lambda will reload on next cold start (usually within minutes)

To force immediate reload, restart the Lambda:
```bash
aws lambda update-function-configuration \
  --function-name pr-redirect-service-redirect \
  --environment Variables="{REDIRECT_BUCKET=panelretter-redirects,REDIRECT_KEY=redirects.json,DEFAULT_REDIRECT_URL=https://panelretter.de,FORCE_RELOAD=$(date +%s)}"
```

## Testing

```bash
curl -I https://short.panelretter.de/mieter
```

Expected response:
```
HTTP/2 301
location: https://panelretter.de/blogs/blog/unser-musterschreiben-fur-mieter
```

## Monitoring

View Lambda logs:

```bash
sam logs -n pr-redirect-service-redirect --tail
```

Or via CloudWatch:
```bash
aws logs tail /aws/lambda/pr-redirect-service-redirect --follow
```

## Local Testing

```bash
sam build
sam local start-api
curl -I http://localhost:3000/mieter
```

## Cost Optimization

- **ARM64 architecture** - ~20% cheaper than x86
- **256 MB memory** - Optimized for S3 reads and JSON parsing
- **In-memory caching** - Reduces S3 API calls
- **HTTP API** - Cheaper than REST API

## Troubleshooting

### Lambda returns 502

Check CloudWatch logs for S3 access errors:
```bash
aws logs tail /aws/lambda/pr-redirect-service-redirect
```

Verify IAM permissions allow `s3:GetObject` on your bucket.

### Custom domain not working

Verify ACM certificate status:
```bash
aws acm describe-certificate --certificate-arn YOUR_CERTIFICATE_ARN
```

Check Route53 alias record points to API Gateway domain.

### Redirects not updating

Lambda caches mappings in memory. Wait for cold start or manually update Lambda environment variable to force reload.

## Redeploying After Code Changes

If you modify Lambda code or SAM template:

```bash
sam build
sam deploy
```

No need for `--guided` - it uses saved config from `samconfig.toml`.

## Cleanup

To delete all resources:

```bash
# Delete SAM stack (Lambda, API Gateway, CloudWatch logs)
sam delete

# Delete S3 bucket (if no longer needed)
aws s3 rb s3://panelretter-redirects --force

# Delete ACM certificate (optional)
aws acm delete-certificate \
  --certificate-arn YOUR_CERTIFICATE_ARN \
  --region eu-central-1
```

## Troubleshooting Common Issues

### Issue: Certificate validation stuck

**Solution**: Verify CNAME record is correct in Route53:
```bash
dig _xxx.short.panelretter.de CNAME
```

### Issue: Custom domain returns 403 Forbidden

**Solution**: 
1. Verify certificate is in `ISSUED` status
2. Check certificate region matches API Gateway region
3. Wait 5-10 minutes for DNS propagation

### Issue: Lambda returns 502

**Solution**: Check Lambda has S3 read permissions:
```bash
aws lambda get-policy --function-name pr-redirect-service-redirect
```

View recent errors:
```bash
aws logs tail /aws/lambda/pr-redirect-service-redirect --since 1h
```

### Issue: Redirects not updating

**Solution**: Lambda caches mappings. Either:
- Wait for cold start (happens naturally after ~15 min of inactivity)
- Use force reload command from "Updating Redirects" section

## Cost Estimate

Based on 100,000 requests/month:

- **Lambda**: ~$0.20 (ARM64, 256MB, 100ms avg)
- **API Gateway**: ~$0.10 (HTTP API pricing)
- **S3**: ~$0.01 (GET requests + storage)
- **Route53**: $0.50 (hosted zone)
- **CloudWatch**: ~$0.50 (logs)

**Total**: ~$1.31/month

## License

MIT