package sales

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/docdate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/idempotency"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Customer dues (S10 item 5, docs/S10_DESIGN.md §5, D6, D7):
//   - GET  /api/customers/{id}/dues: what the customer owes, per sale.
//   - POST /api/customers/{id}/payments (customers:collect): a payment
//     against dues, allocated to the oldest open sales first.
//   - POST /api/customer-payments/{id}/reverse (customers:collect): a
//     wrong collection is reversed by a new negative entry with a reason;
//     collections are never edited or deleted (append-only tables).
//   - GET  /api/customers/{id}/statement: sales, payments, returns and the
//     running balance for a date range.
//   - PUT  /api/customers/{id}/credit-limit (customers:credit_limit).
// The customer row is locked while allocating, as in the credit check.

func customerParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound()
	}
	return id, nil
}

// DueSale is one sale with something due.
type DueSale struct {
	ID        int64           `json:"id"`
	Reference string          `json:"reference"`
	Date      time.Time       `json:"date"`
	Total     decimal.Decimal `json:"total"`
	Paid      decimal.Decimal `json:"paid"`
	Returned  decimal.Decimal `json:"returned"` // returns that reduced the due (option A)
	Due       decimal.Decimal `json:"due"`
	DueDate   *string         `json:"dueDate"`
	Overdue   bool            `json:"overdue"`
}

// DuesJSON is GET /api/customers/{id}/dues.
type DuesJSON struct {
	CustomerID  int64            `json:"customerId"`
	Name        string           `json:"name"`
	Currency    string           `json:"currency"`
	CreditLimit *decimal.Decimal `json:"creditLimit"`
	TotalDue    decimal.Decimal  `json:"totalDue"`
	OverdueDue  decimal.Decimal  `json:"overdueDue"`
	Available   *decimal.Decimal `json:"available"` // credit left; nil without a limit
	Sales       []DueSale        `json:"sales"`
	OwedByShop  decimal.Decimal  `json:"owedByShop"` // returns not yet refunded to the customer
	CanCollect  bool             `json:"canCollect"`
	CanSetLimit bool             `json:"canSetLimit"`
	WalkIn      bool             `json:"walkIn"`
}

const (
	qCustomer = `SELECT c.name, c.credit_limit, s.currency, coalesce(s.default_customer_id = c.id, false)
		FROM customers c CROSS JOIN settings s WHERE c.id = $1 AND c.deleted_at IS NULL AND s.id = 1`
	qCustomerLock = `SELECT c.name, c.credit_limit, s.currency, coalesce(s.default_customer_id = c.id, false)
		FROM customers c CROSS JOIN settings s WHERE c.id = $1 AND c.deleted_at IS NULL AND s.id = 1 FOR UPDATE OF c`
	// Open sales, oldest due first (the allocation order).
	qOpenSales = `SELECT s.id, s.reference, s.date, s.total, s.paid,
			coalesce((SELECT sum(r.applied_to_due) FROM sale_returns r WHERE r.sale_id = s.id), 0),
			s.due, to_char(s.due_date, 'YYYY-MM-DD'), ` + sqlOverdue + `
		FROM sales s WHERE s.customer_id = $1 AND s.status <> 'Void' AND s.due > 0
		ORDER BY coalesce(s.due_date, s.date::date), s.date, s.id`
	qOpenSalesLock = qOpenSales + ` FOR UPDATE OF s`
	qOwedByShop    = `SELECT coalesce(sum(due), 0) FROM sale_returns WHERE customer_id = $1`
)

type customerRow struct {
	name     string
	limit    *decimal.Decimal
	currency string
	walkIn   bool
}

func loadCustomer(ctx context.Context, tx tenancy.TenantTx, id int64, lock bool) (customerRow, error) {
	var c customerRow
	var row pgx.Row
	if lock {
		row = tx.QueryRow(ctx, qCustomerLock, id)
	} else {
		row = tx.QueryRow(ctx, qCustomer, id)
	}
	err := row.Scan(&c.name, &c.limit, &c.currency, &c.walkIn)
	if isNoRows(err) {
		return c, apperr.NotFound()
	}
	return c, err
}

