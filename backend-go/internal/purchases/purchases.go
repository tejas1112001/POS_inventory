// Package purchases records supplier purchases and their stock-in
// (PHASE1_DESIGN step 11; API_CONTRACT C-09, Q-16; PRD §3.10). Every line
// names the variant and store it stocks. Stock goes in once, when the
// purchase becomes Received, which needs purchases:approve. Received is
// terminal: stock corrections go through stock adjustments. Totals are
// computed here from the lines.
package purchases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/docdate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/idempotency"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

const (
	maxLines   = 500
	maxQty     = 1_000_000
	maxNoteLen = 1000
	maxRefLen  = 60
	hashScope  = "POST /api/purchases\n"

	statusReceived  = "Received"
	statusCancelled = "Cancelled"
)

var statuses = map[string]bool{"Ordered": true, "Pending": true, statusReceived: true, statusCancelled: true}

// Deps are the purchases dependencies.
type Deps struct {
	Sessions *tenancy.Sessions
	MaxRows  int
	Now      func() time.Time // tests; default time.Now
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

// Mount registers the purchase routes.
func (a *API) Mount(rt *rbac.Router) {
	req := rbac.Require
	rt.Handle(http.MethodGet, "/purchases", req("purchases", "view"), a.list)
	rt.Handle(http.MethodGet, "/purchases/{id}", req("purchases", "view"), a.get)
	rt.Handle(http.MethodPost, "/purchases", req("purchases", "create"), a.create)
	rt.Handle(http.MethodPut, "/purchases/{id}", req("purchases", "edit"), a.update)
}

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
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

// ----------------------------------------------------------------- input

// itemIn is one AddPurchase.jsx line.
type itemIn struct {
	ProductID money.Int `json:"productId"`
	VariantID money.Int `json:"variantId"`
	StoreID   money.Int `json:"storeId"`
	Quantity  money.Int `json:"quantity"`
	UnitPrice money.Dec `json:"unitPrice"`
	Discount  money.Dec `json:"discount"`

	// Accepted and ignored: line amounts are computed here.
	Subtotal json.RawMessage `json:"subtotal"`
	Total    json.RawMessage `json:"total"`
}

// purchaseIn is the POST body. Decoding is strict; every key the form
// sends is used or on the ignore list.
type purchaseIn struct {
	PurchaseItems []itemIn  `json:"purchaseItems"`
	SupplierID    money.Int `json:"supplierId"`
	Reference     *string   `json:"reference"`
	Date          *string   `json:"date"`
	Status        *string   `json:"status"`
	PaymentStatus *string   `json:"payment_status"`
	Paid          money.Dec `json:"paid"`
	Note          *string   `json:"note"`

	// Accepted and ignored: the total and due amount are computed here.
	Total json.RawMessage `json:"total"`
	Due   json.RawMessage `json:"due"`
}

// updateIn is the PUT body: status, payment and note only (PRD §3.10).
type updateIn struct {
	Status        *string   `json:"status"`
	PaymentStatus *string   `json:"payment_status"`
	Paid          money.Dec `json:"paid"`
	Note          *string   `json:"note"`

	// Accepted and ignored: due is computed and each line names its own
	// store (C-09).
	Due     json.RawMessage `json:"due"`
	StoreID json.RawMessage `json:"storeId"`
}

type line struct {
	productID, variantID, storeID, qty int64
	price, discount                    decimal.Decimal
}

type clean struct {
	lines              []line
	supplierID         int64
	reference, date    string
	status, note       string
	overdue, overdueOK bool // overdueOK: payment_status was sent
	paid               decimal.Decimal
}

// paymentChoice reads payment_status: Overdue is the only value kept
// (Paid, Partial and Unpaid follow from paid and the total).
func paymentChoice(v *string, f map[string]string) (overdue, set bool) {
	if v == nil {
		return false, false
	}
	switch s := strings.TrimSpace(*v); s {
	case "":
		return false, false
	case "Overdue":
		return true, true
	case "Paid", "Unpaid", "Partial":
		return false, true
	default:
		f["payment_status"] = "Payment status must be Paid, Unpaid, Partial or Overdue."
		return false, false
	}
}

func statusChoice(v *string, f map[string]string) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(*v)
	if s != "" && !statuses[s] {
		f["status"] = "Status must be Ordered, Pending, Received or Cancelled."
	}
	return s
}

