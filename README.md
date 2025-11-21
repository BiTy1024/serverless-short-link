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
├── template.yaml           # SAM template
├── samconfig.toml         # SAM deployment config
├── redirects.json         # Example redirect mappings
├── src/
│   ├── handler.py         # Lambda function code
│   └── requirements.txt   # Python dependencies
└── .github/
    └── workflows/
        └── deploy.yml     # GitHub Actions CI/CD
```

## Setup

### 1. Create S3 Bucket

```bash
aws s3 mb s3://your-redirect-bucket
aws s3 cp redirects.json s3://your-redirect-bucket/redirects.json
```

### 2. Create ACM Certificate

```bash
aws acm request-certificate \
  --domain-name short.panelretter.de \
  --validation-method DNS \
  --region eu-central-1
```

Validate the certificate via DNS records in Route53.

### 3. Get Hosted Zone ID

```bash
aws route53 list-hosted-zones-by-name --dns-name panelretter.de
```

### 4. Configure Deployment

Edit `samconfig.toml` and replace placeholder values:

```toml
parameter_overrides = [
    "DomainName=short.panelretter.de",
    "HostedZoneId=YOUR_HOSTED_ZONE_ID",
    "CertificateArn=YOUR_CERTIFICATE_ARN",
    "RedirectBucket=your-redirect-bucket",
    "RedirectKey=redirects.json",
    "DefaultRedirectUrl=https://panelretter.de"
]
```

### 5. Deploy

```bash
sam build
sam deploy --guided
```

Follow the prompts to confirm settings.

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

## GitHub Actions Setup

Configure the following secrets in your GitHub repository:

- `AWS_ROLE_ARN` - IAM role for GitHub Actions OIDC
- `DOMAIN_NAME` - `short.panelretter.de`
- `HOSTED_ZONE_ID` - Route53 hosted zone ID
- `CERTIFICATE_ARN` - ACM certificate ARN
- `REDIRECT_BUCKET` - S3 bucket name

The workflow automatically deploys on push to `main` and updates S3 when `redirects.json` changes.

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

## Cleanup

```bash
sam delete
```

## License

MIT