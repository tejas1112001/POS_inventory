package sales

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestMapMethod(t *testing.T) {
	for in, want := range map[string]string{"": "cash", "Cash": "cash", "UPI": "upi", "GPay UPI": "upi", "Credit Card": "card",
		"card": "card", "Cheque": "cheque", "Bank Transfer": "deposit", "NEFT": "deposit", "Points": "points", "credit": "card", "Other": "cash"} {
		require.Equal(t, want, MapMethod(in), in)
	}
}

func TestSettle(t *testing.T) {
	total := dec("1000")
	// Old fields: missing paid = paid in full, one row.
	st, err := settle(total, cleanSale{}, "INR")
	require.NoError(t, err)
	require.Equal(t, "Paid", st.status)
	require.Len(t, st.rows, 1)
	require.Equal(t, "cash", st.rows[0].method)

	// Split: cash with change, UPI, rest on account.
	ten := dec("700")
	c := cleanSale{payments: []payLine{{method: "cash", amount: dec("500"), tendered: &ten}, {method: "upi", amount: dec("300"), reference: "txn"}, {method: "credit"}}}
	st, err = settle(total, c, "INR")
	require.NoError(t, err)
	require.True(t, st.paid.Equal(dec("800")))
	require.True(t, st.due.Equal(dec("200")))
	require.Equal(t, "Partial", st.status)
	require.Equal(t, []string{"cash", "upi", "credit"}, []string{st.rows[0].method, st.rows[1].method, st.rows[2].method})
	require.True(t, st.rows[0].change.Equal(dec("200")))
	require.True(t, st.rows[2].amount.Equal(dec("200")))
	require.Equal(t, "cash+upi+credit", st.methodLabel(nil))
	require.True(t, st.chg.Equal(dec("200")))

	// Nothing paid: all on account.
	st, err = settle(total, cleanSale{payments: []payLine{}}, "INR")
	require.NoError(t, err)
	require.Equal(t, "Unpaid", st.status)
	require.Len(t, st.rows, 1)

	// Refused: more than the total, cash tendered below the cash amount.
	_, err = settle(total, cleanSale{payments: []payLine{{method: "card", amount: dec("1000.01")}}}, "INR")
	require.Error(t, err)
	low := dec("100")
	_, err = settle(total, cleanSale{payments: []payLine{{method: "cash", amount: dec("500"), tendered: &low}}}, "INR")
	require.Error(t, err)
}

func TestValidatePayments(t *testing.T) {
	f := fieldErrs{}
	validatePayments([]paymentIn{{Method: "bitcoin"}}, f)
	require.Contains(t, f, "payments[0].method")
	f = fieldErrs{}
	validatePayments([]paymentIn{{Method: "upi"}}, f)
	require.Contains(t, f, "payments[0].amount")
}
