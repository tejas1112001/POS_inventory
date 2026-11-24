package products

import (
	"context"
	"net/http"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/pricing"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Strict MRP for prices (pharmacy compliance plan §6.2): a variant's price
// and its price in each store, with tax, may not be above its MRP (printed
// for mrp_pack base units). Checked when a price, an MRP or its pack is
// saved; the sale checks every line again (sales.checkMRP), so a price
// saved before this rule cannot be sold above MRP either.

// Which prices refuseAboveMRP checks.
const (
	checkVariantPrice = 1 << iota
	checkStorePrices
)

// maxMRPPack bounds mrp_pack (numeric(14,3)).
var maxMRPPack = decimal.NewFromInt(1_000_000)

const qPriceMRP = `SELECT v.sku::text, v.price, v.mrp, v.mrp_pack, coalesce(p.tax_rate, 0),
		coalesce(p.tax_type = 'Inclusive', s.prices_include_tax_default, false), st.name, sp.price
	FROM product_variants v JOIN products p ON p.id = v.product_id
	JOIN settings s ON s.id = 1
	LEFT JOIN store_prices sp ON sp.variant_id = v.id
	LEFT JOIN stores st ON st.id = sp.store_id AND st.deleted_at IS NULL
	WHERE v.id = ANY($1) AND v.deleted_at IS NULL AND v.mrp IS NOT NULL
	ORDER BY v.id, sp.store_id`

// refuseAboveMRP returns 409 ABOVE_MRP naming the first price of the given
// variants that is above its MRP.
func refuseAboveMRP(ctx context.Context, tx tenancy.TenantTx, variantIDs []int64, what int) error {
	if len(variantIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, qPriceMRP, variantIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sku string
		var price, mrp, pack, rate decimal.Decimal
		var inclusive bool
		var store *string
		var storePrice *decimal.Decimal
		if err := rows.Scan(&sku, &price, &mrp, &pack, &rate, &inclusive, &store, &storePrice); err != nil {
			return err
		}
		if what&checkVariantPrice != 0 && pricing.AboveMRP(price, rate, inclusive, &mrp, pack) {
			return aboveMRPError(sku, "", price, rate, inclusive, mrp, pack)
		}
		if what&checkStorePrices != 0 && store != nil && storePrice != nil && pricing.AboveMRP(*storePrice, rate, inclusive, &mrp, pack) {
			return aboveMRPError(sku, *store, *storePrice, rate, inclusive, mrp, pack)
		}
	}
	return rows.Err()
}

func aboveMRPError(sku, store string, price, rate decimal.Decimal, inclusive bool, mrp, pack decimal.Decimal) error {
	where := sku
	if store != "" {
		where += " in " + store
	}
	per := ""
	if !pack.Equal(decimal.NewFromInt(1)) {
		per = " for " + pack.String() + " units"
	}
	withTax := pricing.TaxInclusive(price, rate, inclusive)
	msg := where + ": the price with tax (" + withTax.Round(2).StringFixed(2) + " a unit) is above the MRP (" +
		mrp.StringFixed(2) + per + "). Lower the price or correct the MRP."
	return apperr.Conflict(apperr.CodeAboveMRP, msg).WithDetails(map[string]any{
		"sku": sku, "store": store, "price": price, "priceWithTax": withTax.Round(2), "mrp": mrp, "mrpPack": pack})
}

// AboveMRPRow is one price the strict MRP rule refuses (GET
// /api/products/above-mrp): the report the owner works through before the
// rule applies to old prices (pharmacy plan §6.2).
type AboveMRPRow struct {
	// Kind: price (the variant's price), store_price, batch (a batch in
	// stock whose MRP is below the price it sells at), no_mrp (a pharmacy
	// medicine in stock with no MRP at all).
	Kind         string           `json:"kind"`
	ProductID    int64            `json:"productId"`
	ProductName  string           `json:"productName"`
	VariantID    int64            `json:"variantId"`
	SKU          string           `json:"sku"`
	Store        *string          `json:"store"`
	BatchNo      *string          `json:"batchNo"`
	Price        decimal.Decimal  `json:"price"`
	PriceWithTax decimal.Decimal  `json:"priceWithTax"`
	MRP          *decimal.Decimal `json:"mrp"`
	MRPPack      *decimal.Decimal `json:"mrpPack"`
}

// AboveMRPJSON is the report. Truncated: more rows than maxAboveMRPRows.
type AboveMRPJSON struct {
	Rows      []AboveMRPRow `json:"rows"`
	Truncated bool          `json:"truncated"`
}

const maxAboveMRPRows = 1000

// qAboveMRP lists every price with an MRP to compare it with: the
// variant's own, each store price, and each batch in stock at the price
// of the store holding it; and, in a pharmacy, batch-tracked variants in
// stock with no MRP anywhere. Go applies pricing.AboveMRP.
const qAboveMRP = `WITH base AS (
		SELECT v.id AS vid, p.id AS pid, p.name, v.sku::text AS sku, v.price, v.mrp, v.mrp_pack, coalesce(p.tax_rate, 0) AS rate,
			coalesce(p.tax_type = 'Inclusive', s.prices_include_tax_default, false) AS incl, p.tracks_batches,
			s.business_type = 'pharmacy' AS pharmacy
		FROM product_variants v JOIN products p ON p.id = v.product_id AND p.deleted_at IS NULL
		JOIN settings s ON s.id = 1
		WHERE v.deleted_at IS NULL)
	SELECT 'price', b.pid, b.name, b.vid, b.sku, NULL::text, NULL::text, b.price, b.mrp, b.mrp_pack, b.rate, b.incl
		FROM base b WHERE b.mrp IS NOT NULL
	UNION ALL
	SELECT 'store_price', b.pid, b.name, b.vid, b.sku, st.name::text, NULL, sp.price, b.mrp, b.mrp_pack, b.rate, b.incl
		FROM base b JOIN store_prices sp ON sp.variant_id = b.vid JOIN stores st ON st.id = sp.store_id AND st.deleted_at IS NULL
		WHERE b.mrp IS NOT NULL
	UNION ALL
	SELECT 'batch', b.pid, b.name, b.vid, b.sku, st.name::text, bt.batch_no::text, coalesce(sp.price, b.price), bt.mrp, bt.mrp_pack, b.rate, b.incl
		FROM base b JOIN batches bt ON bt.variant_id = b.vid AND bt.mrp IS NOT NULL
		JOIN batch_stock bs ON bs.batch_id = bt.id AND bs.qty > 0
		JOIN stores st ON st.id = bs.store_id AND st.deleted_at IS NULL
		LEFT JOIN store_prices sp ON sp.variant_id = b.vid AND sp.store_id = bs.store_id
	UNION ALL
	SELECT 'no_mrp', b.pid, b.name, b.vid, b.sku, st.name::text, bt.batch_no::text, coalesce(sp.price, b.price), NULL, NULL, b.rate, b.incl
		FROM base b JOIN batches bt ON bt.variant_id = b.vid AND bt.mrp IS NULL
		JOIN batch_stock bs ON bs.batch_id = bt.id AND bs.qty > 0
		JOIN stores st ON st.id = bs.store_id AND st.deleted_at IS NULL
		LEFT JOIN store_prices sp ON sp.variant_id = b.vid AND sp.store_id = bs.store_id
		WHERE b.pharmacy AND b.tracks_batches AND b.mrp IS NULL
	ORDER BY 3, 5, 1, 6 NULLS FIRST, 7 NULLS FIRST
	LIMIT 50000`

// aboveMRP is GET /api/products/above-mrp.
func (a *API) aboveMRP(w http.ResponseWriter, r *http.Request) {
	out := AboveMRPJSON{Rows: []AboveMRPRow{}}
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		rows, err := tx.Query(r.Context(), qAboveMRP)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x AboveMRPRow
			var rate decimal.Decimal
			var incl bool
			if err := rows.Scan(&x.Kind, &x.ProductID, &x.ProductName, &x.VariantID, &x.SKU, &x.Store, &x.BatchNo, &x.Price,
				&x.MRP, &x.MRPPack, &rate, &incl); err != nil {
				return err
			}
			if x.Kind != "no_mrp" && !pricing.AboveMRP(x.Price, rate, incl, x.MRP, *x.MRPPack) {
				continue
			}
			if len(out.Rows) == maxAboveMRPRows {
				out.Truncated = true
				break
			}
			x.PriceWithTax = pricing.TaxInclusive(x.Price, rate, incl).Round(2)
			out.Rows = append(out.Rows, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
