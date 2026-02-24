#!/usr/bin/env python3

"""
Query redirect statistics from DynamoDB with time filtering.
"""

import boto3
import sys
import re
from datetime import datetime, timedelta, timezone
from boto3.dynamodb.conditions import Key
from pathlib import Path


def get_region():
    """Get AWS region from samconfig.toml."""
    config_path = Path(__file__).parent / 'samconfig.toml'
    config_content = config_path.read_text()
    region_match = re.search(r'region\s*=\s*"([^"]+)"', config_content)
    return region_match.group(1) if region_match else 'eu-central-1'


def get_table_name():
    """Get the DynamoDB table name from CloudFormation stack outputs."""
    region = get_region()
    cf_client = boto3.client('cloudformation', region_name=region)
    
    try:
        response = cf_client.describe_stacks(StackName='pr-redirect-service')
        if 'Stacks' in response and len(response['Stacks']) > 0:
            stack = response['Stacks'][0]
            if 'Outputs' in stack:
                for output in stack['Outputs']:
                    if output['OutputKey'] == 'StatsTableName':
                        table_name = output['OutputValue']
                        print(f"✅ Found DynamoDB table: {table_name}")
                        return table_name
    except Exception as e:
        print(f"❌ Error getting table from CloudFormation: {e}")
        sys.exit(1)
    
    print("❌ StatsTableName not found in CloudFormation outputs")
    sys.exit(1)


TABLE_NAME = get_table_name()
REGION = get_region()


def query_all_clicks():
    """Get all clicks across all paths."""
    dynamodb = boto3.resource('dynamodb', region_name=REGION)
    table = dynamodb.Table(TABLE_NAME)
    
    response = table.scan()
    items = response['Items']
    
    # Handle pagination
    while 'LastEvaluatedKey' in response:
        response = table.scan(ExclusiveStartKey=response['LastEvaluatedKey'])
        items.extend(response['Items'])
    
    return items


def query_clicks_by_path(path: str, start_date=None, end_date=None):
    """Get clicks for a specific path, optionally filtered by time range."""
    dynamodb = boto3.resource('dynamodb', region_name=REGION)
    table = dynamodb.Table(TABLE_NAME)
    
    # Build query
    key_condition = Key('redirect_path').eq(path)
    
    if start_date and end_date:
        key_condition &= Key('timestamp').between(
            start_date.isoformat(),
            end_date.isoformat()
        )
    elif start_date:
        key_condition &= Key('timestamp').gte(start_date.isoformat())
    elif end_date:
        key_condition &= Key('timestamp').lte(end_date.isoformat())
    
    response = table.query(KeyConditionExpression=key_condition)
    items = response['Items']
    
    # Handle pagination
    while 'LastEvaluatedKey' in response:
        response = table.query(
            KeyConditionExpression=key_condition,
            ExclusiveStartKey=response['LastEvaluatedKey']
        )
        items.extend(response['Items'])
    
    return items


def get_stats_summary(items):
    """Generate summary statistics from click items."""
    from collections import defaultdict
    
    stats = defaultdict(lambda: {'count': 0, 'target_url': None, 'first_click': None, 'last_click': None})
    
    for item in items:
        path = item['redirect_path']
        timestamp = item['timestamp']
        
        stats[path]['count'] += 1
        stats[path]['target_url'] = item.get('target_url')
        
        if stats[path]['first_click'] is None or timestamp < stats[path]['first_click']:
            stats[path]['first_click'] = timestamp
        if stats[path]['last_click'] is None or timestamp > stats[path]['last_click']:
            stats[path]['last_click'] = timestamp
    
    return dict(stats)


def print_summary():
    """Print overall statistics."""
    print("\n📊 Redirect Statistics - All Time\n")
    
    items = query_all_clicks()
    stats = get_stats_summary(items)
    
    # Sort by click count
    sorted_stats = sorted(stats.items(), key=lambda x: x[1]['count'], reverse=True)
    
    print(f"{'Path':<30} {'Clicks':<10} {'Target URL':<50}")
    print("-" * 100)
    
    for path, data in sorted_stats:
        print(f"{path:<30} {data['count']:<10} {data['target_url']:<50}")
    
    print(f"\nTotal paths: {len(stats)}")
    print(f"Total clicks: {len(items)}")


def print_time_range_stats(days_back=7):
    """Print statistics for the last N days."""
    end_date = datetime.now(timezone.utc)
    start_date = end_date - timedelta(days=days_back)
    
    print(f"\n📊 Redirect Statistics - Last {days_back} Days")
    print(f"From: {start_date.strftime('%Y-%m-%d %H:%M')} to {end_date.strftime('%Y-%m-%d %H:%M')}\n")
    
    # Get all items and filter by date
    all_items = query_all_clicks()
    filtered_items = [
        item for item in all_items
        if start_date.isoformat() <= item['timestamp'] <= end_date.isoformat()
    ]
    
    stats = get_stats_summary(filtered_items)
    sorted_stats = sorted(stats.items(), key=lambda x: x[1]['count'], reverse=True)
    
    print(f"{'Path':<30} {'Clicks':<10} {'Last Click':<20}")
    print("-" * 70)
    
    for path, data in sorted_stats:
        last_click = datetime.fromisoformat(data['last_click']).strftime('%Y-%m-%d %H:%M')
        print(f"{path:<30} {data['count']:<10} {last_click:<20}")
    
    print(f"\nTotal clicks in period: {len(filtered_items)}")


def print_path_details(path: str, days_back=30):
    """Print detailed statistics for a specific path."""
    end_date = datetime.now(timezone.utc)
    start_date = end_date - timedelta(days=days_back)
    
    items = query_clicks_by_path(path, start_date, end_date)
    
    print(f"\n📊 Click Details for: {path}")
    print(f"Period: Last {days_back} days\n")
    
    if not items:
        print("No clicks in this period.")
        return
    
    print(f"Total clicks: {len(items)}")
    print(f"Target URL: {items[0].get('target_url', 'N/A')}")
    print(f"\nRecent clicks:")
    print(f"{'Timestamp':<25}")
    print("-" * 25)
    
    # Show last 10 clicks
    sorted_items = sorted(items, key=lambda x: x['timestamp'], reverse=True)[:10]
    for item in sorted_items:
        timestamp = datetime.fromisoformat(item['timestamp']).strftime('%Y-%m-%d %H:%M:%S')
        print(f"{timestamp}")


if __name__ == '__main__':
    import sys
    
    if len(sys.argv) > 1:
        # Query specific path
        path = sys.argv[1]
        days = int(sys.argv[2]) if len(sys.argv) > 2 else 30
        print_path_details(path, days)
    else:
        # Show overall summary
        print_summary()
        print("\n" + "="*100 + "\n")
        print_time_range_stats(7)
        
        print("\n\n💡 Usage:")
        print("  All stats:           python query_stats.py")
        print("  Specific path:       python query_stats.py /pr-123")
        print("  Path with timeframe: python query_stats.py /pr-123 30")