func noteChoice(v *string, f map[string]string) (string, bool) {
	if v == nil {
		return "", false
	}
	n := strings.TrimSpace(*v)
	if len([]rune(n)) > maxNoteLen {
		f["note"] = fmt.Sprintf("At most %d characters.", maxNoteLen)
	}
	return n, true
}

func validate(in purchaseIn) (clean, error) {
	f := map[string]string{}
	c := clean{status: "Pending"}
	if !in.SupplierID.Valid || in.SupplierID.V <= 0 {
		f["supplierId"] = "supplierId is required."
	}
	c.supplierID = in.SupplierID.V
	if in.Reference != nil {
		c.reference = strings.TrimSpace(*in.Reference)
		switch {
		case len([]rune(c.reference)) > maxRefLen:
			f["reference"] = fmt.Sprintf("At most %d characters.", maxRefLen)
		case strings.IndexFunc(c.reference, unicode.IsControl) >= 0:
			f["reference"] = "Reference cannot contain control characters."
		}
	}
	if s := statusChoice(in.Status, f); s != "" {
		c.status = s
	}
	c.overdue, c.overdueOK = paymentChoice(in.PaymentStatus, f)
	if in.Paid.Valid {
		if in.Paid.D.IsNegative() {
			f["paid"] = "Must not be negative."
		}
		c.paid = in.Paid.D
	}
	if in.Date != nil {
		c.date = strings.TrimSpace(*in.Date)
	}
	c.note, _ = noteChoice(in.Note, f)
	switch {
	case len(in.PurchaseItems) == 0:
		f["purchaseItems"] = "At least one purchase item is required."
	case len(in.PurchaseItems) > maxLines:
		f["purchaseItems"] = fmt.Sprintf("A purchase can have at most %d lines.", maxLines)
	}
	for i, it := range in.PurchaseItems {
		k := fmt.Sprintf("purchaseItems[%d]", i)
		l := line{productID: it.ProductID.V, variantID: it.VariantID.V, storeID: it.StoreID.V, qty: it.Quantity.V}
		if !it.Quantity.Valid || it.Quantity.V <= 0 || it.Quantity.V > maxQty {
			f[k+".quantity"] = "Quantity must be a whole number above 0."
		}
		if (!it.VariantID.Valid || it.VariantID.V <= 0) && (!it.ProductID.Valid || it.ProductID.V <= 0) {
			f[k+".variantId"] = "variantId is required."
		}
		if !it.StoreID.Valid || it.StoreID.V <= 0 {
			f[k+".storeId"] = "storeId is required."
		}
		switch {
		case !it.UnitPrice.Valid:
			f[k+".unitPrice"] = "unitPrice is required."
		case it.UnitPrice.D.IsNegative():
			f[k+".unitPrice"] = "Unit price cannot be negative."
		}
		l.price = it.UnitPrice.D.Round(4)
		if it.Discount.Valid {
			if it.Discount.D.IsNegative() {
				f[k+".discount"] = "Discount cannot be negative."
			}
			l.discount = it.Discount.D
		}
		c.lines = append(c.lines, l)
	}
	return c, apperr.Fields(f)
}

// --------------------------------------------------------------- resolve

type variantRow struct {
	productID int64
	sku, name string
}

const (
	qSingleVariant = `SELECT p.id, min(v.id), count(v.id) FROM products p
		LEFT JOIN product_variants v ON v.product_id = p.id AND v.deleted_at IS NULL
		WHERE p.id = ANY($1) AND p.deleted_at IS NULL GROUP BY p.id`
	qVariants = `SELECT v.id, v.product_id, v.sku::text, p.name FROM product_variants v JOIN products p ON p.id = v.product_id
		WHERE v.id = ANY($1) AND v.deleted_at IS NULL AND p.deleted_at IS NULL`
	qStores = `SELECT id FROM stores WHERE id = ANY($1) AND deleted_at IS NULL AND status = 'Active'`
)

