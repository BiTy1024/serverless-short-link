import json
import os
import boto3
from aws_lambda_powertools import Logger
from collections import defaultdict
from datetime import datetime, timedelta, timezone
from typing import Dict, Any, Optional
from boto3.dynamodb.conditions import Key

logger = Logger()

dynamodb = boto3.resource('dynamodb')
table = dynamodb.Table(os.environ['STATS_TABLE_NAME'])


def json_response(status_code: int, body: Any = None) -> Dict[str, Any]:
    response = {
        'statusCode': status_code,
        'headers': {'Content-Type': 'application/json'},
    }
    if body is not None:
        response['body'] = json.dumps(body)
    return response


def scan_all_clicks():
    items = []
    response = table.scan()
    items.extend(response.get('Items', []))
    while 'LastEvaluatedKey' in response:
        response = table.scan(ExclusiveStartKey=response['LastEvaluatedKey'])
        items.extend(response.get('Items', []))
    return items


def query_clicks_by_path(path: str, start: Optional[str] = None, end: Optional[str] = None):
    key_condition = Key('redirect_path').eq(path)

    if start and end:
        key_condition &= Key('timestamp').between(start, end)
    elif start:
        key_condition &= Key('timestamp').gte(start)
    elif end:
        key_condition &= Key('timestamp').lte(end)

    items = []
    response = table.query(KeyConditionExpression=key_condition)
    items.extend(response.get('Items', []))
    while 'LastEvaluatedKey' in response:
        response = table.query(
            KeyConditionExpression=key_condition,
            ExclusiveStartKey=response['LastEvaluatedKey'],
        )
        items.extend(response.get('Items', []))
    return items


def parse_time_filters(query_params: dict) -> tuple[Optional[str], Optional[str]]:
    from_date = query_params.get('from')
    to_date = query_params.get('to')
    days = query_params.get('days')

    if from_date or to_date:
        start = f"{from_date}T00:00:00+00:00" if from_date else None
        end = f"{to_date}T23:59:59+00:00" if to_date else None
        return start, end

    if days:
        days_int = int(days)
        start = (datetime.now(timezone.utc) - timedelta(days=days_int)).isoformat()
        return start, None

    return None, None


def handle_overview(event: Dict[str, Any]) -> Dict[str, Any]:
    items = scan_all_clicks()

    stats = defaultdict(lambda: {
        'clicks': 0, 'target_url': None, 'first_click': None, 'last_click': None,
    })

    for item in items:
        path = item['redirect_path']
        timestamp = item['timestamp']
        stats[path]['clicks'] += 1
        stats[path]['target_url'] = item.get('target_url')
        if stats[path]['first_click'] is None or timestamp < stats[path]['first_click']:
            stats[path]['first_click'] = timestamp
        if stats[path]['last_click'] is None or timestamp > stats[path]['last_click']:
            stats[path]['last_click'] = timestamp

    result = sorted(
        [{'path': path, **data} for path, data in stats.items()],
        key=lambda x: x['clicks'],
        reverse=True,
    )

    return json_response(200, {'stats': result, 'total_clicks': len(items)})


def handle_detail(event: Dict[str, Any]) -> Dict[str, Any]:
    path = '/' + event.get('pathParameters', {}).get('path', '')
    query_params = event.get('queryStringParameters') or {}

    try:
        start, end = parse_time_filters(query_params)
    except (ValueError, TypeError):
        return json_response(400, {'error': 'Invalid time filter. Use days=N or from=YYYY-MM-DD&to=YYYY-MM-DD'})

    items = query_clicks_by_path(path, start, end)

    sorted_items = sorted(items, key=lambda x: x['timestamp'], reverse=True)
    recent = [{'timestamp': item['timestamp']} for item in sorted_items[:20]]

    target_url = items[0].get('target_url') if items else None

    return json_response(200, {
        'path': path,
        'clicks': len(items),
        'target_url': target_url,
        'recent_clicks': recent,
    })


ROUTES = {
    'GET /api/stats': handle_overview,
    'GET /api/stats/{path}': handle_detail,
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
