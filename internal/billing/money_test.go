package billing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoney(t *testing.T) {
	d, err := parseMoney("221")
	require.NoError(t, err)
	assert.Equal(t, "221.00", formatMoney(d))

	d, err = parseMoney("10.005")
	require.NoError(t, err)
	assert.Equal(t, "10.01", formatMoney(d))

	_, err = parseMoney("abc")
	assert.ErrorIs(t, err, ErrInvalidPayload)
}
