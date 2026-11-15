package billing

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"
)

var ErrInvalidPayload = errors.New("invalid message payload")

func parseMoney(s string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero, fmt.Errorf("%w: money %q", ErrInvalidPayload, s)
	}
	return d.Round(2), nil
}

func formatMoney(d decimal.Decimal) string {
	return d.StringFixed(2)
}
