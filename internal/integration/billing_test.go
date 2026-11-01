//go:build integration

// Integration tests run against real Postgres and MiniStack (see docker-compose.yml / CI).
// Env: POSTGRES_HOST, POSTGRES_PORT, POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB,
// POSTGRES_SSLMODE, AWS_ENDPOINT_URL, AWS_REGION, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/container"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/app/routing"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/billing"
	budgetRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/budget/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment"
	paymentRepo "github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/payment/repository"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/auth"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/id"
	"github.com/POS-FIAP-15SOAT-TEAM-DARK-MODE/auto-repair-shop-billing-service/internal/pkg/messaging"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssns "github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type env struct {
	t          *testing.T
	ctx        context.Context
	db         *sql.DB
	sns        *awssns.Client
	sqs        *awssqs.Client
	router     http.Handler
	commandARN string
	eventsARN  string
	billingQ   string
	testQ      string
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		os.Getenv("POSTGRES_USER"), os.Getenv("POSTGRES_PASSWORD"), os.Getenv("POSTGRES_HOST"),
		os.Getenv("POSTGRES_PORT"), os.Getenv("POSTGRES_DB"), os.Getenv("POSTGRES_SSLMODE"))
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// setup builds the service with real repositories, SNS/SQS and background loops, on fresh topics and queues.
func setup(t *testing.T, mode payment.Mode) *env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	db := openDB(t)
	_, err := db.ExecContext(ctx, "TRUNCATE budget, budget_item, payment, processed_message, outbox CASCADE")
	require.NoError(t, err)

	snsClient, err := messaging.NewSNSClient(ctx)
	require.NoError(t, err)
	sqsClient, err := messaging.NewSQSClient(ctx)
	require.NoError(t, err)

	e := &env{t: t, ctx: ctx, db: db, sns: snsClient, sqs: sqsClient}
	suffix := fmt.Sprint(time.Now().UnixNano())

	e.commandARN = e.createTopic("it-saga-commands-" + suffix)
	e.eventsARN = e.createTopic("it-billing-events-" + suffix)

	e.billingQ = e.createQueue("it-billing-" + suffix)
	e.subscribe(e.commandARN, e.billingQ, true, `{"target":["billing"]}`)

	e.testQ = e.createQueue("it-test-" + suffix)
	e.subscribe(e.eventsARN, e.testQ, true, "")

	budgets := budgetRepo.NewPostgres()
	payments := paymentRepo.NewPostgres()
	idGen := id.NewIDGenerator()
	svc := billing.NewService(db, budgets, payments, payment.NewInternalProvider(mode), idGen.NewUUID)
	e.router = routing.SetupRouter(&container.HandlersWrapper{Billing: billing.NewController(svc)}, &container.Middlewares{})

	consumer := messaging.NewConsumer(db, messaging.NewSQSQueue(sqsClient), e.billingQ)
	svc.RegisterHandlers(consumer)
	relay := messaging.NewRelay(db, messaging.NewSNSPublisher(snsClient), map[string]string{"billing-events": e.eventsARN})
	go func() { _ = consumer.Run(ctx) }()
	go func() { _ = relay.Run(ctx) }()
	return e
}

func (e *env) createTopic(name string) string {
	out, err := e.sns.CreateTopic(e.ctx, &awssns.CreateTopicInput{Name: aws.String(name)})
	require.NoError(e.t, err)
	return aws.ToString(out.TopicArn)
}

func (e *env) createQueue(name string) string {
	out, err := e.sqs.CreateQueue(e.ctx, &awssqs.CreateQueueInput{QueueName: aws.String(name)})
	require.NoError(e.t, err)
	return aws.ToString(out.QueueUrl)
}

