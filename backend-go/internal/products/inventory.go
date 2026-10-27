package products

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// inventoryRow is the old flat GET /api/inventory row.
type inventoryRow struct {
	ID            int64           `json:"id"`
	Qty           int             `json:"qty"`
	QuantityAlert int             `json:"quantityAlert"`
	UpdatedAt     time.Time       `json:"updatedAt"`
	VariantID     int64           `json:"variantId"`
	SKU           string          `json:"sku"`
	Attributes    map[string]any  `json:"attributes"`
	Price         decimal.Decimal `json:"price"`
	ProductID     int64           `json:"productId"`
	ProductName   string          `json:"productName"`
	ProductImage  *string         `json:"productImage"`
	StoreID       int64           `json:"storeId"`
	StoreName     string          `json:"storeName"`
	Status        string          `json:"status"`
}

const (
	qInvFilter = ` FROM inventories i
		JOIN product_variants v ON v.id = i.variant_id AND v.deleted_at IS NULL
		JOIN products p ON p.id = v.product_id AND p.deleted_at IS NULL
		JOIN stores s ON s.id = i.store_id AND s.deleted_at IS NULL
		WHERE ($1 = 0 OR i.store_id = $1)`
	qInvCount = `SELECT count(*)` + qInvFilter
	qInvList  = `SELECT i.id, i.qty, i.quantity_alert, i.updated_at, v.id, v.sku::text, v.attributes, v.price, p.id, p.name,
		p.image_file_id, EXISTS (SELECT 1 FROM store_products sp WHERE sp.product_id = p.id AND sp.is_published), s.id, s.name` +
		qInvFilter + ` ORDER BY i.updated_at DESC, i.id DESC LIMIT $2 OFFSET $3`
)

func stockStatus(qty, alert int) string {
	switch {
	case qty <= 0:
		return "Out of Stock"
	case qty <= alert:
		return "Low Stock"
	default:
		return "In Stock"
	}
}

