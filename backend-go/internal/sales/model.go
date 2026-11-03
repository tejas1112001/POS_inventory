package sales

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// NameRef is the {name} object the old API nested (customer, user, store,
// product).
type NameRef struct {
	Name string `json:"name"`
}

// VariantRef is the nested {sku, attributes} of an order line.
type VariantRef struct {
	SKU        *string         `json:"sku"`
	Attributes json.RawMessage `json:"attributes"`
}

// ItemJSON is one order line. quantity/qty and unit_price/unitPrice/price
// are the aliases the old receipt code reads.
type ItemJSON struct {
	ID           int64            `json:"id"`
	SaleID       int64            `json:"sale_id"`
	ProductID    *int64           `json:"product_id"`
	VariantID    *int64           `json:"variant_id"`
	Name         string           `json:"name"`
	ProductName  string           `json:"product_name"`
	SKU          *string          `json:"sku"`
	Quantity     int              `json:"quantity"`
	Qty          int              `json:"qty"`
	UnitPrice    decimal.Decimal  `json:"unit_price"`
	UnitPriceC   decimal.Decimal  `json:"unitPrice"`
	Price        decimal.Decimal  `json:"price"`
	CatalogPrice decimal.Decimal  `json:"catalog_price"`
	Discount     decimal.Decimal  `json:"discount"`
	TaxType      *string          `json:"tax_type"`
	TaxRate      decimal.Decimal  `json:"tax_rate"`
	TaxAmount    decimal.Decimal  `json:"tax_amount"`
	CGST         *decimal.Decimal `json:"cgst"`
	SGST         *decimal.Decimal `json:"sgst"`
	IGST         *decimal.Decimal `json:"igst"`
	Subtotal     decimal.Decimal  `json:"subtotal"`
	ReturnedQty  int              `json:"returnedQty"` // returned so far, over all returns of the sale
	CreatedAt    time.Time        `json:"createdAt"`
	Product      NameRef          `json:"product"`
	Variant      *VariantRef      `json:"variant"`
}

