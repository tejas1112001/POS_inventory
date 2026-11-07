package products

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// showcaseProduct is one row of the showcase admin page (old shape).
type showcaseProduct struct {
	ID          int64           `json:"id"`
	Name        string          `json:"name"`
	Category    string          `json:"category"`
	Brand       string          `json:"brand"`
	Price       decimal.Decimal `json:"price"`
	Image       string          `json:"image"`
	IsPublished bool            `json:"isPublished"`
	IsFeatured  bool            `json:"isFeatured"`
}

const qShowcaseProducts = `SELECT p.id, p.name, coalesce(c.name::text, ''), coalesce(b.name::text, ''),
	coalesce((SELECT v.price FROM product_variants v WHERE v.product_id = p.id AND v.deleted_at IS NULL ORDER BY v.id LIMIT 1), 0),
	p.image_file_id, coalesce(sp.is_published, false), coalesce(sp.is_featured, false),
	EXISTS (SELECT 1 FROM store_products x WHERE x.product_id = p.id AND x.is_published)
	FROM products p
	LEFT JOIN categories c ON c.id = p.category_id
	LEFT JOIN brands b ON b.id = p.brand_id
	LEFT JOIN store_products sp ON sp.product_id = p.id AND sp.store_id = $1
	WHERE p.deleted_at IS NULL AND p.status = 'Active'
	ORDER BY p.name, p.id LIMIT $2`

// ShowcaseProducts lists products with their publication state in one
// store (catalog's GET /api/stores/{id}/showcase).
func ShowcaseProducts(maxRows int) func(ctx context.Context, tx tenancy.TenantTx, storeID int64) (any, error) {
	return func(ctx context.Context, tx tenancy.TenantTx, storeID int64) (any, error) {
		rows, err := tx.Query(ctx, qShowcaseProducts, storeID, maxRows)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []showcaseProduct{}
		for rows.Next() {
			var p showcaseProduct
			var img *imageID
			var anyPublished bool
			if err := rows.Scan(&p.ID, &p.Name, &p.Category, &p.Brand, &p.Price, &img, &p.IsPublished, &p.IsFeatured, &anyPublished); err != nil {
				return nil, err
			}
			if u := imageURL(img, anyPublished); u != nil {
				p.Image = *u
			}
			out = append(out, p)
		}
		return out, rows.Err()
	}
}

type toggleIn struct {
	ProductID money.Int `json:"productId"`
	Field     string    `json:"field"`
	Value     bool      `json:"value"`
}

type storeProductJSON struct {
	StoreID     int64     `json:"store_id"`
	ProductID   int64     `json:"product_id"`
	IsPublished bool      `json:"isPublished"`
	IsFeatured  bool      `json:"isFeatured"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Publication is per store only; the old global products.is_published side
// effect is gone (P1-16).
const qToggle = `INSERT INTO store_products (store_id, product_id, is_published, is_featured)
	SELECT $1, p.id, ($3 = 'isPublished' AND $4), ($3 = 'isFeatured' AND $4)
	FROM products p WHERE p.id = $2 AND p.deleted_at IS NULL
	ON CONFLICT (store_id, product_id) DO UPDATE SET
	  is_published = CASE WHEN $3 = 'isPublished' THEN $4 ELSE store_products.is_published END,
	  is_featured  = CASE WHEN $3 = 'isFeatured'  THEN $4 ELSE store_products.is_featured END,
	  updated_at = now()
	RETURNING store_id, product_id, is_published, is_featured, updated_at`

func (a *API) toggleShowcase(w http.ResponseWriter, r *http.Request) {
	storeID, err := pathID(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in toggleIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !in.ProductID.Valid || (in.Field != "isPublished" && in.Field != "isFeatured") {
		httpx.WriteError(w, r, apperr.Invalid("field", "Invalid productId or field name."))
		return
	}
	var sp storeProductJSON
	uid := principal(r).UserID
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := lookupStore(r, tx, storeID); err != nil {
			return err
		}
		err := tx.QueryRow(r.Context(), qToggle, storeID, in.ProductID.V, in.Field, in.Value).
			Scan(&sp.StoreID, &sp.ProductID, &sp.IsPublished, &sp.IsFeatured, &sp.UpdatedAt)
		if isNoRows(err) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: uid, Action: "showcase.publish", Entity: "store_product",
			EntityID: strconv.FormatInt(storeID, 10) + ":" + strconv.FormatInt(in.ProductID.V, 10),
			After:    map[string]any{in.Field: in.Value}})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"message": "Product showcase settings updated.", "storeProduct": sp})
}

type batchIn struct {
	PublishAll *bool   `json:"publishAll"`
	ProductIDs []int64 `json:"productIds"`
	Action     string  `json:"action"`
}

const (
	qPublishAll = `INSERT INTO store_products (store_id, product_id, is_published)
		SELECT $1, id, $2 FROM products WHERE deleted_at IS NULL AND status = 'Active'
		ON CONFLICT (store_id, product_id) DO UPDATE SET is_published = EXCLUDED.is_published, updated_at = now()`
	qPublishSome = `INSERT INTO store_products (store_id, product_id, is_published)
		SELECT $1, id, $2 FROM products WHERE deleted_at IS NULL AND id = ANY($3)
		ON CONFLICT (store_id, product_id) DO UPDATE SET is_published = EXCLUDED.is_published, updated_at = now()`
)

// batchPublish replaces the old N+1 upserts with one statement.
func (a *API) batchPublish(w http.ResponseWriter, r *http.Request) {
	storeID, err := pathID(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in batchIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if in.PublishAll == nil && (in.ProductIDs == nil || (in.Action != "publish" && in.Action != "unpublish")) {
		httpx.WriteError(w, r, apperr.Invalid("productIds", "Invalid batch request parameters."))
		return
	}
	if len(in.ProductIDs) > 5000 {
		httpx.WriteError(w, r, apperr.Invalid("productIds", "At most 5000 products per request."))
		return
	}
	uid := principal(r).UserID
	msg := "Selected products updated successfully."
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := lookupStore(r, tx, storeID); err != nil {
			return err
		}
		var after map[string]any
		if in.PublishAll != nil {
			if _, err := tx.Exec(r.Context(), qPublishAll, storeID, *in.PublishAll); err != nil {
				return err
			}
			msg = "All products unpublished successfully."
			if *in.PublishAll {
				msg = "All products published successfully."
			}
			after = map[string]any{"publishAll": *in.PublishAll}
		} else {
			if _, err := tx.Exec(r.Context(), qPublishSome, storeID, in.Action == "publish", in.ProductIDs); err != nil {
				return err
			}
			after = map[string]any{"action": in.Action, "count": len(in.ProductIDs)}
		}
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: uid, Action: "showcase.batch_publish", Entity: "store",
			EntityID: strconv.FormatInt(storeID, 10), After: after})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"message": msg})
}
