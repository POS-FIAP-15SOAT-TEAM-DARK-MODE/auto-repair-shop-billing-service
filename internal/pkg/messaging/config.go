package messaging

import (
	"context"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/env"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// loadAWSConfig reads AWS_REGION (default us-east-1). Credentials come from the SDK default chain.
func loadAWSConfig(ctx context.Context) (aws.Config, error) {
	return awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(env.GetString("AWS_REGION", "us-east-1")))
}

// NewSNSClient builds an SNS client; AWS_ENDPOINT_URL (local MiniStack only) overrides the endpoint.
func NewSNSClient(ctx context.Context) (*sns.Client, error) {
	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := env.GetString("AWS_ENDPOINT_URL", "")
	return sns.NewFromConfig(cfg, func(o *sns.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}

// NewSQSClient builds an SQS client; AWS_ENDPOINT_URL (local MiniStack only) overrides the endpoint.
func NewSQSClient(ctx context.Context) (*sqs.Client, error) {
	cfg, err := loadAWSConfig(ctx)
	if err != nil {
		return nil, err
	}
	endpoint := env.GetString("AWS_ENDPOINT_URL", "")
	return sqs.NewFromConfig(cfg, func(o *sqs.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}
