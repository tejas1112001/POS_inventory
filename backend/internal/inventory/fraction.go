package inventory

import (
	"context"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Fractional quantities (ARCHITECTURE_REVIEW H3). Every quantity column
// holds three decimals, but only a product with allow_fraction (sold by
// weight, length or volume) may use them; the rest stay whole numbers.

// WholeOnlyMessage is the error for a fraction of a whole-unit product.
const WholeOnlyMessage = "This product is sold in whole units only."

const qFractional = `SELECT v.id, p.allow_fraction FROM product_variants v JOIN products p ON p.id = v.product_id WHERE v.id = ANY($1)`

// Fractional returns, for each variant, whether its product allows
// fractional quantities (unknown variants are absent).
func Fractional(ctx context.Context, tx tenancy.TenantTx, variantIDs []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, qFractional, variantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var f bool
		if err := rows.Scan(&id, &f); err != nil {
			return nil, err
		}
		out[id] = f
	}
	return out, rows.Err()
}

// AllowedQty reports whether q may be used for a variant: whole, or the
// product allows fractions.
func AllowedQty(q qty.Q, fractional bool) bool { return fractional || q.IsWhole() }

// CheckWhole returns the first variant whose quantity is a fraction its
// product does not allow (0 when all are fine).
func CheckWhole(ctx context.Context, tx tenancy.TenantTx, qs map[int64]qty.Q) (int64, error) {
	var ids []int64
	for v, q := range qs {
		if !q.IsWhole() {
			ids = append(ids, v)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	frac, err := Fractional(ctx, tx, ids)
	if err != nil {
		return 0, err
	}
	for _, v := range ids {
		if !frac[v] {
			return v, nil
		}
	}
	return 0, nil
}

// RequireAllowed returns a validation error on field when q is a fraction
// the variant's product does not allow.
func RequireAllowed(ctx context.Context, tx tenancy.TenantTx, field string, variantID int64, q qty.Q) error {
	v, err := CheckWhole(ctx, tx, map[int64]qty.Q{variantID: q})
	if err != nil || v == 0 {
		return err
	}
	return apperr.Invalid(field, WholeOnlyMessage)
}
