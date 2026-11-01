package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupAWS starts a fake AWS endpoint and points the SDK clients at it.
// SNS uses the query protocol; SQS uses the JSON protocol (X-Amz-Target header).
func setupAWS(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handle))
	t.Cleanup(srv.Close)

	t.Setenv("AWS_ENDPOINT_URL", srv.URL)
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
}

const snsPublishOK = `<PublishResponse xmlns="http://sns.amazonaws.com/doc/2010-03-31/"><PublishResult><MessageId>m-1</MessageId></PublishResult></PublishResponse>`
const snsFail = `<ErrorResponse><Error><Type>Sender</Type><Code>Bad</Code><Message>nope</Message></Error><RequestId>r</RequestId></ErrorResponse>`

func TestSNSPublisher(t *testing.T) {
	var got map[string]string
	setupAWS(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assert.Equal(t, "Publish", r.PostForm.Get("Action"))
		got = map[string]string{"topic": r.PostForm.Get("TopicArn"), "message": r.PostForm.Get("Message")}
		for i := 1; i <= 2; i++ {
			name := r.PostForm.Get(fmt.Sprintf("MessageAttributes.entry.%d.Name", i))
			got[name] = r.PostForm.Get(fmt.Sprintf("MessageAttributes.entry.%d.Value.StringValue", i))
		}
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, snsPublishOK)
	})

	client, err := NewSNSClient(context.Background())
	require.NoError(t, err)
	require.NoError(t, NewSNSPublisher(client).Publish(context.Background(), "arn:topic", `{"a":1}`, map[string]string{"type": "T", "target": "billing"}))

	assert.Equal(t, "arn:topic", got["topic"])
	assert.Equal(t, `{"a":1}`, got["message"])
	assert.Equal(t, "T", got["type"])
	assert.Equal(t, "billing", got["target"])
}

func TestSNSPublisher_Error(t *testing.T) {
	setupAWS(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, snsFail)
	})
	client, err := NewSNSClient(context.Background())
	require.NoError(t, err)
	assert.Error(t, NewSNSPublisher(client).Publish(context.Background(), "arn", "{}", nil))
}

func TestSQSQueue(t *testing.T) {
	var deleted string
	setupAWS(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var in map[string]any
		_ = json.Unmarshal(body, &in)
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")

		switch target := r.Header.Get("X-Amz-Target"); {
		case strings.HasSuffix(target, ".ReceiveMessage"):
			assert.EqualValues(t, 10, in["MaxNumberOfMessages"])
			assert.EqualValues(t, 20, in["WaitTimeSeconds"])
			_, _ = io.WriteString(w, `{"Messages":[{"MessageId":"1","ReceiptHandle":"rh-1","Body":"{\"a\":1}"}]}`)
		case strings.HasSuffix(target, ".DeleteMessage"):
			deleted, _ = in["ReceiptHandle"].(string)
			_, _ = io.WriteString(w, `{}`)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"__type":"BadRequest","message":"nope"}`)
		}
	})

	client, err := NewSQSClient(context.Background())
	require.NoError(t, err)
	q := NewSQSQueue(client)

	msgs, err := q.ReceiveMessage(context.Background(), "http://queue/q", 10, 20)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, `{"a":1}`, msgs[0].Body)
	assert.Equal(t, "rh-1", msgs[0].ReceiptHandle)

	require.NoError(t, q.DeleteMessage(context.Background(), "http://queue/q", "rh-1"))
	assert.Equal(t, "rh-1", deleted)
}

func TestSQSQueue_Errors(t *testing.T) {
	setupAWS(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"__type":"BadRequest","message":"nope"}`)
	})
	client, err := NewSQSClient(context.Background())
	require.NoError(t, err)
	q := NewSQSQueue(client)

	_, err = q.ReceiveMessage(context.Background(), "http://queue/q", 1, 0)
	assert.Error(t, err)
	assert.Error(t, q.DeleteMessage(context.Background(), "http://queue/q", "x"))
}

func TestClients_WithoutEndpointOverride(t *testing.T) {
	t.Setenv("AWS_ENDPOINT_URL", "")
	t.Setenv("AWS_REGION", "")
	_, err := NewSNSClient(context.Background())
	assert.NoError(t, err)
	_, err = NewSQSClient(context.Background())
	assert.NoError(t, err)
}
