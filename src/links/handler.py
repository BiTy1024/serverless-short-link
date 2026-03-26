import json
import os
import re
import boto3
from aws_lambda_powertools import Logger
from datetime import datetime, timezone
from typing import Dict, Any, Tuple

logger = Logger()

dynamodb = boto3.resource('dynamodb')
table = dynamodb.Table(os.environ['LINKS_TABLE_NAME'])

PATH_PATTERN = re.compile(r'^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$')
MAX_PATH_LENGTH = 64
RESERVED_PATHS = {'api', 'admin', 'health', 'status'}


def json_response(status_code: int, body: Any = None) -> Dict[str, Any]:
    response = {
        'statusCode': status_code,
        'headers': {'Content-Type': 'application/json'},
    }
    if body is not None:
        response['body'] = json.dumps(body)
    return response


def validate_path(path: str) -> Tuple[bool, str]:
    if not path:
        return False, 'Path is required'
    if path in RESERVED_PATHS:
        return False, f"Path '{path}' is reserved and cannot be used as a short link"
    if len(path) > MAX_PATH_LENGTH:
        return False, f'Path must be at most {MAX_PATH_LENGTH} characters'
    if '--' in path:
        return False, 'Path must not contain consecutive hyphens'
    if not PATH_PATTERN.match(path):
        return False, 'Path must contain only lowercase letters, numbers, and hyphens. Must start and end with a letter or number.'
    return True, ''


def validate_url(url: str) -> Tuple[bool, str]:
    if not url:
        return False, 'Target URL is required'
    if not url.startswith('https://'):
        return False, 'Target URL must start with https://'
    if len(url) < len('https://x.xx'):
        return False, 'Target URL must be a valid URL with a domain'
    return True, ''


def handle_list(event: Dict[str, Any]) -> Dict[str, Any]:
    items = []
    response = table.scan()
    items.extend(response.get('Items', []))

    while 'LastEvaluatedKey' in response:
        response = table.scan(ExclusiveStartKey=response['LastEvaluatedKey'])
        items.extend(response.get('Items', []))

    return json_response(200, {'links': items, 'count': len(items)})


def handle_create(event: Dict[str, Any]) -> Dict[str, Any]:
    try:
        body = json.loads(event.get('body', '{}'))
    except json.JSONDecodeError:
        return json_response(400, {'error': 'Invalid JSON body'})

    short_path = body.get('short_path', '').strip()
    target_url = body.get('target_url', '').strip()

    valid, msg = validate_path(short_path)
    if not valid:
        return json_response(400, {'error': msg})

    valid, msg = validate_url(target_url)
    if not valid:
        return json_response(400, {'error': msg})

    now = datetime.now(timezone.utc).isoformat()
    item = {
        'short_path': short_path,
        'target_url': target_url,
        'created_at': now,
        'updated_at': now,
    }

    try:
        table.put_item(
            Item=item,
            ConditionExpression='attribute_not_exists(short_path)',
        )
    except dynamodb.meta.client.exceptions.ConditionalCheckFailedException:
        return json_response(409, {'error': f"Path '{short_path}' already exists"})

    logger.info(f"Created link: {short_path} -> {target_url}")
    return json_response(201, item)


def handle_update(event: Dict[str, Any]) -> Dict[str, Any]:
    short_path = event.get('pathParameters', {}).get('path', '')

    try:
        body = json.loads(event.get('body', '{}'))
    except json.JSONDecodeError:
        return json_response(400, {'error': 'Invalid JSON body'})

    target_url = body.get('target_url', '').strip()

    valid, msg = validate_url(target_url)
    if not valid:
        return json_response(400, {'error': msg})

    now = datetime.now(timezone.utc).isoformat()

    try:
        result = table.update_item(
            Key={'short_path': short_path},
            UpdateExpression='SET target_url = :url, updated_at = :now',
            ConditionExpression='attribute_exists(short_path)',
            ExpressionAttributeValues={
                ':url': target_url,
                ':now': now,
            },
            ReturnValues='ALL_NEW',
        )
    except dynamodb.meta.client.exceptions.ConditionalCheckFailedException:
        return json_response(404, {'error': f"Path '{short_path}' not found"})

    logger.info(f"Updated link: {short_path} -> {target_url}")
    return json_response(200, result['Attributes'])


def handle_delete(event: Dict[str, Any]) -> Dict[str, Any]:
    short_path = event.get('pathParameters', {}).get('path', '')

    try:
        table.delete_item(
            Key={'short_path': short_path},
            ConditionExpression='attribute_exists(short_path)',
        )
    except dynamodb.meta.client.exceptions.ConditionalCheckFailedException:
        return json_response(404, {'error': f"Path '{short_path}' not found"})

    logger.info(f"Deleted link: {short_path}")
    return json_response(204)


ROUTES = {
    'GET /api/links': handle_list,
    'POST /api/links': handle_create,
    'PUT /api/links/{path}': handle_update,
    'DELETE /api/links/{path}': handle_delete,
}


@logger.inject_lambda_context
def lambda_handler(event: Dict[str, Any], context: Any) -> Dict[str, Any]:
    route_key = event.get('routeKey', '')
    logger.info(f"Request: {route_key}")

    handler = ROUTES.get(route_key)
    if not handler:
        return json_response(404, {'error': 'Route not found'})

    try:
        return handler(event)
    except Exception as e:
        logger.error(f"Error handling {route_key}: {str(e)}")
        return json_response(500, {'error': 'Internal server error'})
