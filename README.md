# Serverless Short Link Service

A serverless URL redirect service built with AWS SAM. Routes incoming requests through API Gateway to Lambda, looks up redirect mappings, and returns 301 redirects. Includes a CRUD API for managing links and click tracking via DynamoDB.

## Architecture

```
Route53 → API Gateway (HTTP API) → Lambda
                                     ├── Redirect Handler (S3 lookup → 301)
                                     └── Links API (DynamoDB CRUD)

Click tracking → DynamoDB (RedirectStatsTable)
Link storage   → DynamoDB (LinksTable)
```

## Project Structure

```
├── template.yaml                # SAM/CloudFormation template
├── samconfig.example.toml       # Example deployment config
├── redirects.json               # Static redirect mappings (legacy, for S3)
├── src/
│   ├── redirect/
│   │   ├── handler.py           # Redirect Lambda (S3 lookup + click tracking)
│   │   └── requirements.txt
│   └── links/
│       ├── handler.py           # Links CRUD API Lambda
│       └── requirements.txt
├── scripts/
│   └── migrate_links.py         # Migrate redirects.json → DynamoDB
└── query_stats.py               # CLI tool to query click statistics
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

### 3. Create an S3 Bucket for Redirect Mappings

```bash
aws s3 mb s3://your-redirect-bucket --region eu-central-1
```

Upload the initial redirect mappings:

```bash
aws s3 cp redirects.json s3://your-redirect-bucket/redirects.json
```

### 4. Configure Deployment

```bash
cp samconfig.example.toml samconfig.toml
```

Edit `samconfig.toml` and fill in your values:

```toml
parameter_overrides = [
    "DomainName=short.your-domain.de",
    "HostedZoneId=Z1234567890ABC",
    "CertificateArn=arn:aws:acm:eu-central-1:123456789012:certificate/...",
    "RedirectBucket=your-redirect-bucket",
    "DefaultRedirectUrl=https://your-domain.de"
]
```

### 5. Build and Deploy

```bash
sam build
sam deploy
```

For first-time deployment, use `sam deploy --guided` and follow the prompts.

### 6. Migrate Existing Links to DynamoDB

After deploying, seed the LinksTable with your existing redirects:

```bash
python scripts/migrate_links.py
```

## Links CRUD API

Manage short links via the API at `https://short.your-domain.de/api/links`.

### Create a link

```bash
curl -X POST https://short.your-domain.de/api/links \
  -H "Content-Type: application/json" \
  -d '{"short_path": "example", "target_url": "https://example.com"}'
```

### List all links

```bash
curl https://short.your-domain.de/api/links
```

### Update a link

```bash
curl -X PUT https://short.your-domain.de/api/links/example \
  -H "Content-Type: application/json" \
  -d '{"target_url": "https://new-target.com"}'
```

### Delete a link

```bash
curl -X DELETE https://short.your-domain.de/api/links/example
```

### Validation rules

- **Path**: lowercase letters, numbers, and hyphens only. 1-64 characters. Must start and end with a letter or number.
- **URL**: must start with `https://`

## Click Statistics

Query click statistics from DynamoDB:

```bash
python query_stats.py              # all-time summary
python query_stats.py /path        # stats for a specific path
python query_stats.py /path 7      # stats for the last 7 days
```

## Local Development

```bash
sam build
sam local start-api
curl -I http://localhost:3000/mieter
```

## Monitoring

```bash
# Redirect Lambda logs
sam logs -n pr-redirect-service-redirect --tail

# Links API logs
sam logs -n pr-redirect-service-links-api --tail
```

## Cleanup

```bash
sam delete
aws s3 rb s3://your-redirect-bucket --force
```
