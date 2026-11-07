package reports

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/docdate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// PurchaseFilter selects purchase report rows.
type PurchaseFilter struct {
	From, To            *time.Time
	SupplierID, StoreID int64
	ProductID           int64
	IncludeCancelled    bool
}

// PurchaseProduct is the nested product of a purchase report row.
type PurchaseProduct struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
}

// PurchaseRow is one purchased line. id, reference, sku, dueDate (the
// purchase date), product, category, purchaseQty, purchaseAmount and
// stockQty keep the old names; stockQty is the current stock of the
// variant in the line's store.
type PurchaseRow struct {
	ID             string          `json:"id"`
	Reference      string          `json:"reference"`
	SKU            string          `json:"sku"`
	DueDate        time.Time       `json:"dueDate"`
	Product        PurchaseProduct `json:"product"`
	Category       string          `json:"category"`
	PurchaseQty    int             `json:"purchaseQty"`
	PurchaseAmount decimal.Decimal `json:"purchaseAmount"`
	StockQty       int             `json:"stockQty"`
	PurchaseID     int64           `json:"purchaseId"`
	Date           time.Time       `json:"date"`
	Status         string          `json:"status"`
	ItemSKU        *string         `json:"itemSku"`
	VariantID      int64           `json:"variantId"`
	Supplier       NamedRef        `json:"supplier"`
	SupplierName   string          `json:"supplierName"`
	Store          NamedRef        `json:"store"`
	UnitPrice      decimal.Decimal `json:"unitPrice"`
	Discount       decimal.Decimal `json:"discount"`
}

var errNoPurchases = apperr.Forbidden(apperr.CodeForbidden, "The purchase report needs the purchases view permission.")

const (
	qPurchaseWhere = ` WHERE ($1 OR p.status <> 'Cancelled')
		AND ($2::timestamptz IS NULL OR p.date >= $2) AND ($3::timestamptz IS NULL OR p.date < $3)
		AND ($4 = 0 OR p.supplier_id = $4) AND ($5 = 0 OR i.store_id = $5) AND ($6 = 0 OR i.product_id = $6)`
	qPurchaseCount = `SELECT count(*) FROM purchase_items i JOIN purchases p ON p.id = i.purchase_id` + qPurchaseWhere
	qPurchaseRows  = `SELECT i.id, i.purchase_id, p.reference, p.date, p.status, p.supplier_id, sup.name, i.store_id, st.name,
			i.product_id, i.variant_id, i.product_name, i.sku, pr.image_file_id, coalesce(cat.name, 'Uncategorized'),
			i.quantity, i.unit_price, i.discount, i.subtotal, coalesce(inv.qty, 0)
		FROM purchase_items i
		JOIN purchases p ON p.id = i.purchase_id
		LEFT JOIN suppliers sup ON sup.id = p.supplier_id
		LEFT JOIN stores st ON st.id = i.store_id
		LEFT JOIN products pr ON pr.id = i.product_id
		LEFT JOIN categories cat ON cat.id = pr.category_id
		LEFT JOIN inventories inv ON inv.store_id = i.store_id AND inv.variant_id = i.variant_id` +
		qPurchaseWhere + ` ORDER BY p.date DESC, p.id DESC, i.id LIMIT $7 OFFSET $8`
)

func (f PurchaseFilter) args() []any {
	return []any{f.IncludeCancelled, f.From, f.To, f.SupplierID, f.StoreID, f.ProductID}
}

// purchaseFilter reads the query: startDate, endDate, supplierId, storeId,
// productId, includeCancelled.
func (a *API) purchaseFilter(r *http.Request, b settings.Business) (PurchaseFilter, error) {
	q := r.URL.Query()
	loc := location(b)
	from, err := docdate.Bound(q.Get("startDate"), loc, false)
	if err != nil {
		return PurchaseFilter{}, err
	}
	to, err := docdate.Bound(q.Get("endDate"), loc, true)
	if err != nil {
		return PurchaseFilter{}, err
	}
	return PurchaseFilter{From: from, To: to, SupplierID: queryID(r, "supplierId"), StoreID: queryID(r, "storeId"),
		ProductID: queryID(r, "productId"), IncludeCancelled: q.Get("includeCancelled") == "true"}, nil
}

// PurchaseRows returns up to limit rows after offset, and the total count.
func PurchaseRows(ctx context.Context, tx tenancy.TenantTx, f PurchaseFilter, limit, offset int) ([]PurchaseRow, int, error) {
	var total int
	if err := tx.QueryRow(ctx, qPurchaseCount, f.args()...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, qPurchaseRows, append(f.args(), limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []PurchaseRow{}
	for rows.Next() {
		var (
			x                   PurchaseRow
			itemID, supID, stID int64
			supName, stName     *string
			img                 *uuid.UUID
		)
		if err := rows.Scan(&itemID, &x.PurchaseID, &x.Reference, &x.Date, &x.Status, &supID, &supName, &stID, &stName,
			&x.Product.ID, &x.VariantID, &x.Product.Name, &x.ItemSKU, &img, &x.Category, &x.PurchaseQty, &x.UnitPrice,
			&x.Discount, &x.PurchaseAmount, &x.StockQty); err != nil {
			return nil, 0, err
		}
		x.ID = fmt.Sprintf("%d-%d", x.PurchaseID, itemID)
		x.SKU = fmt.Sprintf("PO-%d-%d", x.PurchaseID, itemID)
		x.DueDate = x.Date
		x.Product.Image = privateImage(img)
		x.SupplierName = str(supName)
		x.Supplier = NamedRef{ID: &supID, Name: x.SupplierName}
		x.Store = NamedRef{ID: &stID, Name: str(stName)}
		out = append(out, x)
	}
	return out, total, rows.Err()
}

// purchaseReport is GET /api/purchase/report: one row per purchased line,
// newest first, cancelled purchases left out unless asked for. It shows
// supplier costs, so it also needs purchases:view (billers have
// reports:view but not purchases:view).
func (a *API) purchaseReport(w http.ResponseWriter, r *http.Request) {
	if !principal(r).Has("purchases:view") {
		httpx.WriteError(w, r, errNoPurchases)
		return
	}
	p := httpx.ParsePage(r, a.d.MaxRows)
	var (
		out   []PurchaseRow
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		b, err := settings.LoadBusiness(r.Context(), tx)
		if err != nil {
			return err
		}
		f, err := a.purchaseFilter(r, b)
		if err != nil {
			return err
		}
		out, total, err = PurchaseRows(r.Context(), tx, f, p.Limit, p.Offset)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}
