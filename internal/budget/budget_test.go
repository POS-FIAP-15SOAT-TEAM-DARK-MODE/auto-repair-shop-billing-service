package budget

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seq() func() string {
	n := 0
	return func() string {
		n++
		return "id-" + decimal.NewFromInt(int64(n)).String()
	}
}

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

func TestNewBudget_TotalOnlyWorks(t *testing.T) {
	b, err := NewBudget(seq(), "SO-1", "cust",
		[]WorkLine{{WorkID: "w1", Name: "Oil", UnitPrice: d("150.00")}, {WorkID: "w2", Name: "Brake", UnitPrice: d("80.50")}},
		nil)
	require.NoError(t, err)
	assert.Equal(t, "230.50", b.TotalAmount.StringFixed(2))
	assert.Equal(t, StatusGenerated, b.Status)
	require.Len(t, b.Items, 2)
	assert.Equal(t, KindWork, b.Items[0].Kind)
	assert.Equal(t, 1, b.Items[0].Quantity)
}

func TestNewBudget_TotalMixed(t *testing.T) {
	b, err := NewBudget(seq(), "SO-1", "cust",
		[]WorkLine{{WorkID: "w1", Name: "Oil", UnitPrice: d("150.00")}},
		[]SupplyLine{{SupplyID: "s1", Name: "Filter", Quantity: 2, UnitPrice: d("35.50")}})
	require.NoError(t, err)
	assert.Equal(t, "221.00", b.TotalAmount.StringFixed(2))
	assert.Equal(t, KindSupply, b.Items[1].Kind)
	assert.Equal(t, 2, b.Items[1].Quantity)
}

func TestNewBudget_RoundingHalfAwayFromZero(t *testing.T) {
	b, err := NewBudget(seq(), "SO-1", "cust",
		[]WorkLine{{WorkID: "w1", Name: "x", UnitPrice: d("10.005")}},
		[]SupplyLine{{SupplyID: "s1", Name: "y", Quantity: 3, UnitPrice: d("0.333")}})
	require.NoError(t, err)
	// unit prices are rounded to 2 decimals before multiplying: 10.01 + 3 * 0.33 = 11.00
	assert.Equal(t, "11.00", b.TotalAmount.StringFixed(2))
	assert.Equal(t, "0.33", b.Items[1].UnitPrice.StringFixed(2))
}

func TestNewBudget_Errors(t *testing.T) {
	work := []WorkLine{{WorkID: "w1", Name: "x", UnitPrice: d("1")}}

	_, err := NewBudget(seq(), "SO-1", "", work, nil)
	assert.ErrorIs(t, err, ErrCustomerRequired)

	_, err = NewBudget(seq(), "SO-1", "cust", nil, []SupplyLine{{Quantity: 1, UnitPrice: d("1")}})
	assert.ErrorIs(t, err, ErrNoWorks)

	_, err = NewBudget(seq(), "SO-1", "cust", []WorkLine{{UnitPrice: d("-1")}}, nil)
	assert.ErrorIs(t, err, ErrNegativePrice)

	_, err = NewBudget(seq(), "SO-1", "cust", work, []SupplyLine{{Quantity: 1, UnitPrice: d("-0.001")}})
	assert.ErrorIs(t, err, ErrNegativePrice)

	_, err = NewBudget(seq(), "SO-1", "cust", work, []SupplyLine{{Quantity: 0, UnitPrice: d("1")}})
	assert.ErrorIs(t, err, ErrInvalidQuantity)
}

func TestBudget_Cancel(t *testing.T) {
	b := &Budget{Status: StatusGenerated}
	assert.True(t, b.Cancel())
	assert.Equal(t, StatusCancelled, b.Status)
	assert.False(t, b.Cancel(), "already cancelled: no change")
}
