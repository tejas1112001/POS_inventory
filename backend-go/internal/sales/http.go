// Package sales implements checkout, sale edit, void, the sales list and
// invoices (PHASE1_DESIGN step 8; API_CONTRACT C-04, C-10, C-11, S2).
package sales

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/outbox"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Deps are the sales dependencies.
type Deps struct {
	Sessions *tenancy.Sessions
	MaxRows  int
	Now      func() time.Time // tests; default time.Now
	// OnReceipt runs after a sale change commits, with the new receipt
	// (the worker renders it ahead of time). Optional.
	OnReceipt func(ctx context.Context, t tenancy.Tenant, receiptID int64)
}

// API holds the handlers.
type API struct{ d Deps }

// New creates the API.
func New(d Deps) *API {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &API{d: d}
}

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

// ownerScope is the user a principal is limited to: 0 with sales:view_all,
// otherwise the principal (C-10).
func ownerScope(p auth.Principal) int64 {
	if p.Has("sales:view_all") {
		return 0
	}
	return p.UserID
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound()
	}
	return id, nil
}

func (a *API) rw(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, fn)
}

func (a *API) ro(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, fn)
}

func queryInt(r *http.Request, name string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get(name)), 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ------------------------------------------------------------------- read

const (
	qSaleFilter = ` FROM sales s WHERE ($1 = 0 OR s.user_id = $1) AND ($2 OR s.status <> 'Void')
		AND ($3 = '' OR s.reference ILIKE $3)
		AND ($4::timestamptz IS NULL OR s.date >= $4) AND ($5::timestamptz IS NULL OR s.date < $5)
		AND ($6 = 0 OR s.store_id = $6) AND ($7 = 0 OR s.customer_id = $7)`
	qSaleCount = `SELECT count(*)` + qSaleFilter
	qSaleIDs   = `SELECT s.id` + qSaleFilter + ` ORDER BY s.date DESC, s.id DESC LIMIT $8 OFFSET $9`
)

