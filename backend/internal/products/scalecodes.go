package products

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/scale"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// The variant of a scale code, with its price at the store and whether it
// sells in part units.
const qScaleVariant = `SELECT v.product_id, v.id, coalesce(sp.price, v.price), p.allow_fraction, p.name
	FROM product_variants v JOIN products p ON p.id = v.product_id
	LEFT JOIN store_prices sp ON sp.variant_id = v.id AND sp.store_id = $2
	WHERE v.scale_code = $1 AND v.deleted_at IS NULL AND p.deleted_at IS NULL AND p.status = 'Active'`

// scaleLookup reads code as a weighing scale's label with the shop's rules
// (00044). It fills out's variant and quantity and returns the product
// ID, or 0 when code is not one (or no product has its scale code).
func scaleLookup(ctx context.Context, tx tenancy.TenantTx, code string, storeID int64, out *barcodeJSON) (int64, error) {
	var rules []scale.Rule
	if err := tx.QueryRow(ctx, `SELECT scale_barcodes FROM settings WHERE id = 1`).Scan(&rules); err != nil {
		return 0, err
	}
	rd, ok := scale.Parse(code, rules)
	if !ok {
		return 0, nil
	}
	var (
		pid        int64
		price      decimal.Decimal
		fractional bool
		name       string
	)
	err := tx.QueryRow(ctx, qScaleVariant, rd.Item, storeID).Scan(&pid, &out.VariantID, &price, &fractional, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	amount := rd.Value
	if rd.Rule.Value == scale.ValuePrice {
		// The label's price at the store's price per base unit, to three
		// decimals (the line comes to within a paisa of the label).
		if !price.IsPositive() {
			return 0, apperr.Conflict(apperr.CodeInvalidState, fmt.Sprintf("%s has no price, so a price label cannot be read as a quantity.", name))
		}
		amount = rd.Value.DivRound(price, qty.Scale)
	}
	q, err := qty.FromDecimal(amount.Round(qty.Scale))
	if err != nil || !q.IsPositive() {
		return 0, apperr.Invalid("barcode", "The scale label's quantity is not usable.")
	}
	if !inventory.AllowedQty(q, fractional) {
		return 0, apperr.Conflict(apperr.CodeInvalidState,
			fmt.Sprintf("%s is sold in whole units only. Turn on part units for it to sell it by weight.", name))
	}
	value := rd.Value
	out.Quantity, out.Scale, out.ScaleValue = &q, rd.Rule.Value, &value
	return pid, nil
}
