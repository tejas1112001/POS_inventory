package reports

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/docdate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// SalesFilter selects sales report rows.
type SalesFilter struct {
	Scope                          int64 // user ID (own-only) or 0
	From, To                       *time.Time
	ProductID, CustomerID, StoreID int64
	IncludeVoided                  bool
}

// NamedRef is a nested {id, name}.
type NamedRef struct {
	ID   *int64 `json:"id"`
	Name string `json:"name"`
}

// SalesProduct is the nested product of a sales report row.
type SalesProduct struct {
	ID    *int64 `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	Brand string `json:"brand"`
}

// SalesRow is one sold line. id, sku, date, product, category, soldQty,
// soldAmount and stockQty keep the old names; sku is still the old
// "SO-<sale>-<line>" label (itemSku is the product's SKU). stockQty is the
// current stock of the variant in the sale's store (it was always 0).
type SalesRow struct {
	ID             string          `json:"id"`
	SKU            string          `json:"sku"`
	Date           time.Time       `json:"date"`
	Product        SalesProduct    `json:"product"`
	Category       string          `json:"category"`
	SoldQty        int             `json:"soldQty"`
	SoldAmount     decimal.Decimal `json:"soldAmount"`
	StockQty       int             `json:"stockQty"`
	SaleID         int64           `json:"saleId"`
	Reference      string          `json:"reference"`
	Status         string          `json:"status"`
	ItemSKU        *string         `json:"itemSku"`
	VariantID      *int64          `json:"variantId"`
	Store          NamedRef        `json:"store"`
	Customer       NamedRef        `json:"customer"`
	CustomerName   string          `json:"customerName"`
	UnitPrice      decimal.Decimal `json:"unitPrice"`
	Discount       decimal.Decimal `json:"discount"`
	Tax            decimal.Decimal `json:"tax"`
	ReturnedQty    int             `json:"returnedQty"`
	ReturnedAmount decimal.Decimal `json:"returnedAmount"`
	NetAmount      decimal.Decimal `json:"netAmount"`
}

const (
	qSalesWhere = ` WHERE ($1 = 0 OR s.user_id = $1) AND ($2 OR s.status <> 'Void')
		AND ($3::timestamptz IS NULL OR s.date >= $3) AND ($4::timestamptz IS NULL OR s.date < $4)
		AND ($5 = 0 OR i.product_id = $5) AND ($6 = 0 OR s.customer_id = $6) AND ($7 = 0 OR s.store_id = $7)`
	qSalesCount = `SELECT count(*) FROM order_items i JOIN sales s ON s.id = i.sale_id` + qSalesWhere
	qSalesRows  = `SELECT i.id, i.sale_id, s.reference, s.date, s.status, s.store_id, st.name, s.customer_id, c.name,
			i.product_id, i.variant_id, i.product_name, i.sku, p.image_file_id, coalesce(b.name, 'No Brand'),
			coalesce(cat.name, 'Uncategorized'), i.quantity, i.unit_price, i.discount, i.tax_amount, i.subtotal,
			coalesce(r.qty, 0), coalesce(r.amount, 0), coalesce(inv.qty, 0)
		FROM order_items i
		JOIN sales s ON s.id = i.sale_id
		LEFT JOIN stores st ON st.id = s.store_id
		LEFT JOIN customers c ON c.id = s.customer_id
		LEFT JOIN products p ON p.id = i.product_id
		LEFT JOIN brands b ON b.id = p.brand_id
		LEFT JOIN categories cat ON cat.id = p.category_id
		LEFT JOIN LATERAL (SELECT sum(x.quantity) AS qty, sum(x.subtotal) AS amount
			FROM return_items x WHERE x.order_item_id = i.id) r ON true
		LEFT JOIN inventories inv ON inv.store_id = s.store_id AND inv.variant_id = i.variant_id` +
		qSalesWhere + ` ORDER BY s.date DESC, s.id DESC, i.id LIMIT $8 OFFSET $9`
)

func (f SalesFilter) args() []any {
	return []any{f.Scope, f.IncludeVoided, f.From, f.To, f.ProductID, f.CustomerID, f.StoreID}
}

// salesFilter reads the query: startDate, endDate (whole days in the
// tenant's time zone), productId, customerId, storeId, includeVoided.
func (a *API) salesFilter(r *http.Request, b settings.Business) (SalesFilter, error) {
	q := r.URL.Query()
	loc := location(b)
	from, err := docdate.Bound(q.Get("startDate"), loc, false)
	if err != nil {
		return SalesFilter{}, err
	}
	to, err := docdate.Bound(q.Get("endDate"), loc, true)
	if err != nil {
		return SalesFilter{}, err
	}
	return SalesFilter{Scope: ownerScope(principal(r)), From: from, To: to, ProductID: queryID(r, "productId"),
		CustomerID: queryID(r, "customerId"), StoreID: queryID(r, "storeId"), IncludeVoided: q.Get("includeVoided") == "true"}, nil
}

// SalesRows returns up to limit rows after offset, and the total count.
func SalesRows(ctx context.Context, tx tenancy.TenantTx, f SalesFilter, limit, offset int) ([]SalesRow, int, error) {
	var total int
	if err := tx.QueryRow(ctx, qSalesCount, f.args()...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, qSalesRows, append(f.args(), limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []SalesRow{}
	for rows.Next() {
		var (
			x                 SalesRow
			itemID            int64
			storeName, custNm *string
			storeID, custID   int64
			img               *uuid.UUID
		)
		if err := rows.Scan(&itemID, &x.SaleID, &x.Reference, &x.Date, &x.Status, &storeID, &storeName, &custID, &custNm,
			&x.Product.ID, &x.VariantID, &x.Product.Name, &x.ItemSKU, &img, &x.Product.Brand, &x.Category, &x.SoldQty,
			&x.UnitPrice, &x.Discount, &x.Tax, &x.SoldAmount, &x.ReturnedQty, &x.ReturnedAmount, &x.StockQty); err != nil {
			return nil, 0, err
		}
		x.ID = fmt.Sprintf("%d-%d", x.SaleID, itemID)
		x.SKU = fmt.Sprintf("SO-%d-%d", x.SaleID, itemID)
		x.Product.Image = privateImage(img)
		x.Store = NamedRef{ID: &storeID, Name: str(storeName)}
		x.CustomerName = str(custNm)
		x.Customer = NamedRef{ID: &custID, Name: x.CustomerName}
		x.NetAmount = x.SoldAmount.Sub(x.ReturnedAmount)
		out = append(out, x)
	}
	return out, total, rows.Err()
}

// salesReport is GET /api/sales/report: one row per sold line, newest
// first; a bare array unless page/limit are given (C-10: own-only).
func (a *API) salesReport(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	var (
		out   []SalesRow
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		b, err := settings.LoadBusiness(r.Context(), tx)
		if err != nil {
			return err
		}
		f, err := a.salesFilter(r, b)
		if err != nil {
			return err
		}
		out, total, err = SalesRows(r.Context(), tx, f, p.Limit, p.Offset)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}
