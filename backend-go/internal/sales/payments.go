package sales

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Split and part payment (S10 item 5, docs/S10_DESIGN.md §5, B6).
//
// A sale is paid with one or more methods; "credit" puts the rest on the
// customer's account. paid = the non-credit payments, due = total − paid
// (later reduced by collections and by returns, option A). Selling a
// completed sale with anything due needs sales:credit and a named customer
// (never Walk-in), and the customer's dues after the sale must stay within
// their credit limit (no limit set = no credit). Pending orders are not
// handed over yet and are not checked.

// Payment methods (sale_payments.method).
const (
	MethodCash    = "cash"
	MethodUPI     = "upi"
	MethodCard    = "card"
	MethodCredit  = "credit"
	MethodCheque  = "cheque"
	MethodDeposit = "deposit"
	MethodPoints  = "points"
)

var saleMethods = map[string]bool{MethodCash: true, MethodUPI: true, MethodCard: true, MethodCredit: true,
	MethodCheque: true, MethodDeposit: true, MethodPoints: true}

// CollectMethods can pay dues later (no credit, no points).
var CollectMethods = map[string]bool{MethodCash: true, MethodUPI: true, MethodCard: true, MethodCheque: true, MethodDeposit: true}

// MapMethod turns a free-text method (the old payment_method field) into
// one of the list, as the backfill migrations do; cash when unknown. The
// old screens send "credit" for a card payment (their button says Card),
// so here it means card; in payments[] "credit" means on account.
func MapMethod(s string) string {
	l := strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(l, "credit"):
		return MethodCard
	case saleMethods[l]:
		return l
	case strings.Contains(l, "upi"):
		return MethodUPI
	case strings.Contains(l, "card"):
		return MethodCard
	case strings.Contains(l, "cheque"), strings.Contains(l, "check"):
		return MethodCheque
	case strings.Contains(l, "bank"), strings.Contains(l, "deposit"), strings.Contains(l, "transfer"),
		strings.Contains(l, "neft"), strings.Contains(l, "imps"), strings.Contains(l, "rtgs"):
		return MethodDeposit
	case strings.Contains(l, "point"):
		return MethodPoints
	}
	return MethodCash
}

// paymentIn is one entry of saleIn.payments.
type paymentIn struct {
	Method    string    `json:"method"`
	Amount    money.Dec `json:"amount"`
	Tendered  money.Dec `json:"tendered"`
	Reference *string   `json:"reference"`
}

// payLine is a validated payment.
type payLine struct {
	method    string
	amount    decimal.Decimal
	tendered  *decimal.Decimal
	change    *decimal.Decimal
	reference string
}

func validatePayments(in []paymentIn, f fieldErrs) []payLine {
	if len(in) > 10 {
		f["payments"] = "At most 10 payments per sale."
		return nil
	}
	out := make([]payLine, 0, len(in))
	for i, p := range in {
		k := fmt.Sprintf("payments[%d]", i)
		m := strings.ToLower(strings.TrimSpace(p.Method))
		if !saleMethods[m] {
			f[k+".method"] = "Method must be cash, upi, card, credit, cheque, deposit or points."
		}
		l := payLine{method: m}
		if m != MethodCredit {
			if !p.Amount.Valid || !p.Amount.D.IsPositive() {
				f[k+".amount"] = "Amount must be above 0."
			}
			l.amount = p.Amount.D
		}
		if p.Tendered.Valid && p.Tendered.D.IsPositive() {
			if m != MethodCash {
				f[k+".tendered"] = "Only cash has an amount tendered."
			}
			t := p.Tendered.D
			l.tendered = &t
		}
		if p.Reference != nil {
			l.reference = strings.TrimSpace(*p.Reference)
			if len([]rune(l.reference)) > 100 {
				f[k+".reference"] = "At most 100 characters."
			}
		}
		out = append(out, l)
	}
	return out
}

// settlement is how a sale is paid: the rows to store and the totals.
type settlement struct {
	rows          []payLine // non-credit rows, then one credit row for what is due
	paid, due     decimal.Decimal
	tendered, chg *decimal.Decimal
	status        string
	method        string // the main method, for the old payment_method column
}

