package purchases

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// NameRef is a nested {id, name}.
type NameRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// SupplierRef is the nested supplier.
type SupplierRef struct {
	ID    int64   `json:"id"`
	Name  string  `json:"name"`
	Email *string `json:"email"`
	Phone *string `json:"phone"`
}

// ItemJSON is one purchase line. unitPrice is the old field name.
type ItemJSON struct {
	ID          int64           `json:"id"`
	PurchaseID  int64           `json:"purchase_id"`
	ProductID   int64           `json:"product_id"`
	VariantID   int64           `json:"variant_id"`
	StoreID     int64           `json:"store_id"`
	ProductName string          `json:"product_name"`
	SKU         *string         `json:"sku"`
	Quantity    int             `json:"quantity"`
	UnitPrice   decimal.Decimal `json:"unitPrice"`
	UnitPriceS  decimal.Decimal `json:"unit_price"`
	Discount    decimal.Decimal `json:"discount"`
	Subtotal    decimal.Decimal `json:"subtotal"`
	CreatedAt   time.Time       `json:"createdAt"`
	Product     NameRef         `json:"product"`
	Store       *NameRef        `json:"store"`
}

// PurchaseJSON keeps the old field names and adds paymentStatus and the
// capitalised Supplier that Purchases.jsx reads.
type PurchaseJSON struct {
	ID             int64           `json:"id"`
	Reference      string          `json:"reference"`
	Date           time.Time       `json:"date"`
	Status         string          `json:"status"`
	PaymentStatus  string          `json:"payment_status"`
	PaymentStatusC string          `json:"paymentStatus"`
	Subtotal       decimal.Decimal `json:"subtotal"`
	Discount       decimal.Decimal `json:"discount"`
	Total          decimal.Decimal `json:"total"`
	Paid           decimal.Decimal `json:"paid"`
	Due            decimal.Decimal `json:"due"`
	Currency       string          `json:"currency"`
	Note           *string         `json:"note"`
	SupplierID     int64           `json:"supplier_id"`
	ReceivedAt     *time.Time      `json:"received_at"`
	ReceivedBy     *int64          `json:"received_by"`
	CreatedBy      int64           `json:"created_by"`
	UpdatedBy      *int64          `json:"updated_by"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	Supplier       *SupplierRef    `json:"supplier"`
	SupplierC      *SupplierRef    `json:"Supplier"`
	Items          []ItemJSON      `json:"items"`
}

const (
	qLoad = `SELECT p.id, p.reference, p.date, p.status, p.payment_status, p.subtotal, p.discount, p.total, p.paid, p.due,
			p.currency, p.note, p.supplier_id, p.received_at, p.received_by, p.created_by, p.updated_by, p.created_at,
			p.updated_at, s.name, s.email::text, s.phone
		FROM purchases p LEFT JOIN suppliers s ON s.id = p.supplier_id
		WHERE p.id = ANY($1)`
	qLoadItems = `SELECT i.id, i.purchase_id, i.product_id, i.variant_id, i.store_id, i.product_name, i.sku, i.quantity,
			i.unit_price, i.discount, i.subtotal, i.created_at, st.name
		FROM purchase_items i LEFT JOIN stores st ON st.id = i.store_id
		WHERE i.purchase_id = ANY($1) ORDER BY i.purchase_id, i.id`
)

// load returns purchases in the order of ids, with their lines.
func load(ctx context.Context, tx tenancy.TenantTx, ids []int64) ([]PurchaseJSON, error) {
	if len(ids) == 0 {
		return []PurchaseJSON{}, nil
	}
	byID := make(map[int64]*PurchaseJSON, len(ids))
	rows, err := tx.Query(ctx, qLoad, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		x := &PurchaseJSON{Items: []ItemJSON{}}
		var name *string
		var email, phone *string
		if err := rows.Scan(&x.ID, &x.Reference, &x.Date, &x.Status, &x.PaymentStatus, &x.Subtotal, &x.Discount, &x.Total,
			&x.Paid, &x.Due, &x.Currency, &x.Note, &x.SupplierID, &x.ReceivedAt, &x.ReceivedBy, &x.CreatedBy, &x.UpdatedBy,
			&x.CreatedAt, &x.UpdatedAt, &name, &email, &phone); err != nil {
			rows.Close()
			return nil, err
		}
		x.PaymentStatusC = x.PaymentStatus
		if name != nil {
			x.Supplier = &SupplierRef{ID: x.SupplierID, Name: *name, Email: email, Phone: phone}
			x.SupplierC = x.Supplier
		}
		byID[x.ID] = x
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = tx.Query(ctx, qLoadItems, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var it ItemJSON
		var store *string
		if err := rows.Scan(&it.ID, &it.PurchaseID, &it.ProductID, &it.VariantID, &it.StoreID, &it.ProductName, &it.SKU,
			&it.Quantity, &it.UnitPrice, &it.Discount, &it.Subtotal, &it.CreatedAt, &store); err != nil {
			rows.Close()
			return nil, err
		}
		it.UnitPriceS = it.UnitPrice
		it.Product = NameRef{ID: it.ProductID, Name: it.ProductName}
		if store != nil {
			it.Store = &NameRef{ID: it.StoreID, Name: *store}
		}
		if x := byID[it.PurchaseID]; x != nil {
			x.Items = append(x.Items, it)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]PurchaseJSON, 0, len(ids))
	for _, id := range ids {
		if x := byID[id]; x != nil {
			out = append(out, *x)
		}
	}
	return out, nil
}

func (a *API) ro(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, fn)
}

const (
	qFilter = ` FROM purchases p WHERE ($1 = 0 OR p.supplier_id = $1) AND ($2 = '' OR p.status = $2)`
	qCount  = `SELECT count(*)` + qFilter
	qIDs    = `SELECT p.id` + qFilter + ` ORDER BY p.date DESC, p.id DESC LIMIT $3 OFFSET $4`
)

// list is GET /api/purchases: newest first, a bare array unless page/limit
// are given; ?supplierId= and ?status= filter.
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	q := r.URL.Query()
	supplier, err := strconv.ParseInt(strings.TrimSpace(q.Get("supplierId")), 10, 64)
	if err != nil || supplier < 0 {
		supplier = 0
	}
	status := strings.TrimSpace(q.Get("status"))
	var (
		out   []PurchaseJSON
		total int
	)
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qCount, supplier, status).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qIDs, supplier, status, p.Limit, p.Offset)
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

// get is GET /api/purchases/{id}.
func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out []PurchaseJSON
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = load(r.Context(), tx, []int64{id})
		return err
	})
	if err == nil && len(out) == 0 {
		err = apperr.NotFound()
	}
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out[0])
}
