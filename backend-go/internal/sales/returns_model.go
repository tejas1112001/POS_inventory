package sales

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// ReturnItemJSON is one return line. unitPrice is the old field name.
type ReturnItemJSON struct {
	ID           int64            `json:"id"`
	SaleReturnID int64            `json:"sale_return_id"`
	OrderItemID  *int64           `json:"order_item_id"`
	ProductID    *int64           `json:"product_id"`
	VariantID    int64            `json:"variant_id"`
	ProductName  string           `json:"product_name"`
	SKU          *string          `json:"sku"`
	Quantity     int              `json:"quantity"`
	UnitPrice    decimal.Decimal  `json:"unitPrice"`
	UnitPriceS   decimal.Decimal  `json:"unit_price"`
	Discount     decimal.Decimal  `json:"discount"`
	TaxAmount    decimal.Decimal  `json:"tax_amount"`
	CGST         *decimal.Decimal `json:"cgst"`
	SGST         *decimal.Decimal `json:"sgst"`
	IGST         *decimal.Decimal `json:"igst"`
	Subtotal     decimal.Decimal  `json:"subtotal"`
	Reason       string           `json:"reason"`
	CreatedAt    time.Time        `json:"createdAt"`
	Product      returnProduct    `json:"product"`
}

type returnProduct struct {
	ID    *int64  `json:"id"`
	Name  string  `json:"name"`
	Image *string `json:"image"`
}

type returnCustomer struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Email  string  `json:"email"`
	Phone  string  `json:"phone"`
	Image  *string `json:"image"`
	Avatar *string `json:"avatar"`
}

