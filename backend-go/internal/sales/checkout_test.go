package sales

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
)

func strict(t *testing.T, body string) (saleIn, error) {
	t.Helper()
	var in saleIn
	dec := json.NewDecoder(bytes.NewReader([]byte(body)))
	dec.DisallowUnknownFields()
	return in, dec.Decode(&in)
}

// posBody is exactly what POS.jsx sends: the PayForm state spread in,
// plus method/amountTendered/total, then the cart.
const posBody = `{
	"receivedAmount": "500", "payingAmount": "284.38", "change": "215.62", "paymentType": "cash",
	"paymentNote": "note p", "saleNote": "note s", "quickCash": "500",
	"method": "cash", "amountTendered": 500, "total": "284.38",
	"orderItems": [{"productId": 1, "variantId": 2, "quantity": 2, "unitPrice": 120.5, "total": 241}],
	"customerId": 3, "storeId": 4, "subtotal": 241, "discount": 0, "tax": 43.38
}`

func TestPOSPayloadDecodesStrictly(t *testing.T) {
	in, err := strict(t, posBody)
	require.NoError(t, err)
	c, err := validate(in)
	require.NoError(t, err)
	require.Equal(t, "cash", *c.paymentMethod)
	require.Equal(t, "note p", *c.paymentNote)
	require.Equal(t, "note s", *c.note)
	require.True(t, c.tendered.Equal(decimal.NewFromInt(500)))
	require.Nil(t, c.paid, "POS sends no paid: paid in full")
	require.Equal(t, int64(4), c.storeID)
	require.Len(t, c.lines, 1)
	require.True(t, c.lines[0].priceSet)
}

func TestBackOfficePayloadDecodes(t *testing.T) {
	// AddSales.jsx: no variantId, plus userId/reference/payment_status/due.
	in, err := strict(t, `{"reference": "REF1", "date": "2026-10-01", "status": "Pending", "payment_status": "Unpaid",
		"payment_method": "", "subtotal": 10, "discount": 0, "tax": 0, "shipping": 5, "total": 15, "paid": 0, "due": 15,
		"note": "", "customerId": "3", "userId": 1, "storeId": "4",
		"orderItems": [{"productId": 9, "quantity": 1, "unitPrice": 10, "total": 10}]}`)
	require.NoError(t, err)
	c, err := validate(in)
	require.NoError(t, err)
	require.Equal(t, "Pending", c.status)
	require.True(t, c.paid.IsZero(), "paid: 0 is kept")
	require.Equal(t, int64(0), c.lines[0].variantID)
	require.Equal(t, int64(9), c.lines[0].productID)
}

func TestUnknownKeysAndBadInput(t *testing.T) {
	_, err := strict(t, `{"storeId": 1, "orderItems": [], "isAdmin": true}`)
	require.Error(t, err, "keys outside the contract and ignore list are refused")

	cases := map[string]string{
		"storeId":                `{"orderItems": [{"variantId": 1, "quantity": 1}]}`,
		"orderItems":             `{"storeId": 1, "orderItems": []}`,
		"orderItems[0].quantity": `{"storeId": 1, "orderItems": [{"variantId": 1, "quantity": 0}]}`,
		"orderItems[0].discount": `{"storeId": 1, "orderItems": [{"variantId": 1, "quantity": 1, "discount": 5}]}`,
		"status":                 `{"storeId": 1, "status": "Void", "orderItems": [{"variantId": 1, "quantity": 1}]}`,
		"discount":               `{"storeId": 1, "discount": -1, "orderItems": [{"variantId": 1, "quantity": 1}]}`,
		"receiptEmail":           `{"storeId": 1, "receiptEmail": "nope", "orderItems": [{"variantId": 1, "quantity": 1}]}`,
	}
	for field, body := range cases {
		in, err := strict(t, body)
		require.NoError(t, err, field)
		_, err = validate(in)
		e, ok := apperr.As(err)
		require.True(t, ok, field)
		require.Contains(t, e.Fields, field)
	}
	in, _ := strict(t, `{"storeId": 1, "orderItems": [{"variantId": 1, "quantity": 1.5}]}`)
	_, err = validate(in)
	require.Error(t, err, "fractional quantity")
}

func TestPay(t *testing.T) {
	d := decimal.RequireFromString
	total := d("100")
	p := pay(total, nil, nil, "INR")
	require.Equal(t, "Paid", p.status)
	require.True(t, p.due.IsZero())

	zero := decimal.Zero
	p = pay(total, &zero, nil, "INR")
	require.Equal(t, "Unpaid", p.status, "paid: 0 is respected")
	require.True(t, p.due.Equal(total))

	part := d("40.005")
	p = pay(total, &part, nil, "INR")
	require.Equal(t, "Partial", p.status)
	require.Equal(t, "40.01", p.paid.StringFixed(2), "rounded to the currency")

	over := d("150")
	tend := d("200")
	p = pay(total, &over, &tend, "INR")
	require.True(t, p.paid.Equal(total), "paid is capped at the total")
	require.True(t, p.chg.Equal(d("100")))

	p = pay(decimal.Zero, nil, nil, "INR")
	require.Equal(t, "Paid", p.status, "a free sale is paid")
}

func TestParseDate(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	now := time.Date(2026, 10, 20, 20, 0, 0, 0, time.UTC) // 01:30 on the 21st in Kolkata

	got, err := parseDate("", loc, now)
	require.NoError(t, err)
	require.Equal(t, now, got)

	got, err = parseDate("2026-10-21", loc, now)
	require.NoError(t, err)
	require.Equal(t, now, got, "today in the tenant's zone means now")

	got, err = parseDate("2026-10-01", loc, now)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, loc), got)

	got, err = parseDate("2026-10-20T10:00:00Z", loc, now)
	require.NoError(t, err)
	require.True(t, got.Equal(time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)))

	_, err = parseDate("2026-12-01", loc, now)
	require.Error(t, err, "future")
	_, err = parseDate("01/10/2026", loc, now)
	require.Error(t, err)
}

func TestBound(t *testing.T) {
	loc := time.UTC
	from, err := bound("2026-10-01", loc, false)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, loc), *from)
	to, err := bound("2026-10-01", loc, true)
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 10, 2, 0, 0, 0, 0, loc), *to, "endDate includes the whole day")
	none, err := bound(" ", loc, false)
	require.NoError(t, err)
	require.Nil(t, none)
	_, err = bound("yesterday", loc, false)
	require.Error(t, err)
}