// SaleJSON keeps the old field names (snake_case columns, camelCase
// timestamps, nested customer/user/items).
type SaleJSON struct {
	ID            int64            `json:"id"`
	Reference     string           `json:"reference"`
	Date          time.Time        `json:"date"`
	Status        string           `json:"status"`
	PaymentStatus string           `json:"payment_status"`
	PaymentMethod *string          `json:"payment_method"`
	PaymentNote   *string          `json:"payment_note"`
	Note          *string          `json:"note"`
	Subtotal      decimal.Decimal  `json:"subtotal"`
	Discount      decimal.Decimal  `json:"discount"`
	Tax           decimal.Decimal  `json:"tax"`
	Shipping      decimal.Decimal  `json:"shipping"`
	Total         decimal.Decimal  `json:"total"`
	Paid          decimal.Decimal  `json:"paid"`
	Due           decimal.Decimal  `json:"due"`
	Tendered      *decimal.Decimal `json:"tendered"`
	Change        *decimal.Decimal `json:"change"`
	Currency      string           `json:"currency"`
	CustomerID    int64            `json:"customer_id"`
	StoreID       int64            `json:"store_id"`
	UserID        int64            `json:"user_id"`
	UpdatedBy     *int64           `json:"updated_by"`
	ReceiptEmail  *string          `json:"receipt_email"`
	VoidedAt      *time.Time       `json:"voided_at"`
	VoidedBy      *int64           `json:"voided_by"`
	VoidReason    *string          `json:"void_reason"`
	Version       int              `json:"version"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
	Customer      *NameRef         `json:"customer"`
	User          *NameRef         `json:"user"`
	Store         *NameRef         `json:"Store"`
	Items         []ItemJSON       `json:"items"`
	ReceiptURL    string           `json:"receiptUrl"`
	// S10 item 5: how the sale was paid, when the rest is due (YYYY-MM-DD)
	// and whether it is overdue (payment_status is then "Overdue").
	Payments []PaymentJSON `json:"payments"`
	DueDate  *string       `json:"dueDate"`
	Overdue  bool          `json:"overdue"`
}

// sqlToday is today's date in the shop's time zone.
const sqlToday = `(now() AT TIME ZONE (SELECT timezone FROM settings WHERE id = 1))::date`

// sqlOverdue is true for a sale with something due past its due date.
const sqlOverdue = `(s.due > 0 AND s.due_date IS NOT NULL AND s.due_date < ` + sqlToday + `)`

const (
	qLoadSales = `SELECT s.id, s.reference, s.date, s.status, s.payment_status, s.payment_method, s.payment_note, s.note,
			s.subtotal, s.discount, s.tax, s.shipping, s.total, s.paid, s.due, s.tendered, s.change_due, s.currency,
			s.customer_id, s.store_id, s.user_id, s.updated_by, s.receipt_email::text, s.voided_at, s.voided_by, s.void_reason,
			s.version, s.created_at, s.updated_at, c.name, u.name, st.name,
			to_char(s.due_date, 'YYYY-MM-DD'), ` + sqlOverdue + `
		FROM sales s
		LEFT JOIN customers c ON c.id = s.customer_id
		LEFT JOIN users u ON u.id = s.user_id
		LEFT JOIN stores st ON st.id = s.store_id
		WHERE s.id = ANY($1)`
	qLoadItems = `SELECT i.id, i.sale_id, i.product_id, i.variant_id, i.product_name, i.sku, i.quantity, i.unit_price,
			i.catalog_price, i.discount, i.tax_type, i.tax_rate, i.tax_amount, i.cgst, i.sgst, i.igst, i.subtotal,
			i.created_at, v.attributes,
			coalesce((SELECT sum(r.quantity) FROM return_items r WHERE r.order_item_id = i.id), 0)
		FROM order_items i LEFT JOIN product_variants v ON v.id = i.variant_id
		WHERE i.sale_id = ANY($1) ORDER BY i.sale_id, i.id`
)

// load returns sales in the order of ids, with their lines.
func load(ctx context.Context, tx tenancy.TenantTx, ids []int64) ([]SaleJSON, error) {
	if len(ids) == 0 {
		return []SaleJSON{}, nil
	}
	byID := make(map[int64]*SaleJSON, len(ids))
	rows, err := tx.Query(ctx, qLoadSales, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		s := &SaleJSON{Items: []ItemJSON{}}
		var cust, user, store *string
		if err := rows.Scan(&s.ID, &s.Reference, &s.Date, &s.Status, &s.PaymentStatus, &s.PaymentMethod, &s.PaymentNote, &s.Note,
			&s.Subtotal, &s.Discount, &s.Tax, &s.Shipping, &s.Total, &s.Paid, &s.Due, &s.Tendered, &s.Change, &s.Currency,
			&s.CustomerID, &s.StoreID, &s.UserID, &s.UpdatedBy, &s.ReceiptEmail, &s.VoidedAt, &s.VoidedBy, &s.VoidReason,
			&s.Version, &s.CreatedAt, &s.UpdatedAt, &cust, &user, &store, &s.DueDate, &s.Overdue); err != nil {
			rows.Close()
			return nil, err
		}
		s.Payments = []PaymentJSON{}
		if s.Overdue && s.Status != "Void" {
			s.PaymentStatus = "Overdue"
		}
		s.Customer, s.User, s.Store = ref(cust), ref(user), ref(store)
		s.ReceiptURL = "/api/sales/" + strconv.FormatInt(s.ID, 10) + "/receipt"
		byID[s.ID] = s
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
		var attrs []byte
		if err := rows.Scan(&it.ID, &it.SaleID, &it.ProductID, &it.VariantID, &it.ProductName, &it.SKU, &it.Quantity, &it.UnitPrice,
			&it.CatalogPrice, &it.Discount, &it.TaxType, &it.TaxRate, &it.TaxAmount, &it.CGST, &it.SGST, &it.IGST, &it.Subtotal,
			&it.CreatedAt, &attrs, &it.ReturnedQty); err != nil {
			rows.Close()
			return nil, err
		}
		it.Name, it.Product = it.ProductName, NameRef{Name: it.ProductName}
		it.Qty, it.UnitPriceC, it.Price = it.Quantity, it.UnitPrice, it.UnitPrice
		if it.VariantID != nil {
			if attrs == nil {
				attrs = []byte("{}")
			}
			it.Variant = &VariantRef{SKU: it.SKU, Attributes: attrs}
		}
		if s := byID[it.SaleID]; s != nil {
			s.Items = append(s.Items, it)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = tx.Query(ctx, `SELECT id, sale_id, method, amount, tendered, change_due, reference, customer_payment_id, created_at
		FROM sale_payments WHERE sale_id = ANY($1) ORDER BY sale_id, id`, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p PaymentJSON
		var saleID int64
		if err := rows.Scan(&p.ID, &saleID, &p.Method, &p.Amount, &p.Tendered, &p.Change, &p.Reference, &p.CustomerPaymentID, &p.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if s := byID[saleID]; s != nil {
			s.Payments = append(s.Payments, p)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]SaleJSON, 0, len(ids))
	for _, id := range ids {
		if s := byID[id]; s != nil {
			out = append(out, *s)
		}
	}
	return out, nil
}

func loadOne(ctx context.Context, tx tenancy.TenantTx, id int64) (SaleJSON, bool, error) {
	l, err := load(ctx, tx, []int64{id})
	if err != nil || len(l) == 0 {
		return SaleJSON{}, false, err
	}
	return l[0], true, nil
}

func ref(name *string) *NameRef {
	if name == nil {
		return nil
	}
	return &NameRef{Name: *name}
}
