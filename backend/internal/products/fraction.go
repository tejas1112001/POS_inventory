package products

import (
	"context"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// maxStock bounds a typed stock quantity or low-stock level.
var maxStock = qty.Int(1_000_000_000)

const (
	// qPartStock: the product holds stock (or a low-stock level) in part
	// units, on either ledger.
	qPartStock = `SELECT EXISTS (SELECT 1 FROM inventories i JOIN product_variants v ON v.id = i.variant_id
			WHERE v.product_id = $1 AND (i.qty <> trunc(i.qty) OR i.quantity_alert <> trunc(i.quantity_alert)))
		OR EXISTS (SELECT 1 FROM batch_stock s JOIN batches b ON b.id = s.batch_id JOIN product_variants v ON v.id = b.variant_id
			WHERE v.product_id = $1 AND (s.qty <> trunc(s.qty) OR s.quarantined_qty <> trunc(s.quarantined_qty)))`
	qAllowFraction = `SELECT allow_fraction FROM products WHERE id = $1`
)

// fractionFor applies a product's "sold in part units" choice (H3) and
// then checks its stock against it: whole units only unless the product
// allows fractions. Turning fractions off while some stock is in part
// units is refused. set nil keeps the product's choice.
func fractionFor(ctx context.Context, tx tenancy.TenantTx, productID int64, set *bool) error {
	var allow bool
	if err := tx.QueryRow(ctx, qAllowFraction, productID).Scan(&allow); err != nil {
		return err
	}
	if set != nil && *set != allow {
		if !*set {
			var part bool
			if err := tx.QueryRow(ctx, qPartStock, productID).Scan(&part); err != nil {
				return err
			}
			if part {
				return apperr.Conflict(apperr.CodeInvalidState,
					"Some stock of this product is in part units. Count it to whole units before selling it in whole units only.")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE products SET allow_fraction = $2, updated_at = now() WHERE id = $1`, productID, *set); err != nil {
			return err
		}
		allow = *set
	}
	if allow {
		return nil
	}
	var part bool
	if err := tx.QueryRow(ctx, qPartStock, productID).Scan(&part); err != nil {
		return err
	}
	if part {
		return apperr.Invalid("variants", inventory.WholeOnlyMessage+" Turn on allowFraction to stock part units.")
	}
	return nil
}