// subscribe links topic -> queue with raw delivery and an optional filter policy.
func (e *env) subscribe(topicARN, queueURL string, raw bool, filter string) {
	e.t.Helper()
	attrs, err := e.sqs.GetQueueAttributes(e.ctx, &awssqs.GetQueueAttributesInput{QueueUrl: aws.String(queueURL), AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn}})
	require.NoError(e.t, err)
	queueARN := attrs.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]

	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"sqs:SendMessage","Resource":"%s","Condition":{"ArnEquals":{"aws:SourceArn":"%s"}}}]}`, queueARN, topicARN)
	_, err = e.sqs.SetQueueAttributes(e.ctx, &awssqs.SetQueueAttributesInput{QueueUrl: aws.String(queueURL), Attributes: map[string]string{"Policy": policy}})
	require.NoError(e.t, err)

	sub, err := e.sns.Subscribe(e.ctx, &awssns.SubscribeInput{TopicArn: aws.String(topicARN), Protocol: aws.String("sqs"), Endpoint: aws.String(queueARN)})
	require.NoError(e.t, err)
	if raw {
		_, err = e.sns.SetSubscriptionAttributes(e.ctx, &awssns.SetSubscriptionAttributesInput{SubscriptionArn: sub.SubscriptionArn, AttributeName: aws.String("RawMessageDelivery"), AttributeValue: aws.String("true")})
		require.NoError(e.t, err)
	}
	if filter != "" {
		_, err = e.sns.SetSubscriptionAttributes(e.ctx, &awssns.SetSubscriptionAttributesInput{SubscriptionArn: sub.SubscriptionArn, AttributeName: aws.String("FilterPolicyScope"), AttributeValue: aws.String("MessageAttributes")})
		require.NoError(e.t, err)
		_, err = e.sns.SetSubscriptionAttributes(e.ctx, &awssns.SetSubscriptionAttributesInput{SubscriptionArn: sub.SubscriptionArn, AttributeName: aws.String("FilterPolicy"), AttributeValue: aws.String(filter)})
		require.NoError(e.t, err)
	}
}

// sendCommand publishes a saga command the way Service Order does (target attribute + envelope body).
func (e *env) sendCommand(env messaging.Envelope) {
	e.t.Helper()
	body, err := json.Marshal(env)
	require.NoError(e.t, err)
	_, err = e.sns.Publish(e.ctx, &awssns.PublishInput{
		TopicArn: aws.String(e.commandARN),
		Message:  aws.String(string(body)),
		MessageAttributes: map[string]snstypes.MessageAttributeValue{
			"target": {DataType: aws.String("String"), StringValue: aws.String("billing")},
			"type":   {DataType: aws.String("String"), StringValue: aws.String(env.Type)},
		},
	})
	require.NoError(e.t, err)
}

// receiveEvent waits for an event of the given type on the test queue.
func (e *env) receiveEvent(typ string) messaging.Envelope {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		out, err := e.sqs.ReceiveMessage(e.ctx, &awssqs.ReceiveMessageInput{QueueUrl: aws.String(e.testQ), MaxNumberOfMessages: 10, WaitTimeSeconds: 2})
		require.NoError(e.t, err)
		for _, m := range out.Messages {
			env, err := messaging.Decode([]byte(aws.ToString(m.Body)))
			require.NoError(e.t, err)
			if env.Type == typ {
				return env
			}
		}
	}
	e.t.Fatalf("event %s not received", typ)
	return messaging.Envelope{}
}

func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.db.QueryRowContext(e.ctx, query, args...).Scan(&n))
	return n
}

// waitForCount polls a count query until it reaches want.
func (e *env) waitForCount(want int, query string, args ...any) {
	e.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if e.count(query, args...) == want {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.Equal(e.t, want, e.count(query, args...), query)
}

func newEnvelope(t *testing.T, typ, msgID string, payload string) messaging.Envelope {
	t.Helper()
	return messaging.Envelope{
		MessageID:      msgID,
		SagaID:         "saga-it-1",
		Type:           typ,
		OccurredAt:     time.Now().UTC(),
		ServiceOrderID: "01JD8Z4R3Q8M2V7K5N9XWABCDE",
		RequestID:      "req-it-1",
		Payload:        json.RawMessage(payload),
	}
}

const generatePayload = `{"customer_user_id":"3b123072-5ad5-4df8-8d3b-3c2770f11b2a","works":[{"work_id":"11111111-1111-4111-8111-000000000001","name":"Troca de óleo","unit_price":"150.00"}],"supplies":[{"supply_id":"22222222-2222-4222-8222-000000000001","name":"Filtro","quantity":2,"unit_price":"35.50"}]}`

const soID = "01JD8Z4R3Q8M2V7K5N9XWABCDE"

func TestGenerateBudgetPublishesEvent(t *testing.T) {
	e := setup(t, payment.ModeManual)

	e.sendCommand(newEnvelope(t, "GenerateBudget", "7c1a0000-0000-4000-8000-000000000001", generatePayload))

	e.waitForCount(1, "SELECT count(*) FROM budget WHERE service_order_id = $1 AND status = 'GENERATED'", soID)
	ev := e.receiveEvent("BudgetGenerated")
	assert.Equal(t, soID, ev.ServiceOrderID)
	assert.Equal(t, "saga-it-1", ev.SagaID)
	var payload map[string]string
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	assert.Equal(t, "221.00", payload["total_amount"])
}

func TestDuplicateMessageAppliesOnce(t *testing.T) {
	e := setup(t, payment.ModeManual)

	msg := newEnvelope(t, "GenerateBudget", "7c1a0000-0000-4000-8000-000000000002", generatePayload)
	e.sendCommand(msg)
	e.sendCommand(msg)

	e.waitForCount(1, "SELECT count(*) FROM outbox WHERE type = 'BudgetGenerated' AND published_at IS NOT NULL")
	time.Sleep(3 * time.Second) // give a redelivery the chance to show up
	assert.Equal(t, 1, e.count("SELECT count(*) FROM budget WHERE service_order_id = $1", soID))
	assert.Equal(t, 1, e.count("SELECT count(*) FROM processed_message WHERE message_id = $1", msg.MessageID))
}

func TestRequestPaymentByMode(t *testing.T) {
	cases := map[payment.Mode]string{
		payment.ModeAutoReject:  "PaymentFailed",
		payment.ModeAutoApprove: "PaymentApproved",
	}
	for mode, want := range cases {
		t.Run(string(mode), func(t *testing.T) {
			e := setup(t, mode)
			e.sendCommand(newEnvelope(t, "GenerateBudget", "7c1a0000-0000-4000-8000-0000000000a1", generatePayload))
			e.waitForCount(1, "SELECT count(*) FROM budget WHERE service_order_id = $1", soID)
			e.sendCommand(newEnvelope(t, "RequestPayment", "7c1a0000-0000-4000-8000-0000000000a2", `{"amount":"221.00"}`))

			ev := e.receiveEvent(want)
			assert.Equal(t, "saga-it-1", ev.SagaID)
		})
	}
}

func TestManualPaymentConfirmViaREST(t *testing.T) {
	e := setup(t, payment.ModeManual)
	e.sendCommand(newEnvelope(t, "GenerateBudget", "7c1a0000-0000-4000-8000-0000000000b1", generatePayload))
	e.waitForCount(1, "SELECT count(*) FROM budget WHERE service_order_id = $1", soID)
	e.sendCommand(newEnvelope(t, "RequestPayment", "7c1a0000-0000-4000-8000-0000000000b2", `{"amount":"221.00"}`))
	e.waitForCount(1, "SELECT count(*) FROM payment WHERE status = 'PENDING' AND service_order_id = $1", soID)

	var paymentID string
	require.NoError(t, e.db.QueryRow("SELECT id FROM payment WHERE service_order_id = $1", soID).Scan(&paymentID))

	token, err := auth.GenerateToken("staff-1", []auth.Role{auth.ATTENDANT}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	srv := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/payments/"+paymentID+"/confirm", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Request-Id", "req-rest-confirm")
	e.router.ServeHTTP(srv, req)
	require.Equal(t, http.StatusNoContent, srv.Code)

	ev := e.receiveEvent("PaymentApproved")
	assert.Equal(t, "saga-it-1", ev.SagaID)
	assert.Equal(t, "req-rest-confirm", ev.RequestID)
}