func openSales(ctx context.Context, tx tenancy.TenantTx, id int64, lock bool) ([]DueSale, error) {
	var rows pgx.Rows
	var err error
	if lock {
		rows, err = tx.Query(ctx, qOpenSalesLock, id)
	} else {
		rows, err = tx.Query(ctx, qOpenSales, id)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DueSale{}
	for rows.Next() {
		var s DueSale
		if err := rows.Scan(&s.ID, &s.Reference, &s.Date, &s.Total, &s.Paid, &s.Returned, &s.Due, &s.DueDate, &s.Overdue); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (a *API) dues(w http.ResponseWriter, r *http.Request) {
	id, err := customerParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	out := DuesJSON{CustomerID: id, CanCollect: p.Has("customers:collect"), CanSetLimit: p.Has("customers:credit_limit")}
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		c, err := loadCustomer(r.Context(), tx, id, false)
		if err != nil {
			return err
		}
		out.Name, out.CreditLimit, out.Currency, out.WalkIn = c.name, c.limit, c.currency, c.walkIn
		if out.Sales, err = openSales(r.Context(), tx, id, false); err != nil {
			return err
		}
		for _, s := range out.Sales {
			out.TotalDue = out.TotalDue.Add(s.Due)
			if s.Overdue {
				out.OverdueDue = out.OverdueDue.Add(s.Due)
			}
		}
		if c.limit != nil {
			left := decimal.Max(c.limit.Sub(out.TotalDue), decimal.Zero)
			out.Available = &left
		}
		return tx.QueryRow(r.Context(), qOwedByShop, id).Scan(&out.OwedByShop)
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ------------------------------------------------------------ collections

// Allocation is the part of a collection that paid one sale.
type Allocation struct {
	SaleID    int64           `json:"saleId"`
	Reference string          `json:"reference"`
	Amount    decimal.Decimal `json:"amount"`
}

// CustomerPaymentJSON is a collection against dues (or its reversal).
type CustomerPaymentJSON struct {
	ID          int64           `json:"id"`
	CustomerID  int64           `json:"customerId"`
	Amount      decimal.Decimal `json:"amount"`
	Method      string          `json:"method"`
	Reference   *string         `json:"reference"`
	Note        *string         `json:"note"`
	Currency    string          `json:"currency"`
	ReversalOf  *int64          `json:"reversalOf"`
	ReversedBy  *int64          `json:"reversedBy"`
	Reason      *string         `json:"reason"`
	UserID      int64           `json:"userId"`
	UserName    *string         `json:"userName"`
	CreatedAt   time.Time       `json:"createdAt"`
	Allocations []Allocation    `json:"allocations"`
}

const (
	qLoadCustomerPayments = `SELECT p.id, p.customer_id, p.amount, p.method, p.reference, p.note, p.currency, p.reversal_of,
			(SELECT x.id FROM customer_payments x WHERE x.reversal_of = p.id), p.reason, p.user_id, u.name, p.created_at
		FROM customer_payments p LEFT JOIN users u ON u.id = p.user_id`
	qAllocations = `SELECT a.customer_payment_id, a.sale_id, s.reference, a.amount FROM customer_payment_allocations a
		JOIN sales s ON s.id = a.sale_id WHERE a.customer_payment_id = ANY($1) ORDER BY a.id`
)

func loadCustomerPayments(ctx context.Context, tx tenancy.TenantTx, where string, arg any) ([]CustomerPaymentJSON, error) {
	var rows pgx.Rows
	var err error
	switch where {
	case "id":
		rows, err = tx.Query(ctx, qLoadCustomerPayments+` WHERE p.id = $1`, arg)
	default:
		rows, err = tx.Query(ctx, qLoadCustomerPayments+` WHERE p.customer_id = $1 ORDER BY p.created_at DESC, p.id DESC LIMIT 500`, arg)
	}
	if err != nil {
		return nil, err
	}
	out := []CustomerPaymentJSON{}
	byID := map[int64]int{}
	var ids []int64
	for rows.Next() {
		var p CustomerPaymentJSON
		if err := rows.Scan(&p.ID, &p.CustomerID, &p.Amount, &p.Method, &p.Reference, &p.Note, &p.Currency, &p.ReversalOf,
			&p.ReversedBy, &p.Reason, &p.UserID, &p.UserName, &p.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		p.Allocations = []Allocation{}
		byID[p.ID] = len(out)
		ids = append(ids, p.ID)
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err = tx.Query(ctx, qAllocations, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid int64
		var al Allocation
		if err := rows.Scan(&pid, &al.SaleID, &al.Reference, &al.Amount); err != nil {
			return nil, err
		}
		out[byID[pid]].Allocations = append(out[byID[pid]].Allocations, al)
	}
	return out, rows.Err()
}

func (a *API) customerPayments(w http.ResponseWriter, r *http.Request) {
	id, err := customerParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out []CustomerPaymentJSON
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if _, err := loadCustomer(r.Context(), tx, id, false); err != nil {
			return err
		}
		var err error
		out, err = loadCustomerPayments(r.Context(), tx, "customer", id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type collectIn struct {
	Amount    money.Dec `json:"amount"`
	Method    string    `json:"method"`
	Reference string    `json:"reference"`
	Note      string    `json:"note"`
	StoreID   money.Int `json:"storeId"`
}

const (
	qInsertCustomerPayment = `INSERT INTO customer_payments (customer_id, amount, method, reference, note, currency, reversal_of, reason, store_id, user_id)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7, NULLIF($8, ''), $9, $10) RETURNING id, created_at`
	qInsertAllocation = `INSERT INTO customer_payment_allocations (customer_payment_id, sale_id, amount) VALUES ($1, $2, $3)`
	qInsertCollectPay = `INSERT INTO sale_payments (sale_id, method, amount, reference, customer_payment_id, user_id, created_at) VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7)`
	qApplyToSale      = `UPDATE sales SET paid = paid + $2, due = due - $2,
			payment_status = CASE WHEN due - $2 <= 0 THEN 'Paid' WHEN paid + $2 > 0 THEN 'Partial' ELSE 'Unpaid' END,
			due_date = CASE WHEN due - $2 > 0 THEN coalesce(due_date, (date AT TIME ZONE (SELECT timezone FROM settings WHERE id = 1))::date + 30) ELSE due_date END,
			updated_at = now()
		WHERE id = $1`
)

// collect is POST /api/customers/{id}/payments.
func (a *API) collect(w http.ResponseWriter, r *http.Request) {
	id, err := customerParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	key, err := idempotency.Key(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	hash := idempotency.Hash(r, "collect")
	var in collectIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := hash()
	f := fieldErrs{}
	in.Method = strings.ToLower(strings.TrimSpace(in.Method))
	if !CollectMethods[in.Method] {
		f["method"] = "Method must be cash, upi, card, cheque or deposit."
	}
	if !in.Amount.Valid || !in.Amount.D.IsPositive() {
		f["amount"] = "Amount must be above 0."
	}
	in.Reference, in.Note = strings.TrimSpace(in.Reference), strings.TrimSpace(in.Note)
	if len([]rune(in.Reference)) > 100 {
		f["reference"] = "At most 100 characters."
	}
	if len([]rune(in.Note)) > 500 {
		f["note"] = "At most 500 characters."
	}
	if err := f.err(); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	var (
		out   CustomerPaymentJSON
		again *idempotency.Replay
	)
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if key != "" {
			var err error
			if again, err = idempotency.Claim(ctx, tx, p.UserID, key, sum); err != nil || again != nil {
				return err
			}
		}
		c, err := loadCustomer(ctx, tx, id, true)
		if err != nil {
			return err
		}
		amount := money.Round(in.Amount.D, c.currency)
		open, err := openSales(ctx, tx, id, true)
		if err != nil {
			return err
		}
		owed := decimal.Zero
		for _, s := range open {
			owed = owed.Add(s.Due)
		}
		if !owed.IsPositive() {
			return apperr.Conflict(apperr.CodeInvalidState, c.name+" owes nothing.")
		}
		if amount.GreaterThan(owed) {
			sym := Symbol(c.currency)
			return apperr.Invalid("amount", fmt.Sprintf("%s owes %s%s; take at most that.", c.name, sym, owed.StringFixed(2)))
		}
		var store *int64
		if in.StoreID.Valid && in.StoreID.V > 0 {
			v := in.StoreID.V
			store = &v
		}
		var pid int64
		var at time.Time
		if err := tx.QueryRow(ctx, qInsertCustomerPayment, id, amount, in.Method, in.Reference, in.Note, c.currency, nil, "", store, p.UserID).
			Scan(&pid, &at); err != nil {
			return err
		}
		left := amount
		var allocs []Allocation
		for _, s := range open {
			if !left.IsPositive() {
				break
			}
			take := decimal.Min(left, s.Due)
			if err := allocate(ctx, tx, pid, s.ID, take, in.Method, in.Reference, p.UserID, at); err != nil {
				return err
			}
			allocs = append(allocs, Allocation{SaleID: s.ID, Reference: s.Reference, Amount: take})
			left = left.Sub(take)
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "customer_payment.collect", Entity: "customer_payment",
			EntityID: strconv.FormatInt(pid, 10), After: map[string]any{"customer_id": id, "amount": amount, "method": in.Method,
				"allocations": allocs}, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		l, err := loadCustomerPayments(ctx, tx, "id", pid)
		if err != nil || len(l) == 0 {
			return orInternal(err)
		}
		out = l[0]
		if key != "" {
			return idempotency.Store(ctx, tx, p.UserID, key, http.StatusCreated, out)
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if again != nil {
		idempotency.Write(w, again)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

// allocate pays amount of one sale from a collection (negative: reversal).
func allocate(ctx context.Context, tx tenancy.TenantTx, paymentID, saleID int64, amount decimal.Decimal, method, ref string, userID int64, at time.Time) error {
	if _, err := tx.Exec(ctx, qInsertAllocation, paymentID, saleID, amount); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qInsertCollectPay, saleID, method, amount, ref, paymentID, userID, at); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, qApplyToSale, saleID, amount)
	return err
}

// reverse is POST /api/customer-payments/{id}/reverse {reason}.
func (a *API) reverse(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if l := len([]rune(in.Reason)); l < 5 || l > 500 {
		httpx.WriteError(w, r, apperr.Invalid("reason", "Give a reason of 5 to 500 characters."))
		return
	}
	p := principal(r)
	var out CustomerPaymentJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		l, err := loadCustomerPayments(ctx, tx, "id", id)
		if err != nil {
			return err
		}
		if len(l) == 0 {
			return apperr.NotFound()
		}
		orig := l[0]
		// The customer row serialises collections and reversals of that
		// customer (the append-only tables cannot be locked FOR UPDATE).
		c, err := loadCustomer(ctx, tx, orig.CustomerID, true)
		if err != nil {
			return err
		}
		if orig.ReversalOf != nil {
			return apperr.Conflict(apperr.CodeInvalidState, "A reversal cannot be reversed.")
		}
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customer_payments WHERE reversal_of = $1)`, id).Scan(&done); err != nil {
			return err
		}
		if done {
			return apperr.Conflict(apperr.CodeInvalidState, "This payment was already reversed.")
		}
		var rid int64
		var at time.Time
		if err := tx.QueryRow(ctx, qInsertCustomerPayment, orig.CustomerID, orig.Amount.Neg(), orig.Method, deref(orig.Reference), "",
			c.currency, id, in.Reason, nil, p.UserID).Scan(&rid, &at); err != nil {
			return err
		}
		for _, al := range orig.Allocations {
			if err := allocate(ctx, tx, rid, al.SaleID, al.Amount.Neg(), orig.Method, deref(orig.Reference), p.UserID, at); err != nil {
				return err
			}
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "customer_payment.reverse", Entity: "customer_payment",
			EntityID: strconv.FormatInt(id, 10), Before: map[string]any{"amount": orig.Amount},
			After: map[string]any{"reversal_id": rid, "reason": in.Reason}, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		l, err = loadCustomerPayments(ctx, tx, "id", rid)
		if err != nil || len(l) == 0 {
			return orInternal(err)
		}
		out = l[0]
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

// ------------------------------------------------------------ credit limit

func (a *API) setCreditLimit(w http.ResponseWriter, r *http.Request) {
	id, err := customerParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in struct {
		CreditLimit money.Dec `json:"creditLimit"` // null: no credit
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var limit *decimal.Decimal
	if in.CreditLimit.Valid {
		if in.CreditLimit.D.IsNegative() || in.CreditLimit.D.GreaterThan(decimal.NewFromInt(1_000_000_000)) {
			httpx.WriteError(w, r, apperr.Invalid("creditLimit", "Enter an amount from 0, or leave it empty for no credit."))
			return
		}
		v := in.CreditLimit.D
		limit = &v
	}
	p := principal(r)
	var out DuesJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		c, err := loadCustomer(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if c.walkIn && limit != nil {
			return apperr.Invalid("creditLimit", "The walk-in customer cannot buy on credit.")
		}
		if limit != nil {
			v := money.Round(*limit, c.currency)
			limit = &v
		}
		if _, err := tx.Exec(ctx, `UPDATE customers SET credit_limit = $2, updated_at = now() WHERE id = $1`, id, limit); err != nil {
			return err
		}
		out = DuesJSON{CustomerID: id, Name: c.name, Currency: c.currency, CreditLimit: limit}
		return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "customer.credit_limit", Entity: "customer",
			EntityID: strconv.FormatInt(id, 10), Before: map[string]any{"credit_limit": c.limit},
			After: map[string]any{"credit_limit": limit}, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- statement

// StatementLine is one line of a customer statement. Debit raises what
// the customer owes, credit lowers it.
type StatementLine struct {
	Date        time.Time       `json:"date"`
	Kind        string          `json:"kind"` // sale, payment, collection, reversal, return, refund
	Reference   string          `json:"reference"`
	Description string          `json:"description"`
	Debit       decimal.Decimal `json:"debit"`
	Credit      decimal.Decimal `json:"credit"`
	Balance     decimal.Decimal `json:"balance"`
}

// StatementJSON is GET /api/customers/{id}/statement.
type StatementJSON struct {
	CustomerID int64           `json:"customerId"`
	Name       string          `json:"name"`
	Currency   string          `json:"currency"`
	From       *time.Time      `json:"from"`
	To         *time.Time      `json:"to"` // exclusive
	Opening    decimal.Decimal `json:"opening"`
	Closing    decimal.Decimal `json:"closing"`
	Lines      []StatementLine `json:"lines"`
}

// qStatementEvents lists every money event of a customer (void sales and
// their payments left out). The amount is signed: + owes more.
const qStatementEvents = `SELECT at, kind, reference, description, amount FROM (
	SELECT s.date AS at, 1 AS ord, 'sale' AS kind, s.reference, 'Sale' AS description, s.total AS amount, s.id AS id
		FROM sales s WHERE s.customer_id = $1 AND s.status <> 'Void'
	UNION ALL
	SELECT p.created_at, 2, 'payment', s.reference, initcap(p.method) || coalesce(' ' || p.reference, ''), -p.amount, p.id
		FROM sale_payments p JOIN sales s ON s.id = p.sale_id
		WHERE s.customer_id = $1 AND s.status <> 'Void' AND p.customer_payment_id IS NULL AND p.method <> 'credit'
	UNION ALL
	SELECT c.created_at, 3, CASE WHEN c.reversal_of IS NULL THEN 'collection' ELSE 'reversal' END, 'PAY-' || c.id,
		CASE WHEN c.reversal_of IS NULL THEN 'Payment received (' || c.method || ')' ELSE 'Reversal of PAY-' || c.reversal_of || ': ' || c.reason END,
		-c.amount, c.id
		FROM customer_payments c WHERE c.customer_id = $1
	UNION ALL
	SELECT r.date, 4, 'return', r.reference, 'Return', -r.total, r.id FROM sale_returns r WHERE r.customer_id = $1
	UNION ALL
	SELECT r.date, 5, 'refund', r.reference, 'Refund paid', r.paid, r.id FROM sale_returns r WHERE r.customer_id = $1 AND r.paid > 0
) e ORDER BY at, ord, id`

func (a *API) statement(w http.ResponseWriter, r *http.Request) {
	id, err := customerParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	out := StatementJSON{CustomerID: id, Lines: []StatementLine{}}
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		s, err := loadSettings(ctx, tx)
		if err != nil {
			return err
		}
		c, err := loadCustomer(ctx, tx, id, false)
		if err != nil {
			return err
		}
		out.Name, out.Currency = c.name, c.currency
		if out.From, err = docdate.Bound(r.URL.Query().Get("startDate"), s.loc, false); err != nil {
			return err
		}
		if out.To, err = docdate.Bound(r.URL.Query().Get("endDate"), s.loc, true); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, qStatementEvents, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		bal := decimal.Zero
		for rows.Next() {
			var l StatementLine
			var amt decimal.Decimal
			if err := rows.Scan(&l.Date, &l.Kind, &l.Reference, &l.Description, &amt); err != nil {
				return err
			}
			if out.From != nil && l.Date.Before(*out.From) {
				out.Opening = out.Opening.Add(amt)
				bal = bal.Add(amt)
				continue
			}
			if out.To != nil && !l.Date.Before(*out.To) {
				continue
			}
			bal = bal.Add(amt)
			if amt.IsNegative() {
				l.Credit = amt.Neg()
			} else {
				l.Debit = amt
			}
			l.Balance = bal
			if len(out.Lines) < 5000 {
				out.Lines = append(out.Lines, l)
			}
		}
		out.Closing = bal
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
