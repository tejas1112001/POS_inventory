package reports

import (
	"net/http"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// PaymentMethodRow is one line of the "sales by payment method" report
// (S10 item 5): money taken with that method in the period, at the till
// and as collections against dues (reversals subtract). "credit" is what
// was put on account.
type PaymentMethodRow struct {
	Method      string          `json:"method"`
	Payments    int             `json:"payments"`
	Amount      decimal.Decimal `json:"amount"`
	Collections decimal.Decimal `json:"collections"` // the part from collections against dues
	// Verification is "recorded" on every row (industry P1 item 2): the
	// cashier recorded these payments; none is verified by a provider.
	// Recorded is true where screens label the method "(recorded)" (Q4).
	Verification string `json:"verification"`
	Recorded     bool   `json:"recorded"`
}

// recordedMethods are labelled "(recorded)" (Q4: all but cash; "credit"
// is on account and points are not money taken).
var recordedMethods = map[string]bool{"upi": true, "card": true, "cheque": true, "deposit": true, "exchange": true, "store_credit": true}

const qPaymentMethods = `SELECT p.method, count(*), coalesce(sum(p.amount), 0),
		coalesce(sum(p.amount) FILTER (WHERE p.customer_payment_id IS NOT NULL), 0)
	FROM sale_payments p JOIN sales s ON s.id = p.sale_id
	WHERE s.status <> 'Void' AND ($1 = 0 OR s.user_id = $1)
	  AND ($2::timestamptz IS NULL OR p.created_at >= $2) AND ($3::timestamptz IS NULL OR p.created_at < $3)
	  AND ($4 = 0 OR s.store_id = $4) AND ($5 = 0 OR s.customer_id = $5)
	GROUP BY p.method ORDER BY sum(p.amount) DESC, p.method`

// paymentMethods is GET /api/sales/report/payment-methods (own-only
// without sales:view_all, as the sales report).
func (a *API) paymentMethods(w http.ResponseWriter, r *http.Request) {
	out := []PaymentMethodRow{}
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		b, err := settings.LoadBusiness(r.Context(), tx)
		if err != nil {
			return err
		}
		f, err := a.salesFilter(r, b)
		if err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qPaymentMethods, f.Scope, f.From, f.To, f.StoreID, f.CustomerID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x PaymentMethodRow
			if err := rows.Scan(&x.Method, &x.Payments, &x.Amount, &x.Collections); err != nil {
				return err
			}
			x.Verification, x.Recorded = "recorded", recordedMethods[x.Method]
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
