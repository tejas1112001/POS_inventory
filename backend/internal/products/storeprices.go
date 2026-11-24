package products

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Price per store (tenant migration 00042): a variant may sell at its own
// price in a store; without one it sells at its variant price. Checkout,
// the quote, the POS list, the product lock and the online shop of that
// store read it; cost and MRP stay per variant.

// maxStorePriceLines bounds one request.
const maxStorePriceLines = 500

type storePriceIn struct {
	VariantID money.Int `json:"variantId"`
	StoreID   money.Int `json:"storeId"`
	// Price: the store's price; null removes it (the variant price applies).
	Price money.Dec `json:"price"`
}

type storePricesIn struct {
	Prices []storePriceIn `json:"prices"`
}

const (
	qStorePriceSet = `INSERT INTO store_prices (store_id, variant_id, price, updated_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (store_id, variant_id) DO UPDATE SET price = excluded.price, updated_by = excluded.updated_by, updated_at = now()
		RETURNING (SELECT price FROM store_prices WHERE store_id = $1 AND variant_id = $2)`
	qStorePriceDel = `DELETE FROM store_prices WHERE store_id = $1 AND variant_id = $2 RETURNING price`
)

// putStorePrices is PUT /api/products/{id}/store-prices: sets or removes
// the prices of the product's variants in stores. It returns the product.
func (a *API) putStorePrices(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in storePricesIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := map[string]string{}
	switch {
	case len(in.Prices) == 0:
		f["prices"] = "At least one price is required."
	case len(in.Prices) > maxStorePriceLines:
		f["prices"] = fmt.Sprintf("At most %d prices at a time.", maxStorePriceLines)
	}
	for i, p := range in.Prices {
		k := fmt.Sprintf("prices[%d]", i)
		if !p.VariantID.Valid || p.VariantID.V <= 0 {
			f[k+".variantId"] = "variantId is required."
		}
		if !p.StoreID.Valid || p.StoreID.V <= 0 {
			f[k+".storeId"] = "storeId is required."
		}
		if p.Price.Valid && (p.Price.D.IsNegative() || p.Price.D.GreaterThan(maxPrice)) {
			f[k+".price"] = "Price must be a number of at least 0."
		}
	}
	if err := apperr.Fields(f); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	var out ProductJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		variants := map[int64]bool{}
		rows, err := tx.Query(ctx, `SELECT v.id FROM product_variants v JOIN products p ON p.id = v.product_id
			WHERE v.product_id = $1 AND v.deleted_at IS NULL AND p.deleted_at IS NULL`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var vid int64
			if err := rows.Scan(&vid); err != nil {
				rows.Close()
				return err
			}
			variants[vid] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(variants) == 0 {
			return apperr.NotFound()
		}
		stores := map[int64]bool{}
		rows, err = tx.Query(ctx, `SELECT id FROM stores WHERE deleted_at IS NULL`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var sid int64
			if err := rows.Scan(&sid); err != nil {
				rows.Close()
				return err
			}
			stores[sid] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i, sp := range in.Prices {
			k := fmt.Sprintf("prices[%d]", i)
			if !variants[sp.VariantID.V] {
				f[k+".variantId"] = "Not a variant of this product."
			}
			if !stores[sp.StoreID.V] {
				f[k+".storeId"] = "Store not found."
			}
		}
		if err := apperr.Fields(f); err != nil {
			return err
		}
		for _, sp := range in.Prices {
			var before *decimal.Decimal
			var after any
			if sp.Price.Valid {
				price := sp.Price.D.Round(4)
				var old *decimal.Decimal
				// The subquery in RETURNING sees the row as it was.
				if err := tx.QueryRow(ctx, qStorePriceSet, sp.StoreID.V, sp.VariantID.V, price, p.UserID).Scan(&old); err != nil {
					return err
				}
				if old != nil && old.Equal(price) {
					continue
				}
				before, after = old, price
			} else {
				var old decimal.Decimal
				err := tx.QueryRow(ctx, qStorePriceDel, sp.StoreID.V, sp.VariantID.V).Scan(&old)
				if isNoRows(err) {
					continue
				}
				if err != nil {
					return err
				}
				before, after = &old, nil
			}
			if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "product.store_price", Entity: "variant",
				EntityID: strconv.FormatInt(sp.VariantID.V, 10), RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent(),
				Before: map[string]any{"store_id": sp.StoreID.V, "price": before}, After: map[string]any{"store_id": sp.StoreID.V, "price": after}}); err != nil {
				return err
			}
		}
		changed := make([]int64, 0, len(in.Prices))
		for _, sp := range in.Prices {
			if sp.Price.Valid {
				changed = append(changed, sp.VariantID.V)
			}
		}
		if err := refuseAboveMRP(ctx, tx, changed, checkStorePrices); err != nil {
			return err
		}
		var ok bool
		out, ok, err = loadOne(ctx, tx, id, loadOpts{})
		if err == nil && !ok {
			err = apperr.NotFound()
		}
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