// bound parses a startDate/endDate filter. A date is a whole day in the
// tenant's time zone; endDate is inclusive.
func bound(v string, loc *time.Location, end bool) (*time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	if d, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		if end {
			d = d.AddDate(0, 0, 1)
		}
		return &d, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		if end {
			t = t.Add(time.Microsecond)
		}
		return &t, nil
	}
	return nil, apperr.Invalid("date", "Dates must be YYYY-MM-DD or RFC 3339 timestamps.")
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	q := r.URL.Query()
	like := ""
	if s := strings.TrimSpace(q.Get("search")); s != "" {
		like = "%" + escapeLike(s) + "%"
	}
	voided := q.Get("includeVoided") == "true"
	scope := ownerScope(principal(r))
	var (
		out   []SaleJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		s, err := loadSettings(r.Context(), tx)
		if err != nil {
			return err
		}
		from, err := bound(q.Get("startDate"), s.loc, false)
		if err != nil {
			return err
		}
		to, err := bound(q.Get("endDate"), s.loc, true)
		if err != nil {
			return err
		}
		args := []any{scope, voided, like, from, to, queryInt(r, "storeId"), queryInt(r, "customerId")}
		if err := tx.QueryRow(r.Context(), qSaleCount, args...).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qSaleIDs, append(args, p.Limit, p.Offset)...)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		out, err = load(r.Context(), tx, ids)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	scope := ownerScope(principal(r))
	var (
		out   SaleJSON
		found bool
	)
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, found, err = loadOne(r.Context(), tx, id)
		return err
	})
	if err == nil && (!found || (scope != 0 && out.UserID != scope)) {
		err = apperr.NotFound()
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// InvoiceJSON is the old /api/invoices row.
type InvoiceJSON struct {
	ID        int64           `json:"id"`
	InvoiceNo string          `json:"invoiceNo"`
	Customer  invoiceCustomer `json:"customer"`
	DueDate   string          `json:"dueDate"`
	Amount    decimal.Decimal `json:"amount"`
	Paid      decimal.Decimal `json:"paid"`
	AmountDue decimal.Decimal `json:"amountDue"`
	Status    string          `json:"status"`
}

type invoiceCustomer struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

const (
	qInvoiceFilter = ` FROM sales s WHERE s.status <> 'Void' AND ($1 = 0 OR s.user_id = $1)`
	qInvoices      = `SELECT s.id, s.reference, s.date, s.total, s.paid, s.due, s.payment_status, c.name, c.image_file_id
		FROM sales s LEFT JOIN customers c ON c.id = s.customer_id
		WHERE s.status <> 'Void' AND ($1 = 0 OR s.user_id = $1)
		ORDER BY s.date DESC, s.id DESC LIMIT $2 OFFSET $3`
)

// invoices lists non-void sales as invoices, own-only without
// sales:view_all (C-10).
func (a *API) invoices(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	scope := ownerScope(principal(r))
	var (
		out   []InvoiceJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		s, err := loadSettings(r.Context(), tx)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `SELECT count(*)`+qInvoiceFilter, scope).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qInvoices, scope, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				inv  InvoiceJSON
				date time.Time
				name *string
				img  *uuid.UUID
			)
			if err := rows.Scan(&inv.ID, &inv.InvoiceNo, &date, &inv.Amount, &inv.Paid, &inv.AmountDue, &inv.Status, &name, &img); err != nil {
				return err
			}
			inv.DueDate = date.In(s.loc).Format("02 Jan 2006")
			inv.Customer = invoiceCustomer{Name: "N/A"}
			if name != nil {
				inv.Customer.Name = *name
				if img != nil {
					inv.Customer.Avatar = files.URL(*img, files.PurposeCustomerImage)
				}
			}
			out = append(out, inv)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

// ----------------------------------------------------------------- create

// hashingBody hashes the request body as the JSON decoder reads it.
type hashingBody struct {
	io.Reader
	io.Closer
}

func idempotencyKey(r *http.Request) (string, error) {
	k := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if k == "" {
		return "", nil
	}
	if len(k) > 100 {
		return "", apperr.Invalid("Idempotency-Key", "Idempotency-Key must be at most 100 characters.")
	}
	for _, c := range k {
		if c < 0x21 || c > 0x7e {
			return "", apperr.Invalid("Idempotency-Key", "Idempotency-Key must be printable ASCII.")
		}
	}
	return k, nil
}

type replay struct {
	status int
	body   []byte
}

const (
	qExpireKey = `DELETE FROM idempotency_keys WHERE user_id = $1 AND key = $2 AND created_at < now() - interval '24 hours'`
	qClaimKey  = `INSERT INTO idempotency_keys (user_id, key, request_hash) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`
	qReadKey   = `SELECT request_hash, status, response FROM idempotency_keys WHERE user_id = $1 AND key = $2`
	qStoreKey  = `UPDATE idempotency_keys SET status = $3, response = $4 WHERE user_id = $1 AND key = $2`
)

// claimKey records the key in the sale's transaction. A concurrent request
// with the same key waits on the primary key until this transaction ends;
// a completed one is replayed. Failed requests roll back, so only
// successful responses are stored (S2).
func claimKey(ctx context.Context, tx tenancy.TenantTx, userID int64, key string, sum []byte) (*replay, error) {
	if _, err := tx.Exec(ctx, qExpireKey, userID, key); err != nil {
		return nil, err
	}
	tag, err := tx.Exec(ctx, qClaimKey, userID, key, sum)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 1 {
		return nil, nil
	}
	var (
		h      []byte
		status *int
		body   []byte
	)
	if err := tx.QueryRow(ctx, qReadKey, userID, key).Scan(&h, &status, &body); err != nil {
		return nil, err
	}
	if !bytes.Equal(h, sum) {
		return nil, apperr.Conflict(apperr.CodeConflict, "This Idempotency-Key was already used for a different request.")
	}
	if status == nil {
		return nil, apperr.Conflict(apperr.CodeConflict, "A request with this Idempotency-Key is still in progress.")
	}
	return &replay{status: *status, body: body}, nil
}

const qInsertSale = `INSERT INTO sales (reference, date, status, payment_status, payment_method, payment_note, note,
		subtotal, discount, tax, shipping, total, paid, due, tendered, change_due, currency,
		customer_id, store_id, user_id, receipt_email)
	VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), $8, $9, $10, $11, $12, $13, $14, $15, $16, $17,
		$18, $19, $20, NULLIF($21, ''))
	RETURNING id`

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	key, err := idempotencyKey(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	h := sha256.New()
	r.Body = hashingBody{io.TeeReader(r.Body, h), r.Body}
	var in saleIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := h.Sum(nil)
	c, err := validate(in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	now := a.d.Now()
	var (
		out       SaleJSON
		stored    []byte
		again     *replay
		receiptID int64
	)
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if key != "" {
			var err error
			if again, err = claimKey(ctx, tx, p.UserID, key, sum); err != nil || again != nil {
				return err
			}
		}
		s, err := loadSettings(ctx, tx)
		if err != nil {
			return err
		}
		date, err := parseDate(c.date, s.loc, now)
		if err != nil {
			return err
		}
		cust, err := checkParties(ctx, tx, c, s)
		if err != nil {
			return err
		}
		byID, err := resolve(ctx, tx, c.lines)
		if err != nil {
			return err
		}
		pr, err := price(c, byID, s, p, nil)
		if err != nil {
			return err
		}
		deltas := map[stockKey]int{}
		for _, l := range pr.lines {
			deltas[stockKey{c.storeID, l.v.id}] -= int(l.qty)
		}
		// Lock stock first, then the counter: the counter row serialises
		// checkouts, so it is held for the shortest time.
		keys, err := lockStock(ctx, tx, deltas, skuMap(pr))
		if err != nil {
			return err
		}
		ref, err := nextReference(ctx, tx, s.prefix)
		if err != nil {
			return err
		}
		status := c.status
		if status == "" {
			status = "Completed"
		}
		var custName string
		var custEmail *string
		if err := tx.QueryRow(ctx, `SELECT name, email::text FROM customers WHERE id = $1`, cust).Scan(&custName, &custEmail); err != nil {
			return err
		}
		// Receipt email: the address the cashier typed, else the customer's.
		receiptTo := deref(c.receiptEmail)
		if receiptTo == "" && custEmail != nil && mailer.ValidEmail(*custEmail) {
			receiptTo = *custEmail
		}
		pm := pay(pr.quote.Total, c.paid, c.tendered, s.currency)
		var id int64
		if err := tx.QueryRow(ctx, qInsertSale, ref, date, status, pm.status, deref(c.paymentMethod), deref(c.paymentNote),
			deref(c.note), pr.quote.Subtotal, pr.quote.Discount, pr.quote.Tax, pr.quote.Shipping, pr.quote.Total,
			pm.paid, pm.due, pm.tendered, pm.chg, s.currency, cust, c.storeID, p.UserID, receiptTo).Scan(&id); err != nil {
			return err
		}
		if err := insertItems(ctx, tx, id, pr); err != nil {
			return err
		}
		if err := moveStock(ctx, tx, keys, deltas, inventory.ReasonSale, inventory.Ref{Type: "sale", ID: id}, p.UserID); err != nil {
			return err
		}
		if receiptID, err = receipts.Create(ctx, tx, id, 1); err != nil {
			return err
		}
		if receiptTo != "" {
			if _, err := outbox.Enqueue(ctx, tx, outbox.Item{Key: fmt.Sprintf("sale:%d:receipt", id), Kind: outbox.KindReceipt,
				To: receiptTo, ToName: custName, Template: "receipt", ReceiptID: receiptID,
				Data: map[string]any{"sale_id": id, "receipt_id": receiptID}}); err != nil {
				return err
			}
		}
		if len(pr.overrides) > 0 {
			if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "sale.price_override", Entity: "sale",
				EntityID: strconv.FormatInt(id, 10), After: map[string]any{"reference": ref, "lines": pr.overrides},
				RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
				return err
			}
		}
		var found bool
		if out, found, err = loadOne(ctx, tx, id); err != nil || !found {
			return orInternal(err)
		}
		if key != "" {
			if stored, err = json.Marshal(out); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, qStoreKey, p.UserID, key, http.StatusCreated, string(stored)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if again != nil {
		w.Header().Set("Idempotent-Replayed", "true")
		httpx.WriteJSON(w, again.status, json.RawMessage(again.body))
		return
	}
	a.afterCommit(r, receiptID)
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) afterCommit(r *http.Request, receiptID int64) {
	if a.d.OnReceipt != nil && receiptID > 0 {
		a.d.OnReceipt(context.WithoutCancel(r.Context()), tenant(r), receiptID)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orInternal(err error) error {
	if err != nil {
		return err
	}
	return apperr.Internal(nil)
}

// ------------------------------------------------------------------- edit

type saleRow struct {
	userID, storeID     int64
	status              string
	paid                decimal.Decimal
	tendered            *decimal.Decimal
	date                time.Time
	method, pnote, note *string
	email               *string
	total               decimal.Decimal
}

const qLockSale = `SELECT user_id, store_id, status, paid, tendered, date, payment_method, payment_note, note,
		receipt_email::text, total FROM sales WHERE id = $1 FOR UPDATE`

// lockSale locks a sale the principal may change (own-only without
// sales:view_all; others' sales are reported as not found).
func lockSale(ctx context.Context, tx tenancy.TenantTx, id int64, p auth.Principal) (saleRow, error) {
	var s saleRow
	err := tx.QueryRow(ctx, qLockSale, id).Scan(&s.userID, &s.storeID, &s.status, &s.paid, &s.tendered, &s.date,
		&s.method, &s.pnote, &s.note, &s.email, &s.total)
	if isNoRows(err) {
		return s, apperr.NotFound()
	}
	if err != nil {
		return s, err
	}
	if scope := ownerScope(p); scope != 0 && s.userID != scope {
		return s, apperr.NotFound()
	}
	return s, nil
}

type oldItem struct {
	variantID     *int64
	qty           int
	unit, catalog decimal.Decimal
}

func oldItems(ctx context.Context, tx tenancy.TenantTx, saleID int64) ([]oldItem, error) {
	rows, err := tx.Query(ctx, `SELECT variant_id, quantity, unit_price, catalog_price FROM order_items WHERE sale_id = $1 ORDER BY id`, saleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []oldItem
	for rows.Next() {
		var o oldItem
		if err := rows.Scan(&o.variantID, &o.qty, &o.unit, &o.catalog); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func keep(v, old *string) string {
	if v != nil {
		return *v
	}
	return deref(old)
}

const qUpdateSale = `UPDATE sales SET date = $2, status = $3, payment_status = $4, payment_method = NULLIF($5, ''),
		payment_note = NULLIF($6, ''), note = NULLIF($7, ''), subtotal = $8, discount = $9, tax = $10, shipping = $11,
		total = $12, paid = $13, due = $14, tendered = $15, change_due = $16, customer_id = $17, store_id = $18,
		receipt_email = NULLIF($19, ''), updated_by = $20, version = version + 1, updated_at = now()
	WHERE id = $1 RETURNING version`

// update replaces a sale's lines and recomputes it. The original biller
// stays on the sale and the editor is recorded in updated_by (Q-10).
func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in saleIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := validate(in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	now := a.d.Now()
	var (
		out       SaleJSON
		receiptID int64
	)
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		old, err := lockSale(ctx, tx, id, p)
		if err != nil {
			return err
		}
		if old.status == "Void" {
			return apperr.Conflict(apperr.CodeInvalidState, "A voided sale cannot be edited.")
		}
		s, err := loadSettings(ctx, tx)
		if err != nil {
			return err
		}
		date := old.date
		if c.date != "" {
			if date, err = parseDate(c.date, s.loc, now); err != nil {
				return err
			}
		}
		cust, err := checkParties(ctx, tx, c, s)
		if err != nil {
			return err
		}
		items, err := oldItems(ctx, tx, id)
		if err != nil {
			return err
		}
		prior := map[priorPrice]decimal.Decimal{}
		deltas := map[stockKey]int{}
		for _, o := range items {
			if o.variantID != nil {
				prior[priorPrice{*o.variantID, o.unit.String()}] = o.catalog
				deltas[stockKey{old.storeID, *o.variantID}] += o.qty
			}
		}
		byID, err := resolve(ctx, tx, c.lines)
		if err != nil {
			return err
		}
		pr, err := price(c, byID, s, p, prior)
		if err != nil {
			return err
		}
		for _, l := range pr.lines {
			deltas[stockKey{c.storeID, l.v.id}] -= int(l.qty)
		}
		if err := applyStock(ctx, tx, deltas, skuMap(pr), inventory.ReasonSaleEdit, inventory.Ref{Type: "sale", ID: id}, p.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM order_items WHERE sale_id = $1`, id); err != nil {
			return err
		}
		if err := insertItems(ctx, tx, id, pr); err != nil {
			return err
		}
		status := c.status
		if status == "" {
			status = old.status
		}
		paid := c.paid
		if paid == nil {
			v := decimal.Min(old.paid, pr.quote.Total)
			paid = &v
		}
		tendered := c.tendered
		if tendered == nil {
			tendered = old.tendered
		}
		pm := pay(pr.quote.Total, paid, tendered, s.currency)
		var version int
		if err := tx.QueryRow(ctx, qUpdateSale, id, date, status, pm.status, keep(c.paymentMethod, old.method),
			keep(c.paymentNote, old.pnote), keep(c.note, old.note), pr.quote.Subtotal, pr.quote.Discount, pr.quote.Tax,
			pr.quote.Shipping, pr.quote.Total, pm.paid, pm.due, pm.tendered, pm.chg, cust, c.storeID,
			keep(c.receiptEmail, old.email), p.UserID).Scan(&version); err != nil {
			return err
		}
		if receiptID, err = receipts.Create(ctx, tx, id, version); err != nil {
			return err
		}
		meta := audit.Entry{ActorID: p.UserID, Entity: "sale", EntityID: strconv.FormatInt(id, 10),
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}
		e := meta
		e.Action = "sale.update"
		e.Before = map[string]any{"total": old.total, "paid": old.paid, "store_id": old.storeID, "lines": len(items)}
		e.After = map[string]any{"total": pr.quote.Total, "paid": pm.paid, "store_id": c.storeID, "lines": len(pr.lines)}
		if err := audit.Write(ctx, tx, e); err != nil {
			return err
		}
		if len(pr.overrides) > 0 {
			e := meta
			e.Action, e.After = "sale.price_override", map[string]any{"lines": pr.overrides}
			if err := audit.Write(ctx, tx, e); err != nil {
				return err
			}
		}
		var found bool
		out, found, err = loadOne(ctx, tx, id)
		if err != nil || !found {
			return orInternal(err)
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a.afterCommit(r, receiptID)
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ------------------------------------------------------------------- void

// void is DELETE /api/sales/{id}: the sale is kept with status Void and its
// stock goes back (C-11). Voiding twice is a no-op.
func (a *API) void(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	if len([]rune(reason)) > 500 {
		httpx.WriteError(w, r, apperr.Invalid("reason", "Reason must be at most 500 characters."))
		return
	}
	p := principal(r)
	var receiptID int64
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		old, err := lockSale(ctx, tx, id, p)
		if err != nil || old.status == "Void" {
			return err
		}
		items, err := oldItems(ctx, tx, id)
		if err != nil {
			return err
		}
		deltas := map[stockKey]int{}
		for _, o := range items {
			if o.variantID != nil {
				deltas[stockKey{old.storeID, *o.variantID}] += o.qty
			}
		}
		if err := applyStock(ctx, tx, deltas, nil, inventory.ReasonSaleVoid, inventory.Ref{Type: "sale", ID: id}, p.UserID); err != nil {
			return err
		}
		var version int
		if err := tx.QueryRow(ctx, `UPDATE sales SET status = 'Void', voided_at = now(), voided_by = $2, void_reason = NULLIF($3, ''),
			updated_by = $2, version = version + 1, updated_at = now() WHERE id = $1 RETURNING version`, id, p.UserID, reason).Scan(&version); err != nil {
			return err
		}
		// The void gets its own receipt version, rendered with a VOID mark.
		if receiptID, err = receipts.Create(ctx, tx, id, version); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "sale.void", Entity: "sale",
			EntityID: strconv.FormatInt(id, 10), Before: map[string]any{"total": old.total, "status": old.status},
			After: map[string]any{"reason": reason}, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a.afterCommit(r, receiptID)
	w.WriteHeader(http.StatusNoContent)
}

// ------------------------------------------------------------------ mount

// Mount registers the sales and invoice routes.
func (a *API) Mount(rt *rbac.Router) {
	req := rbac.Require
	rt.Handle(http.MethodGet, "/sales", req("sales", "view"), a.list)
	rt.Handle(http.MethodGet, "/sales/{id}", req("sales", "view"), a.get)
	rt.Handle(http.MethodPost, "/sales", req("sales", "create"), a.create)
	rt.Handle(http.MethodPut, "/sales/{id}", req("sales", "edit"), a.update)
	rt.Handle(http.MethodDelete, "/sales/{id}", req("sales", "delete"), a.void)
	rt.Handle(http.MethodGet, "/invoices", req("invoices", "view"), a.invoices)
}