// resolve fills each line's variant (a line with only productId must name
// a single-variant product) and checks the variants, stores and supplier.
func resolve(ctx context.Context, tx tenancy.TenantTx, c *clean) (map[int64]variantRow, error) {
	f := map[string]string{}
	var supplierOK bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM suppliers WHERE id = $1 AND deleted_at IS NULL)`, c.supplierID).Scan(&supplierOK); err != nil {
		return nil, err
	}
	if !supplierOK {
		f["supplierId"] = "Supplier not found."
	}
	var productOnly, storeIDs []int64
	for _, l := range c.lines {
		if l.variantID <= 0 {
			productOnly = append(productOnly, l.productID)
		}
		storeIDs = append(storeIDs, l.storeID)
	}
	if len(productOnly) > 0 {
		only := map[int64]int64{}
		rows, err := tx.Query(ctx, qSingleVariant, productOnly)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var pid int64
			var vid *int64
			var n int
			if err := rows.Scan(&pid, &vid, &n); err != nil {
				rows.Close()
				return nil, err
			}
			only[pid] = -1
			if n == 1 && vid != nil {
				only[pid] = *vid
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for i := range c.lines {
			if c.lines[i].variantID > 0 {
				continue
			}
			k := fmt.Sprintf("purchaseItems[%d].variantId", i)
			switch v := only[c.lines[i].productID]; {
			case v == 0:
				f[k] = "Product not found."
			case v < 0:
				f[k] = "Choose a variant for this product."
			default:
				c.lines[i].variantID = v
			}
		}
	}
	ids := make([]int64, 0, len(c.lines))
	for _, l := range c.lines {
		ids = append(ids, l.variantID)
	}
	byID := map[int64]variantRow{}
	rows, err := tx.Query(ctx, qVariants, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var v variantRow
		if err := rows.Scan(&id, &v.productID, &v.sku, &v.name); err != nil {
			rows.Close()
			return nil, err
		}
		byID[id] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	activeStores := map[int64]bool{}
	rows, err = tx.Query(ctx, qStores, storeIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		activeStores[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, l := range c.lines {
		k := fmt.Sprintf("purchaseItems[%d]", i)
		if l.variantID > 0 {
			v, ok := byID[l.variantID]
			switch {
			case !ok:
				f[k+".variantId"] = "Product variant not found."
			case l.productID > 0 && v.productID != l.productID:
				f[k+".variantId"] = "The variant does not belong to this product."
			}
		}
		if !activeStores[l.storeID] {
			f[k+".storeId"] = "Store not found or inactive."
		}
	}
	return byID, apperr.Fields(f)
}

// ---------------------------------------------------------------- amounts

type pricedLine struct {
	line
	v               variantRow
	gross, subtotal decimal.Decimal
}

type priced struct {
	lines                     []pricedLine
	subtotal, discount, total decimal.Decimal
}

func price(c clean, byID map[int64]variantRow, currency string) (priced, error) {
	f := map[string]string{}
	var p priced
	for i, l := range c.lines {
		gross := money.Round(l.price.Mul(decimal.NewFromInt(l.qty)), currency)
		disc := money.Round(l.discount, currency)
		if disc.GreaterThan(gross) {
			f[fmt.Sprintf("purchaseItems[%d].discount", i)] = "Discount cannot be larger than the line amount."
		}
		l.discount = disc
		x := pricedLine{line: l, v: byID[l.variantID], gross: gross, subtotal: gross.Sub(disc)}
		p.lines = append(p.lines, x)
		p.subtotal, p.discount, p.total = p.subtotal.Add(gross), p.discount.Add(disc), p.total.Add(x.subtotal)
	}
	return p, apperr.Fields(f)
}

// payment derives due and the payment status. paid above the total is
// refused rather than capped: a purchase is entered by hand.
func payment(total, paid decimal.Decimal, overdue bool, currency string) (decimal.Decimal, decimal.Decimal, string, error) {
	paid = money.Round(paid, currency)
	if paid.GreaterThan(total) {
		return paid, decimal.Zero, "", apperr.Invalid("paid", "Paid cannot be more than the total.")
	}
	status := "Unpaid"
	switch {
	case paid.Equal(total):
		status = "Paid"
	case overdue:
		status = "Overdue"
	case paid.IsPositive():
		status = "Partial"
	}
	return paid, total.Sub(paid), status, nil
}

// --------------------------------------------------------------- receive

// receive puts every line's quantity into its store, through the ledger,
// in deterministic lock order. The caller holds the purchase's row lock,
// so a purchase is received at most once.
func receive(ctx context.Context, tx tenancy.TenantTx, purchaseID, userID int64) (int, error) {
	rows, err := tx.Query(ctx, `SELECT store_id, variant_id, quantity FROM purchase_items WHERE purchase_id = $1`, purchaseID)
	if err != nil {
		return 0, err
	}
	deltas := map[inventory.Key]int{}
	units := 0
	for rows.Next() {
		var k inventory.Key
		var q int
		if err := rows.Scan(&k.Store, &k.Variant, &q); err != nil {
			rows.Close()
			return 0, err
		}
		deltas[k] += q
		units += q
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return units, inventory.LockAndApply(ctx, tx, deltas, nil, inventory.ReasonPurchase, inventory.Ref{Type: "purchase", ID: purchaseID}, userID)
}

// ----------------------------------------------------------------- create

const (
	qInsertPurchase = `INSERT INTO purchases (reference, date, status, payment_status, subtotal, discount, total, paid, due,
			currency, note, supplier_id, received_at, received_by, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''), $12,
			CASE WHEN $3 = 'Received' THEN now() END, CASE WHEN $3 = 'Received' THEN $13::bigint END, $13)
		RETURNING id`
	qInsertItem = `INSERT INTO purchase_items (purchase_id, product_id, variant_id, store_id, product_name, sku, quantity,
			unit_price, discount, subtotal)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
)

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "purchases_supplier_reference_key"
}

