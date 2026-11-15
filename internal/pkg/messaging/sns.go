package messaging

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// Publisher sends a message body with string attributes to a topic.
type Publisher interface {
	Publish(ctx context.Context, topicARN, body string, attributes map[string]string) error
}

// SNSPublisher is the aws-sdk-go-v2 implementation of Publisher.
type SNSPublisher struct {
	client *sns.Client
}

func NewSNSPublisher(client *sns.Client) *SNSPublisher {
	return &SNSPublisher{client: client}
}

func (p *SNSPublisher) Publish(ctx context.Context, topicARN, body string, attributes map[string]string) error {
	attrs := make(map[string]types.MessageAttributeValue, len(attributes))
	for k, v := range attributes {
		attrs[k] = types.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(v)}
	}
	_, err := p.client.Publish(ctx, &sns.PublishInput{
		TopicArn:          aws.String(topicARN),
		Message:           aws.String(body),
		MessageAttributes: attrs,
	})
	return err
}
