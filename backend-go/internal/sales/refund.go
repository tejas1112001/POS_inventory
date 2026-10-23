package sales

import (
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
)

// refundShare is the part of a sold line's amount that k more of its sold
// units refund, after before units were returned already. The cut is made
// on the running quantity, so a line returned in parts adds up to exactly
// the line amount, and a full return refunds exactly what was charged.
func refundShare(amount decimal.Decimal, sold, before, k int64, currency string) decimal.Decimal {
	if sold <= 0 || k <= 0 {
		return decimal.Zero
	}
	n := decimal.NewFromInt(sold)
	at := func(q int64) decimal.Decimal {
		if q >= sold {
			return amount
		}
		return money.Round(amount.Mul(decimal.NewFromInt(q)).Div(n), currency)
	}
	return at(before + k).Sub(at(before))
}

// refundSharePtr is refundShare for an optional amount (a GST component
// that is not set on the line stays unset).
func refundSharePtr(amount *decimal.Decimal, sold, before, k int64, currency string) *decimal.Decimal {
	if amount == nil {
		return nil
	}
	v := refundShare(*amount, sold, before, k, currency)
	return &v
}
