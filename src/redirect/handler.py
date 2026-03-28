import os
import boto3
from aws_lambda_powertools import Logger
from datetime import datetime, timezone
from typing import Dict, Any

logger = Logger()

dynamodb = boto3.resource('dynamodb')
links_table = dynamodb.Table(os.environ['LINKS_TABLE_NAME'])
stats_table = dynamodb.Table(os.environ['STATS_TABLE_NAME'])

DEFAULT_REDIRECT_URL = os.environ['DEFAULT_REDIRECT_URL']
ADMIN_ORIGIN = os.environ['ADMIN_ORIGIN']


def get_redirect_url(path: str) -> tuple[str, bool]:
    short_path = path.strip('/')
    if not short_path:
        return DEFAULT_REDIRECT_URL, False

    response = links_table.get_item(Key={'short_path': short_path})
    item = response.get('Item')
    if item:
        target_url = item['target_url']
        logger.info(f"Redirect hit: /{short_path} -> {target_url}")
        return target_url, True

    logger.info(f"Redirect miss: /{short_path}, using default: {DEFAULT_REDIRECT_URL}")
    return DEFAULT_REDIRECT_URL, False


def track_redirect(path: str, target_url: str) -> None:
    try:
        timestamp = datetime.now(timezone.utc).isoformat()
        stats_table.put_item(
            Item={
                'redirect_path': path,
                'timestamp': timestamp,
                'target_url': target_url,
            }
        )
        logger.info(f"Tracked click: {path} at {timestamp}")
    except Exception as e:
        logger.error(f"Failed to track redirect: {str(e)}")


@logger.inject_lambda_context
def lambda_handler(event: Dict[str, Any], context: Any) -> Dict[str, Any]:
    try:
        path = event.get('rawPath', '/')
        logger.info(f"Request received for path: {path}")

        if path.startswith('/api'):
            method = event.get('requestContext', {}).get('http', {}).get('method', '')
            cors_headers = {
                'Access-Control-Allow-Origin': ADMIN_ORIGIN,
                'Access-Control-Allow-Methods': 'GET, POST, PUT, DELETE, OPTIONS',
                'Access-Control-Allow-Headers': 'Content-Type, Authorization',
                'Access-Control-Max-Age': '3600',
            }
            if method == 'OPTIONS':
                return {'statusCode': 200, 'headers': cors_headers}
            return {
                'statusCode': 404,
                'headers': {**cors_headers, 'Content-Type': 'application/json'},
                'body': '{"error": "Not found"}',
            }

        redirect_url, is_known_link = get_redirect_url(path)

        if is_known_link:
            normalized_path = '/' + path.strip('/')
            track_redirect(normalized_path, redirect_url)

        return {
            'statusCode': 301,
            'headers': {
                'Location': redirect_url,
            },
        }

    except Exception as e:
        logger.error(f"Error processing request: {str(e)}")
        return {
            'statusCode': 502,
            'headers': {
                'Content-Type': 'text/plain',
            },
            'body': 'Service temporarily unavailable',
        }
