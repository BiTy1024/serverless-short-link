import json
import os
import logging
import boto3
from datetime import datetime, timezone
from typing import Dict, Any, Optional

logger = logging.getLogger()
logger.setLevel(logging.INFO)

s3_client = boto3.client('s3')
dynamodb = boto3.resource('dynamodb')

redirect_mappings: Optional[Dict[str, str]] = None

REDIRECT_BUCKET = os.environ['REDIRECT_BUCKET']
REDIRECT_KEY = os.environ['REDIRECT_KEY']
DEFAULT_REDIRECT_URL = os.environ['DEFAULT_REDIRECT_URL']
STATS_TABLE_NAME = os.environ['STATS_TABLE_NAME']


def load_redirects() -> Dict[str, str]:
    global redirect_mappings
    
    if redirect_mappings is not None:
        return redirect_mappings
    
    try:
        logger.info(f"Loading redirects from s3://{REDIRECT_BUCKET}/{REDIRECT_KEY}")
        response = s3_client.get_object(Bucket=REDIRECT_BUCKET, Key=REDIRECT_KEY)
        content = response['Body'].read().decode('utf-8')
        redirect_mappings = json.loads(content)
        logger.info(f"Loaded {len(redirect_mappings)} redirect mappings")
        return redirect_mappings
    except Exception as e:
        logger.error(f"Failed to load redirects: {str(e)}")
        raise


def normalize_path(path: str) -> str:
    if not path:
        return '/'
    
    if not path.startswith('/'):
        path = '/' + path
    
    if len(path) > 1 and path.endswith('/'):
        path = path.rstrip('/')
    
    return path


def get_redirect_url(path: str, mappings: Dict[str, str]) -> str:
    normalized_path = normalize_path(path)
    
    if normalized_path in mappings:
        logger.info(f"Redirect hit: {normalized_path} -> {mappings[normalized_path]}")
        return mappings[normalized_path]
    
    logger.info(f"Redirect miss: {normalized_path}, using default: {DEFAULT_REDIRECT_URL}")
    return DEFAULT_REDIRECT_URL


def track_redirect(path: str, target_url: str) -> None:
    """Track redirect in DynamoDB by incrementing click count."""
    try:
        table = dynamodb.Table(STATS_TABLE_NAME)
        response = table.update_item(
            Key={'redirect_path': path},
            UpdateExpression='SET target_url = :url, last_accessed = :timestamp ADD click_count :inc',
            ExpressionAttributeValues={
                ':url': target_url,
                ':timestamp': datetime.now(timezone.utc).isoformat(),
                ':inc': 1
            },
            ReturnValues='UPDATED_NEW'
        )
        logger.info(f"Tracked redirect: {path} -> total clicks: {response.get('Attributes', {}).get('click_count', 1)}")
    except Exception as e:
        # Don't fail the redirect if tracking fails
        logger.error(f"Failed to track redirect: {str(e)}")


def lambda_handler(event: Dict[str, Any], context: Any) -> Dict[str, Any]:
    try:
        path = event.get('rawPath', '/')
        logger.info(f"Request received for path: {path}")
        
        mappings = load_redirects()
        redirect_url = get_redirect_url(path, mappings)
        
        # Track the redirect asynchronously (non-blocking)
        normalized_path = normalize_path(path)
        track_redirect(normalized_path, redirect_url)
        
        return {
            'statusCode': 301,
            'headers': {
                'Location': redirect_url
            }
        }
    
    except Exception as e:
        logger.error(f"Error processing request: {str(e)}")
        return {
            'statusCode': 502,
            'headers': {
                'Content-Type': 'text/plain'
            },
            'body': 'Service temporarily unavailable'
        }