// settle works out the payments of a sale. With explicit payments, paid is
// their non-credit sum (refused above the total); without, the old fields
// (paid, payment_method, tendered) give one row. Amounts are rounded to the
// currency.
func settle(total decimal.Decimal, c cleanSale, currency string) (settlement, error) {
	var st settlement
	if c.payments != nil {
		for _, p := range c.payments {
			if p.method == MethodCredit {
				continue
			}
			p.amount = money.Round(p.amount, currency)
			if p.tendered != nil {
				t := money.Round(*p.tendered, currency)
				if t.LessThan(p.amount) {
					return st, apperr.Invalid("payments", "Cash tendered cannot be less than the cash amount.")
				}
				chg := t.Sub(p.amount)
				p.tendered, p.change = &t, &chg
			}
			st.rows = append(st.rows, p)
			st.paid = st.paid.Add(p.amount)
		}
		if st.paid.GreaterThan(total) {
			return st, apperr.Invalid("payments", "The payments are more than the total. Give change on cash instead.")
		}
	} else {
		pm := pay(total, c.paid, c.tendered, currency)
		st.paid = pm.paid
		if pm.paid.IsPositive() {
			l := payLine{method: MapMethod(deref(c.paymentMethod)), amount: pm.paid}
			if pm.tendered != nil && l.method == MethodCash {
				l.tendered, l.change = pm.tendered, pm.chg
			}
			st.rows = append(st.rows, l)
		}
	}
	st.due = total.Sub(st.paid)
	switch {
	case st.paid.GreaterThanOrEqual(total):
		st.status = "Paid"
	case st.paid.IsPositive():
		st.status = "Partial"
	default:
		st.status = "Unpaid"
	}
	if st.due.IsPositive() {
		st.rows = append(st.rows, payLine{method: MethodCredit, amount: st.due})
	}
	// The old columns: the cash tendered and change, and the main method.
	for _, r := range st.rows {
		if r.tendered != nil {
			t, ch := *r.tendered, *r.change
			if st.tendered != nil {
				t, ch = st.tendered.Add(t), st.chg.Add(ch)
			}
			st.tendered, st.chg = &t, &ch
		}
	}
	if len(st.rows) > 0 {
		st.method = st.rows[0].method
	}
	return st, nil
}

// methodLabel is how the old payment_method column shows the payments.
func (st settlement) methodLabel(old *string) string {
	if len(st.rows) == 0 {
		return deref(old)
	}
	seen := map[string]bool{}
	var parts []string
	for _, r := range st.rows {
		if !seen[r.method] {
			seen[r.method] = true
			parts = append(parts, r.method)
		}
	}
	return strings.Join(parts, "+")
}

// Symbol is the currency sign for messages.
func Symbol(currency string) string {
	switch currency {
	case "INR":
		return "₹"
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	}
	return currency + " "
}

// creditCheck applies B6 to a completed sale with an amount due. exclude is
// the sale being edited (its old due is not counted twice). The customer
// row is locked, so two tills cannot both use the last of a limit.
func creditCheck(ctx context.Context, tx tenancy.TenantTx, p auth.Principal, s settings, customerID, exclude int64,
	named bool, due decimal.Decimal) error {
	if !due.IsPositive() {
		return nil
	}
	if !p.Has("sales:credit") {
		return apperr.Forbidden(apperr.CodeForbidden, "Selling on credit or taking part payment needs the 'Sell on credit' permission. Take the full amount or ask the owner.")
	}
	if !named || (s.defaultCustomer != nil && *s.defaultCustomer == customerID) {
		return apperr.Invalid("customerId", "Choose a named customer to sell on credit (not Walk-in).")
	}
	var name string
	var limit *decimal.Decimal
	if err := tx.QueryRow(ctx, `SELECT name, credit_limit FROM customers WHERE id = $1 FOR UPDATE`, customerID).Scan(&name, &limit); err != nil {
		return err
	}
	var owed decimal.Decimal
	if err := tx.QueryRow(ctx, `SELECT coalesce(sum(due), 0) FROM sales WHERE customer_id = $1 AND status <> 'Void' AND due > 0 AND id <> $2`,
		customerID, exclude).Scan(&owed); err != nil {
		return err
	}
	lim := decimal.Zero
	if limit != nil {
		lim = *limit
	}
	if owed.Add(due).LessThanOrEqual(lim) {
		return nil
	}
	take := decimal.Min(owed.Add(due).Sub(lim), due)
	sym := Symbol(s.currency)
	msg := fmt.Sprintf("Credit limit reached: %s owes %s%s of %s%s. Take %s%s now or ask the owner to raise the limit.",
		name, sym, money.Round(owed, s.currency).StringFixed(2), sym, lim.StringFixed(2), sym, take.StringFixed(2))
	return apperr.Conflict(CodeCreditLimit, msg).WithDetails(map[string]any{
		"customer": name, "owed": owed, "limit": lim, "takeNow": take, "creditLimitSet": limit != nil})
}

