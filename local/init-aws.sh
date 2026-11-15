#!/bin/sh
# Idempotent local setup of billing's messaging resources on MiniStack.
# Creates only the topics/queues owned by billing-service (prefix auto-repair-shop-local-).
set -eu

P=auto-repair-shop-local
ENDPOINT="${AWS_ENDPOINT_URL:-http://ministack:4566}"
REGION="${AWS_REGION:-us-east-1}"
AWS="aws --endpoint-url $ENDPOINT --region $REGION"

# Topics
COMMANDS_ARN=$($AWS sns create-topic --name "$P-saga-commands" --query TopicArn --output text)
EVENTS_ARN=$($AWS sns create-topic --name "$P-billing-events" --query TopicArn --output text)

# Queue with DLQ (maxReceiveCount=5)
DLQ_URL=$($AWS sqs create-queue --queue-name "$P-billing-queue-dlq" --query QueueUrl --output text)
DLQ_ARN=$($AWS sqs get-queue-attributes --queue-url "$DLQ_URL" --attribute-names QueueArn --query Attributes.QueueArn --output text)
QUEUE_URL=$($AWS sqs create-queue --queue-name "$P-billing-queue" --query QueueUrl --output text)
QUEUE_ARN=$($AWS sqs get-queue-attributes --queue-url "$QUEUE_URL" --attribute-names QueueArn --query Attributes.QueueArn --output text)

REDRIVE="{\"deadLetterTargetArn\":\"$DLQ_ARN\",\"maxReceiveCount\":\"5\"}"
$AWS sqs set-queue-attributes --queue-url "$QUEUE_URL" --attributes "RedrivePolicy=$REDRIVE" || true

# Queue policy: only the saga-commands topic may send to the queue
POLICY="{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":\"*\",\"Action\":\"sqs:SendMessage\",\"Resource\":\"$QUEUE_ARN\",\"Condition\":{\"ArnEquals\":{\"aws:SourceArn\":\"$COMMANDS_ARN\"}}}]}"
$AWS sqs set-queue-attributes --queue-url "$QUEUE_URL" --attributes "Policy=$POLICY" || true

# Subscription saga-commands -> billing-queue, raw delivery, filtered by target=billing
SUB_ARN=$($AWS sns subscribe --topic-arn "$COMMANDS_ARN" --protocol sqs --notification-endpoint "$QUEUE_ARN" --query SubscriptionArn --output text)
$AWS sns set-subscription-attributes --subscription-arn "$SUB_ARN" --attribute-name RawMessageDelivery --attribute-value true || true
$AWS sns set-subscription-attributes --subscription-arn "$SUB_ARN" --attribute-name FilterPolicyScope --attribute-value MessageAttributes || true
$AWS sns set-subscription-attributes --subscription-arn "$SUB_ARN" --attribute-name FilterPolicy --attribute-value '{"target":["billing"]}' || true

echo "billing messaging ready: queue=$QUEUE_URL events=$EVENTS_ARN"
