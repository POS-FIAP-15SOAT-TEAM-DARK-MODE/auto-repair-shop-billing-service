package mercadopago

import (
	"strconv"
	"strings"
	"time"

	"github.com/mercadopago/sdk-go/pkg/webhook"
)

// Verifier checks the x-signature header of a webhook with the SDK helper: it rebuilds the manifest
// "id:<data.id>;request-id:<x-request-id>;ts:<ts>;", compares the HMAC-SHA256 in constant time and
// rejects timestamps further than Tolerance from now.
//
// The tolerance is checked here, not by the SDK: the SDK reads ts as milliseconds, but Mercado Pago
// sends it in seconds (10 digits) in real notifications and in the panel's simulation.
type Verifier struct {
	Secret    string
	Tolerance time.Duration
	Now       func() time.Time
}

// Verify returns nil when the signature is valid. A Verifier without a secret rejects everything.
func (v Verifier) Verify(xSignature, xRequestID, dataID string) error {
	if v.Secret == "" {
		return errNoSecret
	}
	if err := webhook.ValidateSignature(xSignature, xRequestID, dataID, v.Secret); err != nil {
		return err
	}
	return v.checkFresh(xSignature, xRequestID)
}

// checkFresh rejects a header whose ts is further than Tolerance from now. ts is read as seconds, or
// as milliseconds when it has 13 digits or more.
func (v Verifier) checkFresh(xSignature, xRequestID string) error {
	var raw string
	for _, part := range strings.Split(xSignature, ",") {
		if k, val, ok := strings.Cut(strings.TrimSpace(part), "="); ok && k == "ts" {
			raw = val
		}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return &webhook.SignatureError{Reason: webhook.ReasonMalformedSignatureHeader, RequestID: xRequestID, Timestamp: raw}
	}
	sent := time.Unix(n, 0)
	if n >= 1e12 {
		sent = time.UnixMilli(n)
	}
	now := time.Now
	if v.Now != nil {
		now = v.Now
	}
	drift := now().Sub(sent)
	if drift < 0 {
		drift = -drift
	}
	if drift > v.Tolerance {
		return &webhook.SignatureError{Reason: webhook.ReasonTimestampOutOfTolerance, RequestID: xRequestID, Timestamp: raw}
	}
	return nil
}

var errNoSecret = errorString("webhook secret not configured")

type errorString string

func (e errorString) Error() string { return string(e) }
