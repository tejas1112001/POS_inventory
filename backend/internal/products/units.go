package products

import (
	"context"
	"fmt"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Units a product is sold or bought in besides its base unit
// (ARCHITECTURE_REVIEW H3, tenant migration 00037). Stock, the ledgers and
// line quantities stay in the base unit (products.unit_id); a unit says how
// many base units it holds.

// MaxUnits bounds a product's extra units.
const MaxUnits = 10

// maxFactor bounds a unit's size in base units.
var maxFactor = qty.Int(1_000_000)

// UnitJSON is one of a product's extra units.
type UnitJSON struct {
	ID          int64  `json:"id"`
	UnitID      int64  `json:"unitId"`
	Name        string `json:"name"`
	ShortName   string `json:"shortName"`
	Factor      qty.Q  `json:"factor"` // base units in one of this unit
	ForSale     bool   `json:"forSale"`
	ForPurchase bool   `json:"forPurchase"`
}

// unitIn is one unit in a product payload.
type unitIn struct {
	UnitID      money.Int `json:"unitId"`
	Factor      qty.In    `json:"factor"`
	ForSale     *bool     `json:"forSale"`     // default true
	ForPurchase *bool     `json:"forPurchase"` // default true
}

const (
	qLoadUnits = `SELECT pu.id, pu.product_id, pu.unit_id, u.name::text, u.short_name, pu.factor, pu.for_sale, pu.for_purchase
		FROM product_units pu JOIN units u ON u.id = pu.unit_id
		WHERE pu.product_id = ANY($1) ORDER BY pu.product_id, pu.factor, pu.id`
	qUnitBase      = `SELECT unit_id, allow_fraction FROM products WHERE id = $1`
	qUnitsExist    = `SELECT id FROM units WHERE id = ANY($1) AND deleted_at IS NULL`
	qPartFactor    = `SELECT EXISTS (SELECT 1 FROM product_units WHERE product_id = $1 AND factor <> trunc(factor))`
	qDeleteUnits   = `DELETE FROM product_units WHERE product_id = $1`
	qInsertUnit    = `INSERT INTO product_units (product_id, unit_id, factor, for_sale, for_purchase) VALUES ($1, $2, $3, $4, $5)`
	qDeleteUnitsIf = `DELETE FROM product_units WHERE product_id = $1 AND unit_id = $2`
)

// loadUnits fills each product's Units.
func loadUnits(ctx context.Context, tx tenancy.TenantTx, ids []int64, byID map[int64]*ProductJSON) error {
	for _, p := range byID {
		p.Units = []UnitJSON{}
	}
	rows, err := tx.Query(ctx, qLoadUnits, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var u UnitJSON
		var pid int64
		if err := rows.Scan(&u.ID, &pid, &u.UnitID, &u.Name, &u.ShortName, &u.Factor, &u.ForSale, &u.ForPurchase); err != nil {
			return err
		}
		if p := byID[pid]; p != nil {
			p.Units = append(p.Units, u)
		}
	}
	return rows.Err()
}

// unitsFor replaces a product's extra units (in nil keeps them) and checks
// them against the product: a base unit is set, no unit repeats or is the
// base unit, and a product sold in whole units has whole factors.
func unitsFor(ctx context.Context, tx tenancy.TenantTx, productID int64, in *[]unitIn) error {
	var base *int64
	var allow bool
	if err := tx.QueryRow(ctx, qUnitBase, productID).Scan(&base, &allow); err != nil {
		return err
	}
	if in == nil {
		if base != nil {
			// A unit that became the base unit is dropped.
			if _, err := tx.Exec(ctx, qDeleteUnitsIf, productID, *base); err != nil {
				return err
			}
		}
		if allow {
			return nil
		}
		var part bool
		if err := tx.QueryRow(ctx, qPartFactor, productID).Scan(&part); err != nil {
			return err
		}
		if part {
			return apperr.Conflict(apperr.CodeInvalidState,
				"A unit of this product holds part of a base unit. Change it before selling the product in whole units only.")
		}
		return nil
	}
	f := fieldErrs{}
	list := *in
	if len(list) > MaxUnits {
		f.add("units", fmt.Sprintf("At most %d units.", MaxUnits))
		return f.err()
	}
	if len(list) > 0 && base == nil {
		f.add("unitId", "Choose the product's base unit before adding other units.")
		return f.err()
	}
	ids := make([]int64, 0, len(list))
	seen := map[int64]bool{}
	for i, u := range list {
		k := fmt.Sprintf("units[%d]", i)
		switch {
		case !u.UnitID.Valid || u.UnitID.V <= 0:
			f.add(k+".unitId", "unitId is required")
		case base != nil && u.UnitID.V == *base:
			f.add(k+".unitId", "This is the product's base unit.")
		case seen[u.UnitID.V]:
			f.add(k+".unitId", "The same unit is listed twice.")
		}
		seen[u.UnitID.V] = true
		ids = append(ids, u.UnitID.V)
		switch {
		case !u.Factor.Valid || !u.Factor.V.IsPositive() || u.Factor.V.Greater(maxFactor):
			f.add(k+".factor", "How many base units one holds: a number above 0, at most 3 decimals.")
		case !allow && !u.Factor.V.IsWhole():
			f.add(k+".factor", "A product sold in whole units needs a whole number of base units per unit.")
		}
		if u.ForSale != nil && u.ForPurchase != nil && !*u.ForSale && !*u.ForPurchase {
			f.add(k+".forSale", "Use the unit for sale, for purchase or both.")
		}
	}
	if err := f.err(); err != nil {
		return err
	}
	known := map[int64]bool{}
	rows, err := tx.Query(ctx, qUnitsExist, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		known[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i, u := range list {
		if !known[u.UnitID.V] {
			f.add(fmt.Sprintf("units[%d].unitId", i), "Unit not found.")
		}
	}
	if err := f.err(); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qDeleteUnits, productID); err != nil {
		return err
	}
	for _, u := range list {
		sale, buy := u.ForSale == nil || *u.ForSale, u.ForPurchase == nil || *u.ForPurchase
		if _, err := tx.Exec(ctx, qInsertUnit, productID, u.UnitID.V, u.Factor.V, sale, buy); err != nil {
			return err
		}
	}
	return nil
}

// saleUnits are the units a product is sold in (the POS list).
func saleUnits(us []UnitJSON) []UnitJSON {
	var out []UnitJSON
	for _, u := range us {
		if u.ForSale {
			out = append(out, u)
		}
	}
	return out
}
