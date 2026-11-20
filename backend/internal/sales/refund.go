package sales

import (
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
)

// refundShare is the part of a sold line's amount that k more of its sold
// units refund, after before units were returned already. The cut is made
// on the running quantity, so a line returned in parts adds up to exactly
// the line amount, and a full return refunds exactly what was charged.
func refundShare(amount decimal.Decimal, sold, before, k qty.Q, currency string) decimal.Decimal {
	if !sold.IsPositive() || !k.IsPositive() {
		return decimal.Zero
	}
	n := sold.Decimal()
	at := func(q qty.Q) decimal.Decimal {
		if !q.Less(sold) {
			return amount
		}
		return money.Round(amount.Mul(q.Decimal()).Div(n), currency)
	}
	return at(before.Add(k)).Sub(at(before))
}

// refundSharePtr is refundShare for an optional amount (a GST component
// that is not set on the line stays unset).
func refundSharePtr(amount *decimal.Decimal, sold, before, k qty.Q, currency string) *decimal.Decimal {
	if amount == nil {
		return nil
	}
	v := refundShare(*amount, sold, before, k, currency)
	return &v
}
