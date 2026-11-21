package purchases

import (
	"context"

	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// lineUnit is the unit a line is bought in when it is not the product's
// base unit (H3, tenant migration 00037): copied on the line as it is now.
// The line's quantity and unit price stay in base units.
type lineUnit struct {
	name   string
	factor qty.Q // base units in one
	qty    qty.Q // the quantity in this unit
}

// columns are the purchase_items unit_name, unit_factor and unit_qty
// values (all null for the base unit).
func (u *lineUnit) columns() (name *string, factor, n *qty.Q) {
	if u == nil {
		return nil, nil, nil
	}
	return &u.name, &u.factor, &u.qty
}

type unitKey struct{ product, unit int64 }

const qPurchaseUnits = `SELECT pu.product_id, pu.unit_id, pu.factor, u.name::text FROM product_units pu JOIN units u ON u.id = pu.unit_id
	WHERE pu.product_id = ANY($1) AND pu.for_purchase`

// purchaseUnits loads the purchase units of the products whose lines name
// one.
func purchaseUnits(ctx context.Context, tx tenancy.TenantTx, lines []line, byID map[int64]variantRow) (map[unitKey]lineUnit, error) {
	out := map[unitKey]lineUnit{}
	var pids []int64
	for _, l := range lines {
		if v, ok := byID[l.variantID]; ok && l.unitID > 0 {
			pids = append(pids, v.productID)
		}
	}
	if len(pids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, qPurchaseUnits, pids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k unitKey
		var u lineUnit
		if err := rows.Scan(&k.product, &k.unit, &u.factor, &u.name); err != nil {
			return nil, err
		}
		out[k] = u
	}
	return out, rows.Err()
}