// CodeCreditLimit is the error code of a refused credit sale.
const CodeCreditLimit = "CREDIT_LIMIT"

// dueDate is the due date of a sale with something due: the one given, or
// 30 days after the sale's day in the shop's time zone. It is a calendar
// day, returned at midnight UTC so the date column stores that day.
func dueDate(given string, saleDate time.Time, loc *time.Location) (*time.Time, error) {
	sd := saleDate.In(loc)
	saleDay := time.Date(sd.Year(), sd.Month(), sd.Day(), 0, 0, 0, 0, time.UTC)
	if given != "" {
		d, err := time.Parse("2006-01-02", given)
		if err != nil {
			return nil, apperr.Invalid("dueDate", "Use the format YYYY-MM-DD.")
		}
		if d.Before(saleDay) {
			return nil, apperr.Invalid("dueDate", "The due date cannot be before the sale.")
		}
		return &d, nil
	}
	d := saleDay.AddDate(0, 0, 30)
	return &d, nil
}

const qInsertPayment = `INSERT INTO sale_payments (sale_id, method, amount, tendered, change_due, reference, user_id, created_at)
	VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)`

// writePayments replaces a sale's own payment rows (never the rows of
// collections against dues).
func writePayments(ctx context.Context, tx tenancy.TenantTx, saleID, userID int64, at time.Time, rows []payLine) error {
	if _, err := tx.Exec(ctx, `DELETE FROM sale_payments WHERE sale_id = $1 AND customer_payment_id IS NULL`, saleID); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.Exec(ctx, qInsertPayment, saleID, r.method, r.amount, r.tendered, r.change, r.reference, userID, at); err != nil {
			return err
		}
	}
	return nil
}

// refuseIfCollected stops an edit or void of a sale that a collection
// against dues has paid: reverse the collection first.
func refuseIfCollected(ctx context.Context, tx tenancy.TenantTx, saleID int64, action string) error {
	var paid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customer_payment_allocations WHERE sale_id = $1)`, saleID).Scan(&paid); err != nil {
		return err
	}
	if paid {
		return apperr.Conflict(apperr.CodeInvalidState, "A payment against dues was collected for this sale, so it cannot be "+action+". Reverse that payment first.")
	}
	return nil
}

// PaymentJSON is one payment of a sale.
type PaymentJSON struct {
	ID                int64            `json:"id"`
	Method            string           `json:"method"`
	Amount            decimal.Decimal  `json:"amount"`
	Tendered          *decimal.Decimal `json:"tendered"`
	Change            *decimal.Decimal `json:"change"`
	Reference         *string          `json:"reference"`
	CustomerPaymentID *int64           `json:"customerPaymentId"`
	CreatedAt         time.Time        `json:"createdAt"`
}

// ownPayments are a sale's own non-credit payments (not collections).
func ownPayments(ctx context.Context, tx tenancy.TenantTx, saleID int64) ([]payLine, error) {
	rows, err := tx.Query(ctx, `SELECT method, amount, tendered, coalesce(reference, '') FROM sale_payments
		WHERE sale_id = $1 AND customer_payment_id IS NULL AND method <> 'credit' ORDER BY id`, saleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []payLine{}
	for rows.Next() {
		var l payLine
		if err := rows.Scan(&l.method, &l.amount, &l.tendered, &l.reference); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
