package mercadopago

import (
	"time"

	"github.com/mercadopago/sdk-go/pkg/webhook"
)

// Verifier checks the x-signature header of a webhook with the SDK helper: it rebuilds the manifest
// "id:<data.id>;request-id:<x-request-id>;ts:<ts>;", compares the HMAC-SHA256 in constant time and
// rejects timestamps further than Tolerance from now.
type Verifier struct {
	Secret    string
	Tolerance time.Duration
	Now       func() time.Time
}

// Verify returns nil when the signature is valid. A Verifier without a secret rejects everything.
func (v Verifier) Verify(xSignature, xRequestID, dataID string) error {
	opts := []webhook.Option{webhook.WithTolerance(v.Tolerance)}
	if v.Now != nil {
		opts = append(opts, webhook.WithNow(v.Now))
	}
	if v.Secret == "" {
		return errNoSecret
	}
	return webhook.ValidateSignature(xSignature, xRequestID, dataID, v.Secret, opts...)
}

var errNoSecret = errorString("webhook secret not configured")

type errorString string

func (e errorString) Error() string { return string(e) }
