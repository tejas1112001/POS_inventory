package sales

// Store credit (industry P1 item 9, A21, A22; DB21). An append-only ledger
// per customer: + from a return given as store credit, - when a sale is
// paid with it, ± an owner's adjustment with a reason. The balance is the
// sum; it never goes below 0, checked under the customer's row lock. Walk-in
// customers get none. No expiry (Q5).

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/idempotency"
	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// MethodExchange and MethodStoreCredit are the payment methods of item 9.
const (
	MethodExchange    = "exchange"
	MethodStoreCredit = "store_credit"
)

// CodeStoreCreditLow is refused spending or adjusting: the customer's store credit does not cover it.
const CodeStoreCreditLow = "STORE_CREDIT_LOW"

const qLedgerBalance = `SELECT coalesce(sum(amount), 0) FROM customer_credit_entries WHERE customer_id = $1`

const qLedgerInsert = `INSERT INTO customer_credit_entries (customer_id, amount, kind, sale_return_id, sale_id, reason, currency, user_id)
	VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)`

// lockCredit locks the customer row and returns the store credit balance.
// A walk-in (the default customer) or an unknown customer is refused.
func lockCredit(ctx context.Context, tx tenancy.TenantTx, s settings, customerID int64, field string) (decimal.Decimal, string, error) {
	if s.defaultCustomer != nil && *s.defaultCustomer == customerID {
		return decimal.Zero, "", apperr.Invalid(field, "Store credit needs a named customer (not Walk-in).")
	}
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM customers WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, customerID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return decimal.Zero, "", apperr.Invalid(field, "Customer not found.")
	}
	if err != nil {
		return decimal.Zero, "", err
	}
	var bal decimal.Decimal
	err = tx.QueryRow(ctx, qLedgerBalance, customerID).Scan(&bal)
	return bal, name, err
}

// creditFromReturn puts a return's value on the customer's store credit.
func creditFromReturn(ctx context.Context, tx tenancy.TenantTx, s settings, customerID, returnID, userID int64, amount decimal.Decimal) error {
	if !amount.IsPositive() {
		return nil
	}
	if _, _, err := lockCredit(ctx, tx, s, customerID, "settlement"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, qLedgerInsert, customerID, amount, "return", returnID, nil, "", s.currency, userID)
	return err
}

// spendCredit takes a sale's store credit payments off the balance.
func spendCredit(ctx context.Context, tx tenancy.TenantTx, s settings, customerID, saleID, userID int64, amount decimal.Decimal) error {
	if !amount.IsPositive() {
		return nil
	}
	bal, name, err := lockCredit(ctx, tx, s, customerID, "payments")
	if err != nil {
		return err
	}
	if bal.LessThan(amount) {
		sym := Symbol(s.currency)
		return apperr.Conflict(CodeStoreCreditLow, name+" has "+sym+money.Round(bal, s.currency).StringFixed(2)+
			" of store credit, less than the "+sym+amount.StringFixed(2)+" to pay with it.").WithDetails(map[string]any{"balance": bal})
	}
	_, err = tx.Exec(ctx, qLedgerInsert, customerID, amount.Neg(), "sale", nil, saleID, "", s.currency, userID)
	return err
}

// creditPayments is the store credit part of a sale's payments.
func creditPayments(rows []payLine) decimal.Decimal {
	sum := decimal.Zero
	for _, r := range rows {
		if r.method == MethodStoreCredit {
			sum = sum.Add(r.amount)
		}
	}
	return sum
}

