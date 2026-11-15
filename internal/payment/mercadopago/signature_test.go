package mercadopago

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// header builds a valid x-signature the way Mercado Pago does, so the tests do not depend on it.
// Mercado Pago sends ts in seconds; headerMs is the same with milliseconds.
func header(secret, dataID, requestID string, at time.Time) string {
	return signed(secret, dataID, requestID, strconv.FormatInt(at.Unix(), 10))
}

func headerMs(secret, dataID, requestID string, at time.Time) string {
	return signed(secret, dataID, requestID, strconv.FormatInt(at.UnixMilli(), 10))
}

func signed(secret, dataID, requestID, ts string) string {
	manifest := "id:" + dataID + ";request-id:" + requestID + ";ts:" + ts + ";"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(manifest))
	return fmt.Sprintf("ts=%s,v1=%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

func TestVerifier(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	v := Verifier{Secret: "s3cret", Tolerance: 5 * time.Minute, Now: func() time.Time { return now }}

	assert.NoError(t, v.Verify(header("s3cret", "186237542593", "req-1", now), "req-1", "186237542593"))
	assert.NoError(t, v.Verify(header("s3cret", "186237542593", "req-1", now.Add(-4*time.Minute)), "req-1", "186237542593"), "inside the tolerance")

	assert.NoError(t, v.Verify(headerMs("s3cret", "186237542593", "req-1", now), "req-1", "186237542593"), "milliseconds are accepted too")
	assert.Error(t, v.Verify(headerMs("s3cret", "186237542593", "req-1", now.Add(-time.Hour)), "req-1", "186237542593"), "stale timestamp in milliseconds")
	assert.Error(t, v.Verify(header("s3cret", "186237542593", "req-1", now.Add(time.Hour)), "req-1", "186237542593"), "timestamp from the future")

	assert.Error(t, v.Verify(header("wrong", "186237542593", "req-1", now), "req-1", "186237542593"), "wrong secret")
	assert.Error(t, v.Verify(header("s3cret", "186237542593", "req-1", now.Add(-time.Hour)), "req-1", "186237542593"), "stale timestamp")
	assert.Error(t, v.Verify(header("s3cret", "186237542593", "req-1", now), "req-1", "999"), "signed for another payment id")
	assert.Error(t, v.Verify(header("s3cret", "186237542593", "req-1", now), "other-request", "186237542593"), "signed for another request id")
	assert.Error(t, v.Verify("", "req-1", "186237542593"), "no header")
	assert.Error(t, v.Verify("garbage", "req-1", "186237542593"), "malformed header")

	assert.Error(t, Verifier{Tolerance: time.Minute}.Verify(header("s3cret", "1", "r", now), "r", "1"), "no secret fails closed")
	assert.NoError(t, Verifier{Secret: "s3cret", Tolerance: time.Minute}.Verify(header("s3cret", "1", "r", time.Now()), "r", "1"), "real clock when Now is not set")
}
