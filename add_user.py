#!/usr/bin/env python3
"""Add a Cognito user to the redirect service User Pool."""

import argparse
import secrets
import string

import boto3


REGION = "eu-central-1"


def add_user(email, password, pool_id, group=None):
    cognito = boto3.client("cognito-idp", region_name=REGION)

    cognito.admin_create_user(
        UserPoolId=pool_id,
        Username=email,
        UserAttributes=[
            {"Name": "email", "Value": email},
            {"Name": "email_verified", "Value": "true"},
        ],
        MessageAction="SUPPRESS",
    )

    cognito.admin_set_user_password(
        UserPoolId=pool_id,
        Username=email,
        Password=password,
        Permanent=True,
    )

    if group:
        cognito.admin_add_user_to_group(
            UserPoolId=pool_id,
            Username=email,
            GroupName=group,
        )

    print(f"Created user: {email} (group: {group or 'none'})")


def generate_password(length=16):
    alphabet = string.ascii_letters + string.digits + "!@#$%&*"
    return "".join(secrets.choice(alphabet) for _ in range(length))


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Add a Cognito user")
    parser.add_argument("--email", required=True)
    parser.add_argument("--password", required=True)
    parser.add_argument("--group", choices=["admin", "viewer"])
    parser.add_argument(
        "--user-pool-id",
        required=True,
        help="Cognito User Pool ID (terraform output user_pool_id)",
    )
    args = parser.parse_args()

    add_user(args.email, args.password, args.user_pool_id, args.group)