func (a *API) listInventory(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	store, _ := queryInt(r, "storeId")
	var (
		out   []inventoryRow
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qInvCount, store).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qInvList, store, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x inventoryRow
			var img *imageID
			var published bool
			if err := rows.Scan(&x.ID, &x.Qty, &x.QuantityAlert, &x.UpdatedAt, &x.VariantID, &x.SKU, &x.Attributes, &x.Price,
				&x.ProductID, &x.ProductName, &img, &published, &x.StoreID, &x.StoreName); err != nil {
				return err
			}
			x.ProductImage = imageURL(img, published)
			x.Status = stockStatus(x.Qty, x.QuantityAlert)
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

type setInventoryIn struct {
	VariantID     money.Int `json:"variantId"`
	StoreID       money.Int `json:"storeId"`
	Qty           money.Int `json:"qty"`
	QuantityAlert money.Int `json:"quantityAlert"`
}

// setInventory sets an absolute quantity (qty >= 0), writes the ledger and
// the audit log (F: no more negative or silent stock edits).
func (a *API) setInventory(w http.ResponseWriter, r *http.Request) {
	var in setInventoryIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := fieldErrs{}
	if !in.VariantID.Valid || in.VariantID.V <= 0 {
		f.add("variantId", "variantId is required")
	}
	if !in.StoreID.Valid || in.StoreID.V <= 0 {
		f.add("storeId", "storeId is required")
	}
	if !in.Qty.Valid || in.Qty.V < 0 || in.Qty.V > 1_000_000_000 {
		f.add("qty", "qty must be between 0 and 1000000000")
	}
	if in.QuantityAlert.V < 0 || in.QuantityAlert.V > 1_000_000_000 {
		f.add("quantityAlert", "quantityAlert must not be negative")
	}
	if err := f.err(); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	uid := principal(r).UserID
	var out InventoryJSON
	err := a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var ok bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM product_variants v JOIN products p ON p.id = v.product_id
			WHERE v.id = $1 AND v.deleted_at IS NULL AND p.deleted_at IS NULL)
			AND EXISTS (SELECT 1 FROM stores WHERE id = $2 AND deleted_at IS NULL)`, in.VariantID.V, in.StoreID.V).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return apperr.Invalid("variantId", "Variant or store not found.")
		}
		var before int
		if err := tx.QueryRow(r.Context(), `SELECT coalesce((SELECT qty FROM inventories WHERE store_id = $1 AND variant_id = $2), 0)`,
			in.StoreID.V, in.VariantID.V).Scan(&before); err != nil {
			return err
		}
		alert := int(in.QuantityAlert.V)
		if !in.QuantityAlert.Valid {
			if err := tx.QueryRow(r.Context(), `SELECT coalesce((SELECT quantity_alert FROM inventories WHERE store_id = $1 AND variant_id = $2), 0)`,
				in.StoreID.V, in.VariantID.V).Scan(&alert); err != nil {
				return err
			}
		}
		id, err := inventory.Set(r.Context(), tx, in.StoreID.V, in.VariantID.V, int(in.Qty.V), alert, inventory.ReasonManualSet,
			inventory.Ref{Type: "manual"}, uid)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `SELECT id, qty, quantity_alert, variant_id, store_id, created_at, updated_at FROM inventories WHERE id = $1`, id).
			Scan(&out.ID, &out.Qty, &out.QuantityAlert, &out.VariantID, &out.StoreID, &out.CreatedAt, &out.UpdatedAt); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: uid, Action: "stock.manual_set", Entity: "inventory",
			EntityID: strconv.FormatInt(id, 10), Before: map[string]any{"qty": before}, After: map[string]any{"qty": out.Qty, "storeId": out.StoreID, "variantId": out.VariantID}})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

// movementJSON is one stock ledger row.
type movementJSON struct {
	ID        int64     `json:"id"`
	StoreID   int64     `json:"storeId"`
	VariantID int64     `json:"variantId"`
	SKU       string    `json:"sku"`
	Product   string    `json:"productName"`
	Delta     int       `json:"delta"`
	QtyAfter  int       `json:"qtyAfter"`
	Reason    string    `json:"reason"`
	RefType   *string   `json:"refType"`
	RefID     *int64    `json:"refId"`
	UserID    *int64    `json:"userId"`
	CreatedAt time.Time `json:"createdAt"`
}

const (
	qMoveFilter = ` FROM stock_movements m JOIN product_variants v ON v.id = m.variant_id JOIN products p ON p.id = v.product_id
		WHERE ($1 = 0 OR m.store_id = $1) AND ($2 = 0 OR m.variant_id = $2)`
	qMoveCount = `SELECT count(*)` + qMoveFilter
	qMoveList  = `SELECT m.id, m.store_id, m.variant_id, v.sku::text, p.name, m.delta, m.qty_after, m.reason, m.ref_type, m.ref_id,
		m.user_id, m.created_at` + qMoveFilter + ` ORDER BY m.id DESC LIMIT $3 OFFSET $4`
)

// listMovements is GET /api/stock-movements (paginated ledger).
func (a *API) listMovements(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	if !p.Paged {
		p = httpx.Page{Paged: true, Page: 1, Limit: 50}
	}
	store, _ := queryInt(r, "storeId")
	variant, _ := queryInt(r, "variantId")
	var (
		out   []movementJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qMoveCount, store, variant).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qMoveList, store, variant, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m movementJSON
			if err := rows.Scan(&m.ID, &m.StoreID, &m.VariantID, &m.SKU, &m.Product, &m.Delta, &m.QtyAfter, &m.Reason,
				&m.RefType, &m.RefID, &m.UserID, &m.CreatedAt); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

// lookupStore checks a store exists (showcase handlers).
func lookupStore(r *http.Request, tx tenancy.TenantTx, id int64) error {
	var ok bool
	if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM stores WHERE id = $1 AND deleted_at IS NULL)`, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return apperr.NotFound()
	}
	return nil
}

var errNoRows = pgx.ErrNoRows

func isNoRows(err error) bool { return errors.Is(err, errNoRows) }