// ReturnJSON is a sales return. It keeps the old list's flattened fields
// (customerName, productName, ...) and adds the nested customer and
// product objects and paymentStatus that SalesReturnList.jsx reads.
type ReturnJSON struct {
	ID             int64            `json:"id"`
	Reference      string           `json:"reference"`
	Date           time.Time        `json:"date"`
	Status         string           `json:"status"`
	Total          decimal.Decimal  `json:"total"`
	Tax            decimal.Decimal  `json:"tax"`
	Paid           decimal.Decimal  `json:"paid"`
	Due            decimal.Decimal  `json:"due"`
	PaymentStatus  string           `json:"payment_status"`
	PaymentStatusC string           `json:"paymentStatus"`
	Currency       string           `json:"currency"`
	Note           *string          `json:"note"`
	SaleID         *int64           `json:"sale_id"`
	SaleReference  *string          `json:"saleReference"`
	CustomerID     int64            `json:"customer_id"`
	StoreID        int64            `json:"store_id"`
	StoreName      *string          `json:"storeName"`
	CreatedBy      int64            `json:"created_by"`
	CreatedAt      time.Time        `json:"createdAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
	CustomerName   string           `json:"customerName"`
	CustomerEmail  string           `json:"customerEmail"`
	CustomerPhone  string           `json:"customerPhone"`
	CustomerImage  string           `json:"customerImage"`
	ProductName    string           `json:"productName"`
	ProductImage   string           `json:"productImage"`
	ItemsCount     int              `json:"itemsCount"`
	Items          []ReturnItemJSON `json:"items"`
	Customer       returnCustomer   `json:"customer"`
	Product        returnProduct    `json:"product"`
}

const (
	qLoadReturns = `SELECT r.id, r.reference, r.date, r.status, r.payment_status, r.total, r.tax, r.paid, r.due, r.currency, r.note,
			r.sale_id, s.reference, r.customer_id, r.store_id, st.name, r.created_by, r.created_at, r.updated_at,
			c.name, c.email::text, c.phone, c.image_file_id
		FROM sale_returns r
		LEFT JOIN sales s ON s.id = r.sale_id
		LEFT JOIN customers c ON c.id = r.customer_id
		LEFT JOIN stores st ON st.id = r.store_id
		WHERE r.id = ANY($1)`
	qLoadReturnItems = `SELECT i.id, i.sale_return_id, i.order_item_id, i.product_id, i.variant_id, i.product_name, i.sku, i.quantity,
			i.unit_price, i.discount, i.tax_amount, i.cgst, i.sgst, i.igst, i.subtotal, coalesce(i.reason, ''), i.created_at,
			p.image_file_id
		FROM return_items i LEFT JOIN products p ON p.id = i.product_id
		WHERE i.sale_return_id = ANY($1) ORDER BY i.sale_return_id, i.id`
)

// privateImage is the signed-in URL of a file (returns are never public).
func privateImage(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := "/api/files/" + id.String()
	return &s
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// loadReturns returns returns in the order of ids, with their lines.
func loadReturns(ctx context.Context, tx tenancy.TenantTx, ids []int64) ([]ReturnJSON, error) {
	if len(ids) == 0 {
		return []ReturnJSON{}, nil
	}
	byID := make(map[int64]*ReturnJSON, len(ids))
	rows, err := tx.Query(ctx, qLoadReturns, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		x := &ReturnJSON{Items: []ReturnItemJSON{}}
		var name, email, phone *string
		var img *uuid.UUID
		if err := rows.Scan(&x.ID, &x.Reference, &x.Date, &x.Status, &x.PaymentStatus, &x.Total, &x.Tax, &x.Paid, &x.Due,
			&x.Currency, &x.Note, &x.SaleID, &x.SaleReference, &x.CustomerID, &x.StoreID, &x.StoreName, &x.CreatedBy,
			&x.CreatedAt, &x.UpdatedAt, &name, &email, &phone, &img); err != nil {
			rows.Close()
			return nil, err
		}
		x.PaymentStatusC = x.PaymentStatus
		x.CustomerName, x.CustomerEmail, x.CustomerPhone = str(name), str(email), str(phone)
		if x.CustomerName == "" {
			x.CustomerName = "Walk-In Customer"
		}
		image := files.URLPtr(img, files.PurposeCustomerImage)
		x.CustomerImage = str(image)
		x.Customer = returnCustomer{ID: x.CustomerID, Name: x.CustomerName, Email: x.CustomerEmail, Phone: x.CustomerPhone,
			Image: image, Avatar: image}
		x.ProductName = "N/A"
		x.Product = returnProduct{Name: "N/A"}
		byID[x.ID] = x
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = tx.Query(ctx, qLoadReturnItems, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var it ReturnItemJSON
		var img *uuid.UUID
		if err := rows.Scan(&it.ID, &it.SaleReturnID, &it.OrderItemID, &it.ProductID, &it.VariantID, &it.ProductName, &it.SKU,
			&it.Quantity, &it.UnitPrice, &it.Discount, &it.TaxAmount, &it.CGST, &it.SGST, &it.IGST, &it.Subtotal, &it.Reason,
			&it.CreatedAt, &img); err != nil {
			rows.Close()
			return nil, err
		}
		it.UnitPriceS = it.UnitPrice
		it.Product = returnProduct{ID: it.ProductID, Name: it.ProductName, Image: privateImage(img)}
		if x := byID[it.SaleReturnID]; x != nil {
			if len(x.Items) == 0 {
				x.Product, x.ProductName, x.ProductImage = it.Product, it.ProductName, str(it.Product.Image)
			}
			x.Items = append(x.Items, it)
			x.ItemsCount = len(x.Items)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]ReturnJSON, 0, len(ids))
	for _, id := range ids {
		if x := byID[id]; x != nil {
			out = append(out, *x)
		}
	}
	return out, nil
}

const (
	qReturnFilter = ` FROM sale_returns r WHERE ($1 = 0 OR r.sale_id = $1)`
	qReturnCount  = `SELECT count(*)` + qReturnFilter
	qReturnIDs    = `SELECT r.id` + qReturnFilter + ` ORDER BY r.date DESC, r.id DESC LIMIT $2 OFFSET $3`
)

// listReturns is GET /api/sales/returns: newest first, a bare array
// unless page/limit are given; ?saleId= narrows to one sale.
func (a *API) listReturns(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	saleID := queryInt(r, "saleId")
	var (
		out   []ReturnJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qReturnCount, saleID).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qReturnIDs, saleID, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		out, err = loadReturns(r.Context(), tx, ids)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}
