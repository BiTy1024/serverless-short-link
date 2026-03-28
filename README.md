# Serverless Short Link Service

A serverless URL redirect service built with AWS SAM. Routes incoming requests through API Gateway to Lambda, looks up redirect mappings in DynamoDB, and returns 301 redirects. Includes a CRUD API for managing links and click tracking.

## Architecture

```
Route53 → API Gateway (HTTP API) → Lambda
                                     ├── Redirect Handler (DynamoDB lookup → 301, public)
                                     ├── Links API (DynamoDB CRUD, auth required)
                                     └── Stats API (DynamoDB query, auth required)

Auth          → Cognito User Pool + JWT Authorizer
Click tracking → DynamoDB (RedirectStatsTable)
Link storage   → DynamoDB (LinksTable)
```

## Project Structure

```
├── template.yaml                # SAM/CloudFormation template
├── samconfig.example.toml       # Example deployment config
├── src/
│   ├── redirect/
│   │   ├── handler.py           # Redirect Lambda (DynamoDB lookup + click tracking)
│   │   └── requirements.txt
│   ├── links/
│   │   ├── handler.py           # Links CRUD API Lambda
│   │   └── requirements.txt
│   ├── stats/
│   │   ├── handler.py           # Stats API Lambda
│   │   └── requirements.txt
│   └── cert/
│       ├── handler.py           # Custom resource: cross-region ACM cert
│       └── requirements.txt
```

## Prerequisites

- [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) configured with credentials
- [AWS SAM CLI](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/install-sam-cli.html)
- Python 3.12
- A registered domain with a Route53 hosted zone

## Setup Guide

### 1. Create a Route53 Hosted Zone

If you don't already have a hosted zone for your domain:

```bash
aws route53 create-hosted-zone \
  --name your-domain.de \
  --caller-reference "$(date +%s)"
```

This returns a set of **name servers** (NS records). You must configure these at your domain registrar (e.g. Namecheap, GoDaddy, IONOS) — replace whatever NS records are there with the four AWS name servers.

**How to find your name servers later:**

```bash
aws route53 get-hosted-zone --id YOUR_HOSTED_ZONE_ID \
  --query 'DelegationSet.NameServers'
```

**Get your Hosted Zone ID:**

```bash
aws route53 list-hosted-zones-by-name \
  --dns-name your-domain.de \
  --query 'HostedZones[0].Id' \
  --output text
```

This returns something like `/hostedzone/Z1234567890ABC`. The ID is the `Z1234567890ABC` part.

**Important:** DNS propagation after changing name servers at your registrar can take up to 48 hours, though it usually completes within a few hours. The hosted zone itself is created instantly.

### 2. Request an ACM Certificate

The certificate must be in the **same region** as your API Gateway (default: `eu-central-1`).

```bash
aws acm request-certificate \
  --domain-name short.your-domain.de \
  --validation-method DNS \
  --region eu-central-1
```

Save the returned `CertificateArn`.

**Validate via DNS** — ACM needs to verify you own the domain. Get the validation CNAME record:

```bash
aws acm describe-certificate \
  --certificate-arn YOUR_CERTIFICATE_ARN \
  --region eu-central-1 \
  --query 'Certificate.DomainValidationOptions[0].ResourceRecord'
```

This outputs a `Name` and `Value`. Add this CNAME to your hosted zone:

```bash
aws route53 change-resource-record-sets \
  --hosted-zone-id YOUR_HOSTED_ZONE_ID \
  --change-batch '{
    "Changes": [{
      "Action": "CREATE",
      "ResourceRecordSet": {
        "Name": "THE_CNAME_NAME",
        "Type": "CNAME",
        "TTL": 300,
        "ResourceRecords": [{"Value": "THE_CNAME_VALUE"}]
      }
    }]
  }'
```

Wait for validation (usually 5-10 minutes):

```bash
aws acm wait certificate-validated \
  --certificate-arn YOUR_CERTIFICATE_ARN \
  --region eu-central-1
```

