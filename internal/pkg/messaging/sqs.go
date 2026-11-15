package messaging

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// Message is a received SQS message.
type Message struct {
	Body          string
	ReceiptHandle string
}

// Queue receives and deletes SQS messages.
type Queue interface {
	ReceiveMessage(ctx context.Context, queueURL string, max, waitSeconds int32) ([]Message, error)
	DeleteMessage(ctx context.Context, queueURL, receiptHandle string) error
}

// SQSQueue is the aws-sdk-go-v2 implementation of Queue.
type SQSQueue struct {
	client *sqs.Client
}

func NewSQSQueue(client *sqs.Client) *SQSQueue {
	return &SQSQueue{client: client}
}

func (q *SQSQueue) ReceiveMessage(ctx context.Context, queueURL string, max, waitSeconds int32) ([]Message, error) {
	out, err := q.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: max,
		WaitTimeSeconds:     waitSeconds,
	})
	if err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(out.Messages))
	for _, m := range out.Messages {
		msgs = append(msgs, Message{Body: aws.ToString(m.Body), ReceiptHandle: aws.ToString(m.ReceiptHandle)})
	}
	return msgs, nil
}

func (q *SQSQueue) DeleteMessage(ctx context.Context, queueURL, receiptHandle string) error {
	_, err := q.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	return err
}
