#!/usr/bin/env python3

"""
Script to upload redirects.json to S3 bucket.
Reads configuration from samconfig.toml.
"""

import re
import boto3
from pathlib import Path


def parse_samconfig() -> tuple[str, str]:
    """Extract RedirectBucket and RedirectKey from samconfig.toml."""
    config_path = Path(__file__).parent / 'samconfig.toml'
    config_content = config_path.read_text()
    
    bucket_match = re.search(r'RedirectBucket=([^,\]]+)', config_content)
    key_match = re.search(r'RedirectKey=([^,\]]+)', config_content)
    
    if not bucket_match or not key_match:
        raise ValueError("Could not find RedirectBucket or RedirectKey in samconfig.toml")
    
    bucket = bucket_match.group(1).strip().strip('"')
    key = key_match.group(1).strip().strip('"')
    
    return bucket, key


def upload_redirects():
    """Upload redirects.json to S3."""
    bucket, key = parse_samconfig()
    
    print("📦 Uploading redirects.json to S3...")
    print(f"   Bucket: {bucket}")
    print(f"   Key: {key}")
    print()
    
    s3_client = boto3.client('s3')
    redirects_path = Path(__file__).parent / 'redirects.json'
    
    s3_client.upload_file(
        str(redirects_path),
        bucket,
        key,
        ExtraArgs={'ContentType': 'application/json'}
    )
    
    print()
    print("✅ Upload successful!")
    print()
    print("🔗 Verify the file:")
    print(f"   aws s3 cp s3://{bucket}/{key} -")


if __name__ == '__main__':
    upload_redirects()