### 3. Configure Deployment

```bash
cp samconfig.example.toml samconfig.toml
```

Edit `samconfig.toml` and fill in your values:

```toml
parameter_overrides = [
    "DomainName=short.your-domain.de",
    "HostedZoneId=Z1234567890ABC",
    "CertificateArn=arn:aws:acm:eu-central-1:123456789012:certificate/...",
    "DefaultRedirectUrl=https://your-domain.de"
]
```

### 4. Build and Deploy

```bash
sam build
sam deploy
```

For first-time deployment, use `sam deploy --guided` and follow the prompts.

## Authentication

All `/api/*` endpoints require a Cognito JWT token. Redirects remain public.

### Create an admin user

```bash
aws cognito-idp admin-create-user \
  --user-pool-id YOUR_USER_POOL_ID \
  --username admin@example.com \
  --temporary-password "TempPass123!" \
  --user-attributes Name=email,Value=admin@example.com
```

### Get an access token (for API testing)

```bash
# Get token (replace values from stack outputs)
aws cognito-idp admin-initiate-auth \
  --user-pool-id YOUR_USER_POOL_ID \
  --client-id YOUR_CLIENT_ID \
  --auth-flow ADMIN_USER_PASSWORD_AUTH \
  --auth-parameters USERNAME=admin@example.com,PASSWORD=YourPassword

# Use the AccessToken from the response:
curl -H "Authorization: Bearer YOUR_ACCESS_TOKEN" \
  https://short.your-domain.de/api/links
```

## Links CRUD API

Manage short links via the API at `https://short.your-domain.de/api/links`. All requests require an `Authorization: Bearer <token>` header.

### Create a link

```bash
curl -X POST https://short.your-domain.de/api/links \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"short_path": "example", "target_url": "https://example.com"}'
```

### List all links

```bash
curl -H "Authorization: Bearer TOKEN" \
  https://short.your-domain.de/api/links
```

### Update a link

```bash
curl -X PUT https://short.your-domain.de/api/links/example \
  -H "Authorization: Bearer TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"target_url": "https://new-target.com"}'
```

### Delete a link

```bash
curl -X DELETE -H "Authorization: Bearer TOKEN" \
  https://short.your-domain.de/api/links/example
```

### Validation rules

- **Path**: lowercase letters, numbers, and hyphens only. 1-64 characters. Must start and end with a letter or number.
- **URL**: must start with `https://`

## Statistics API

Query click statistics at `https://short.your-domain.de/api/stats`.

### Overview (all links)

```bash
curl -H "Authorization: Bearer TOKEN" \
  https://short.your-domain.de/api/stats
```

### Stats for a specific link

```bash
curl -H "Authorization: Bearer TOKEN" \
  https://short.your-domain.de/api/stats/example
```

### Filter by time period

```bash
# Last 7 days
curl -H "Authorization: Bearer TOKEN" \
  "https://short.your-domain.de/api/stats/example?days=7"

# Date range
curl -H "Authorization: Bearer TOKEN" \
  "https://short.your-domain.de/api/stats/example?from=2025-12-03&to=2026-01-26"
```

## Admin Frontend

The admin dashboard is a React SPA hosted on `admin.short.your-domain.de` via S3 + CloudFront.

### Deploy everything (backend + frontend)

```bash
./deploy.sh
```

### Frontend development

```bash
cd frontend
cp .env.example .env   # Fill in your values
npm run dev             # Starts dev server at localhost:5173
```

### Frontend-only deploy

```bash
cd frontend && npm run build && cd ..
aws s3 sync frontend/dist/ s3://BUCKET_NAME --delete
aws cloudfront create-invalidation --distribution-id DIST_ID --paths "/*"
```

## Monitoring

```bash
# Redirect Lambda logs
sam logs -n pr-redirect-service-redirect --tail

# Links API logs
sam logs -n pr-redirect-service-links-api --tail

# Stats API logs
sam logs -n pr-redirect-service-stats-api --tail
```

## Cleanup

```bash
sam delete
```