func nextReference(ctx context.Context, tx tenancy.TenantTx) (string, error) {
	var n int64
	if err := tx.QueryRow(ctx, `UPDATE doc_counters SET next_value = next_value + 1 WHERE name = 'purchase' RETURNING next_value - 1`).Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("PUR-%06d", n), nil
}

func tenantLocation(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// create is POST /api/purchases. Creating it as Received needs
// purchases:approve and stocks every line at once.
func (a *API) create(w http.ResponseWriter, r *http.Request) {
	key, err := idempotency.Key(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	hash := idempotency.Hash(r, hashScope)
	var in purchaseIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	sum := hash()
	c, err := validate(in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	if c.status == statusReceived && !p.Has("purchases:approve") {
		httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeForbidden, "Receiving a purchase needs the approve permission."))
		return
	}
	now := a.d.Now()
	var (
		out   PurchaseJSON
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
		s, err := settings.LoadBusiness(ctx, tx)
		if err != nil {
			return err
		}
		date, err := docdate.Parse(c.date, tenantLocation(s.Timezone), now)
		if err != nil {
			return err
		}
		byID, err := resolve(ctx, tx, &c)
		if err != nil {
			return err
		}
		pr, err := price(c, byID, s.Currency)
		if err != nil {
			return err
		}
		paid, due, payStatus, err := payment(pr.total, c.paid, c.overdue, s.Currency)
		if err != nil {
			return err
		}
		ref := c.reference
		if ref == "" {
			if ref, err = nextReference(ctx, tx); err != nil {
				return err
			}
		}
		var id int64
		err = tx.QueryRow(ctx, qInsertPurchase, ref, date, c.status, payStatus, pr.subtotal, pr.discount, pr.total, paid, due,
			s.Currency, c.note, c.supplierID, p.UserID).Scan(&id)
		if isUniqueViolation(err) {
			return apperr.Conflict(apperr.CodeConflict, fmt.Sprintf("This supplier already has a purchase with reference %s.", ref))
		}
		if err != nil {
			return err
		}
		for _, l := range pr.lines {
			if _, err := tx.Exec(ctx, qInsertItem, id, l.v.productID, l.variantID, l.storeID, l.v.name, l.v.sku, l.qty,
				l.price, l.discount, l.subtotal); err != nil {
				return err
			}
		}
		meta := audit.Entry{ActorID: p.UserID, Entity: "purchase", EntityID: strconv.FormatInt(id, 10),
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}
		e := meta
		e.Action = "purchase.create"
		e.After = map[string]any{"reference": ref, "supplier_id": c.supplierID, "status": c.status, "total": pr.total, "lines": len(pr.lines)}
		if err := audit.Write(ctx, tx, e); err != nil {
			return err
		}
		if c.status == statusReceived {
			units, err := receive(ctx, tx, id, p.UserID)
			if err != nil {
				return err
			}
			e := meta
			e.Action, e.After = "purchase.receive", map[string]any{"reference": ref, "units": units}
			if err := audit.Write(ctx, tx, e); err != nil {
				return err
			}
		}
		l, err := load(ctx, tx, []int64{id})
		if err != nil {
			return err
		}
		if len(l) == 0 {
			return apperr.Internal(nil)
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

// ----------------------------------------------------------------- update

// update is PUT /api/purchases/{id}: status, payment and note. Moving to
// Received needs purchases:approve and stocks the lines; Received and
// Cancelled are final (409 INVALID_STATE on any other status).
func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in updateIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := map[string]string{}
	newStatus := statusChoice(in.Status, f)
	overdue, overdueSet := paymentChoice(in.PaymentStatus, f)
	note, noteSet := noteChoice(in.Note, f)
	if in.Paid.Valid && in.Paid.D.IsNegative() {
		f["paid"] = "Must not be negative."
	}
	if err := apperr.Fields(f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	var out PurchaseJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		var (
			status, payStatus, currency, ref string
			total, paid                      decimal.Decimal
			oldNote                          *string
		)
		err := tx.QueryRow(ctx, `SELECT status, payment_status, total, paid, currency, reference, note FROM purchases WHERE id = $1 FOR UPDATE`, id).
			Scan(&status, &payStatus, &total, &paid, &currency, &ref, &oldNote)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if newStatus == "" {
			newStatus = status
		}
		switch {
		case status == statusReceived && newStatus != statusReceived:
			return apperr.Conflict(apperr.CodeInvalidState, "A received purchase cannot change status. Correct stock with a stock adjustment.")
		case status == statusCancelled && newStatus != statusCancelled:
			return apperr.Conflict(apperr.CodeInvalidState, "A cancelled purchase cannot be reopened.")
		case newStatus == statusReceived && status != statusReceived && !p.Has("purchases:approve"):
			return apperr.Forbidden(apperr.CodeForbidden, "Receiving a purchase needs the approve permission.")
		}
		if !overdueSet {
			overdue = payStatus == "Overdue"
		}
		newPaid := paid
		if in.Paid.Valid {
			newPaid = in.Paid.D
		}
		newPaid, due, newPayStatus, err := payment(total, newPaid, overdue, currency)
		if err != nil {
			return err
		}
		if !noteSet {
			note = deref(oldNote)
		}
		receiving := newStatus == statusReceived && status != statusReceived
		if _, err := tx.Exec(ctx, `UPDATE purchases SET status = $2, payment_status = $3, paid = $4, due = $5, note = NULLIF($6, ''),
				received_at = CASE WHEN $7 THEN now() ELSE received_at END,
				received_by = CASE WHEN $7 THEN $8::bigint ELSE received_by END,
				updated_by = $8, updated_at = now()
			WHERE id = $1`, id, newStatus, newPayStatus, newPaid, due, note, receiving, p.UserID); err != nil {
			return err
		}
		meta := audit.Entry{ActorID: p.UserID, Entity: "purchase", EntityID: strconv.FormatInt(id, 10),
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}
		e := meta
		e.Action = "purchase.update"
		e.Before = map[string]any{"status": status, "payment_status": payStatus, "paid": paid}
		e.After = map[string]any{"status": newStatus, "payment_status": newPayStatus, "paid": newPaid}
		if err := audit.Write(ctx, tx, e); err != nil {
			return err
		}
		if receiving {
			units, err := receive(ctx, tx, id, p.UserID)
			if err != nil {
				return err
			}
			e := meta
			e.Action, e.After = "purchase.receive", map[string]any{"reference": ref, "units": units}
			if err := audit.Write(ctx, tx, e); err != nil {
				return err
			}
		}
		l, err := load(ctx, tx, []int64{id})
		if err != nil {
			return err
		}
		if len(l) == 0 {
			return apperr.Internal(nil)
		}
		out = l[0]
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
