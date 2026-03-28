import json
import time
import boto3
import urllib.request
from aws_lambda_powertools import Logger

logger = Logger()

acm = boto3.client('acm', region_name='us-east-1')
route53 = boto3.client('route53')

# Timeout for certificate validation during deploy
TIMEOUT = 600


def send_response(event, context, status, data=None, reason=None):
    body = json.dumps({
        'Status': status,
        'Reason': reason or f"See CloudWatch Log Stream: {context.log_stream_name}",
        'PhysicalResourceId': data.get('CertificateArn', 'NONE') if data else 'NONE',
        'StackId': event['StackId'],
        'RequestId': event['RequestId'],
        'LogicalResourceId': event['LogicalResourceId'],
        'Data': data or {},
    }).encode('utf-8')

    req = urllib.request.Request(
        event['ResponseURL'],
        data=body,
        headers={'Content-Type': 'application/json'},
        method='PUT',
    )
    urllib.request.urlopen(req)


def create_certificate(domain_name, hosted_zone_id):
    # Request certificate in us-east-1 for cloudfront
    response = acm.request_certificate(
        DomainName=domain_name,
        ValidationMethod='DNS',
    )
    cert_arn = response['CertificateArn']
    logger.info(f"Requested certificate: {cert_arn}")

    # Wait for DNS validation details to become available
    for _ in range(30):
        cert = acm.describe_certificate(CertificateArn=cert_arn)
        options = cert['Certificate'].get('DomainValidationOptions', [])
        if options and 'ResourceRecord' in options[0]:
            break
        time.sleep(2)
    else:
        raise TimeoutError("DNS validation details not available after 60s")

    record = options[0]['ResourceRecord']
    logger.info(f"Validation record: {record['Name']} -> {record['Value']}")

    # Create DNS validation record in Route53
    route53.change_resource_record_sets(
        HostedZoneId=hosted_zone_id,
        ChangeBatch={
            'Changes': [{
                'Action': 'UPSERT',
                'ResourceRecordSet': {
                    'Name': record['Name'],
                    'Type': record['Type'],
                    'TTL': 300,
                    'ResourceRecords': [{'Value': record['Value']}],
                },
            }],
        },
    )
    logger.info("Created DNS validation record")

    # Wait for certificate validation
    start = time.time()
    while time.time() - start < TIMEOUT:
        cert = acm.describe_certificate(CertificateArn=cert_arn)
        status = cert['Certificate']['Status']
        if status == 'ISSUED':
            logger.info("Certificate issued successfully")
            return cert_arn
        if status == 'FAILED':
            raise Exception(f"Certificate validation failed: {cert_arn}")
        time.sleep(10)

    raise TimeoutError(f"Certificate validation timed out after {TIMEOUT}s")


def delete_certificate(cert_arn, domain_name, hosted_zone_id):
    # Remove DNS validation record
    try:
        cert = acm.describe_certificate(CertificateArn=cert_arn)
        options = cert['Certificate'].get('DomainValidationOptions', [])
        if options and 'ResourceRecord' in options[0]:
            record = options[0]['ResourceRecord']
            route53.change_resource_record_sets(
                HostedZoneId=hosted_zone_id,
                ChangeBatch={
                    'Changes': [{
                        'Action': 'DELETE',
                        'ResourceRecordSet': {
                            'Name': record['Name'],
                            'Type': record['Type'],
                            'TTL': 300,
                            'ResourceRecords': [{'Value': record['Value']}],
                        },
                    }],
                },
            )
            logger.info("Deleted DNS validation record")
    except Exception as e:
        logger.warning(f"Failed to delete DNS record: {e}")
    try:
        acm.delete_certificate(CertificateArn=cert_arn)
        logger.info(f"Deleted certificate: {cert_arn}")
    except Exception as e:
        logger.warning(f"Failed to delete certificate: {e}")


def lambda_handler(event, context):
    logger.info(f"Event: {json.dumps(event)}")
    request_type = event['RequestType']
    props = event['ResourceProperties']
    domain_name = props['DomainName']
    hosted_zone_id = props['HostedZoneId']

    try:
        if request_type == 'Create':
            cert_arn = create_certificate(domain_name, hosted_zone_id)
            send_response(event, context, 'SUCCESS', {'CertificateArn': cert_arn})

        elif request_type == 'Update':
            old_props = event.get('OldResourceProperties', {})
            if old_props.get('DomainName') != domain_name:
                # Domain changed — create new cert, delete old
                cert_arn = create_certificate(domain_name, hosted_zone_id)
                old_cert_arn = event.get('PhysicalResourceId')
                if old_cert_arn and old_cert_arn != 'NONE':
                    delete_certificate(old_cert_arn, old_props.get('DomainName', ''), old_props.get('HostedZoneId', ''))
                send_response(event, context, 'SUCCESS', {'CertificateArn': cert_arn})
            else:
                send_response(event, context, 'SUCCESS', {'CertificateArn': event['PhysicalResourceId']})

        elif request_type == 'Delete':
            cert_arn = event.get('PhysicalResourceId')
            if cert_arn and cert_arn != 'NONE':
                delete_certificate(cert_arn, domain_name, hosted_zone_id)
            send_response(event, context, 'SUCCESS')

    except Exception as e:
        logger.error(f"Error: {str(e)}")
        send_response(event, context, 'FAILED', reason=str(e))