// refuseIfExchanged stops an edit or void of a sale paid by an exchange or
// with store credit (design §9: reverse the exchange first).
func refuseIfExchanged(ctx context.Context, tx tenancy.TenantTx, saleID int64, action string) error {
	var paid bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sale_payments WHERE sale_id = $1 AND method IN ('exchange', 'store_credit'))`,
		saleID).Scan(&paid); err != nil {
		return err
	}
	if paid {
		return apperr.Conflict(apperr.CodeInvalidState, "This sale was paid by an exchange or with store credit, so it cannot be "+action+".")
	}
	return nil
}

// ------------------------------------------------------------------- A21

// CreditEntryJSON is one store credit entry.
type CreditEntryJSON struct {
	ID           int64           `json:"id"`
	Amount       decimal.Decimal `json:"amount"`
	Kind         string          `json:"kind"`
	SaleReturnID *int64          `json:"saleReturnId"`
	SaleID       *int64          `json:"saleId"`
	Reference    *string         `json:"reference"` // the return's or the sale's number
	Reason       *string         `json:"reason"`
	UserName     *string         `json:"userName"`
	CreatedAt    time.Time       `json:"createdAt"`
}

// StoreCreditJSON is GET /api/customers/{id}/store-credit.
type StoreCreditJSON struct {
	CustomerID int64             `json:"customerId"`
	Name       string            `json:"name"`
	WalkIn     bool              `json:"walkIn"`
	Balance    decimal.Decimal   `json:"balance"`
	CanAdjust  bool              `json:"canAdjust"`
	Entries    []CreditEntryJSON `json:"entries"`
}

const qLedgerEntries = `SELECT e.id, e.amount, e.kind, e.sale_return_id, e.sale_id, coalesce(r.reference, s.reference), e.reason,
		u.name, e.created_at
	FROM customer_credit_entries e
	LEFT JOIN sale_returns r ON r.id = e.sale_return_id
	LEFT JOIN sales s ON s.id = e.sale_id
	LEFT JOIN users u ON u.id = e.user_id
	WHERE e.customer_id = $1 ORDER BY e.created_at DESC, e.id DESC LIMIT 500`

func loadCredit(ctx context.Context, tx tenancy.TenantTx, p auth.Principal, id int64) (StoreCreditJSON, error) {
	out := StoreCreditJSON{CustomerID: id, Entries: []CreditEntryJSON{}, CanAdjust: p.Has("customers:store_credit")}
	var def *int64
	err := tx.QueryRow(ctx, `SELECT c.name, st.default_customer_id FROM customers c, settings st WHERE c.id = $1 AND st.id = 1`, id).
		Scan(&out.Name, &def)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, apperr.NotFound()
	}
	if err != nil {
		return out, err
	}
	out.WalkIn = def != nil && *def == id
	out.CanAdjust = out.CanAdjust && !out.WalkIn
	if err := tx.QueryRow(ctx, qLedgerBalance, id).Scan(&out.Balance); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, qLedgerEntries, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var e CreditEntryJSON
		if err := rows.Scan(&e.ID, &e.Amount, &e.Kind, &e.SaleReturnID, &e.SaleID, &e.Reference, &e.Reason, &e.UserName, &e.CreatedAt); err != nil {
			return out, err
		}
		out.Entries = append(out.Entries, e)
	}
	return out, rows.Err()
}

// storeCredit is GET /api/customers/{id}/store-credit.
func (a *API) storeCredit(w http.ResponseWriter, r *http.Request) {
	id, err := customerParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out StoreCreditJSON
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = loadCredit(r.Context(), tx, principal(r), id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type creditAdjustIn struct {
	Amount money.Dec `json:"amount"`
	Reason *string   `json:"reason"`
}

// adjustCredit is POST /api/customers/{id}/store-credit/adjust {amount,
// reason} (customers:store_credit, owner only): amount is signed; the
// balance may not go below 0. An Idempotency-Key replays the answer.
func (a *API) adjustCredit(w http.ResponseWriter, r *http.Request) {
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
	hash := idempotency.Hash(r, "POST /api/customers/"+strconv.FormatInt(id, 10)+"/store-credit/adjust\n")
	var in creditAdjustIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := hash()
	f := fieldErrs{}
	reason := ""
	if in.Reason != nil {
		reason = strings.TrimSpace(*in.Reason)
	}
	switch n := len([]rune(reason)); {
	case n < 5:
		f["reason"] = "Write a reason (at least 5 characters)."
	case n > maxReasonLen:
		f["reason"] = "At most 500 characters."
	}
	if !in.Amount.Valid || in.Amount.D.IsZero() || in.Amount.D.Abs().GreaterThan(decimal.NewFromInt(100_000_000)) {
		f["amount"] = "Enter an amount other than 0."
	}
	if err := f.err(); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	var (
		out   StoreCreditJSON
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
		s, err := loadSettings(ctx, tx)
		if err != nil {
			return err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customers WHERE id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return apperr.NotFound()
		}
		bal, name, err := lockCredit(ctx, tx, s, id, "customerId")
		if err != nil {
			return err
		}
		amount := money.Round(in.Amount.D, s.currency)
		if amount.IsZero() {
			return apperr.Invalid("amount", "Enter an amount other than 0.")
		}
		if bal.Add(amount).IsNegative() {
			sym := Symbol(s.currency)
			return apperr.Conflict(CodeStoreCreditLow, name+" has "+sym+money.Round(bal, s.currency).StringFixed(2)+
				" of store credit; the balance cannot go below 0.").WithDetails(map[string]any{"balance": bal})
		}
		if _, err := tx.Exec(ctx, qLedgerInsert, id, amount, "adjust", nil, nil, reason, s.currency, p.UserID); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "customer.store_credit_adjust", Entity: "customer",
			EntityID: strconv.FormatInt(id, 10), Before: map[string]any{"balance": bal},
			After:     map[string]any{"amount": amount, "balance": bal.Add(amount), "reason": reason},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		if out, err = loadCredit(ctx, tx, p, id); err != nil {
			return err
		}
		if key != "" {
			return idempotency.Store(ctx, tx, p.UserID, key, http.StatusOK, out)
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
	httpx.WriteJSON(w, http.StatusOK, out)
}
