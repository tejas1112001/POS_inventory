package sales

import (
	"context"

	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// lineUnit is the unit a line is sold in when it is not the product's base
// unit (H3, tenant migration 00037): copied on the line as it is now.
type lineUnit struct {
	id     int64 // units.id (order_items.unit_id, 00038)
	name   string
	factor qty.Q // base units in one
	qty    qty.Q // the quantity in this unit
}

// columns are the order_items unit_name, unit_factor and unit_qty values
// (all null for the base unit).
func (u *lineUnit) columns() (name *string, factor, n *qty.Q) {
	if u == nil {
		return nil, nil, nil
	}
	return &u.name, &u.factor, &u.qty
}

// unitID is the order_items.unit_id value (null for the base unit).
func (u *lineUnit) unitID() *int64 {
	if u == nil {
		return nil
	}
	return &u.id
}

type unitKey struct{ product, unit int64 }

const qSaleUnits = `SELECT pu.product_id, pu.unit_id, pu.factor, u.name::text FROM product_units pu JOIN units u ON u.id = pu.unit_id
	WHERE pu.product_id = ANY($1) AND pu.for_sale`

// saleUnits loads the sale units of the products whose lines name one.
func saleUnits(ctx context.Context, tx tenancy.TenantTx, lines []line, byID map[int64]variantRow) (map[unitKey]lineUnit, error) {
	var pids []int64
	for _, l := range lines {
		if v, ok := byID[l.variantID]; ok && l.unitID > 0 {
			pids = append(pids, v.productID)
		}
	}
	return unitsOf(ctx, tx, pids)
}

// unitsOf loads the sale units of the products.
func unitsOf(ctx context.Context, tx tenancy.TenantTx, pids []int64) (map[unitKey]lineUnit, error) {
	out := map[unitKey]lineUnit{}
	if len(pids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, qSaleUnits, pids)
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
		u.id = k.unit
		out[k] = u
	}
	return out, rows.Err()
}

// inUnits is a base quantity in units of factor f, to three decimals: the
// display copy on a return row (the base quantity is the record). False
// when it rounds to nothing.
func inUnits(q, f qty.Q) (qty.Q, bool) {
	n, err := qty.FromDecimal(q.Decimal().DivRound(f.Decimal(), qty.Scale))
	if err != nil || !n.IsPositive() {
		return qty.Zero, false
	}
	return n, true
}

// copyIn is the unit copy of a return row of base quantity q, from the
// unit name and factor (nil when the row is in base units).
func copyIn(name *string, factor *qty.Q, q qty.Q) *lineUnit {
	if name == nil || factor == nil {
		return nil
	}
	n, ok := inUnits(q, *factor)
	if !ok {
		return nil
	}
	return &lineUnit{name: *name, factor: *factor, qty: n}
}
