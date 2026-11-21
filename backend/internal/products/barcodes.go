package products

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Several barcodes per variant (tenant migration 00043, review L6). The
// main code stays product_variants.item_barcode (labels print it); a
// variant may have other codes, each optionally naming one of the
// product's units (a case code: scanning it counts one case).

// scaleCodePattern is a scale code (PLU): 4 to 6 digits.
var scaleCodePattern = regexp.MustCompile(`^[0-9]{4,6}$`)

// MaxBarcodesPerProduct bounds a product's extra codes.
const MaxBarcodesPerProduct = 50

// VariantBarcodeJSON is one extra code of a variant.
type VariantBarcodeJSON struct {
	Code     string  `json:"code"`
	UnitID   *int64  `json:"unitId"`
	UnitName *string `json:"unitName"`
	Factor   *qty.Q  `json:"factor"` // base units in the code's unit
}

const qLoadBarcodes = `SELECT vb.variant_id, vb.code, vb.unit_id, u.name::text, pu.factor
	FROM variant_barcodes vb
	JOIN product_variants v ON v.id = vb.variant_id
	LEFT JOIN units u ON u.id = vb.unit_id
	LEFT JOIN product_units pu ON pu.product_id = v.product_id AND pu.unit_id = vb.unit_id
	WHERE vb.variant_id = ANY($1) ORDER BY vb.id`

// loadBarcodes fills each variant's Barcodes.
func loadBarcodes(ctx context.Context, tx tenancy.TenantTx, variantIDs []int64, owner map[int64]*ProductJSON) error {
	if len(variantIDs) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, qLoadBarcodes, variantIDs)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var vid int64
		var b VariantBarcodeJSON
		if err := rows.Scan(&vid, &b.Code, &b.UnitID, &b.UnitName, &b.Factor); err != nil {
			return err
		}
		p := owner[vid]
		if p == nil {
			continue
		}
		for i := range p.ProductVariants {
			if p.ProductVariants[i].ID == vid {
				p.ProductVariants[i].Barcodes = append(p.ProductVariants[i].Barcodes, b)
			}
		}
	}
	return rows.Err()
}

// extraCodeTaken refuses a main barcode that is another variant's extra
// code (the other direction of the uniqueness rule).
func extraCodeTaken(ctx context.Context, tx tenancy.TenantTx, code *string) error {
	if code == nil || *code == "" {
		return nil
	}
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM variant_barcodes WHERE code = $1)`, *code).Scan(&taken); err != nil {
		return err
	}
	if taken {
		return apperr.Conflict(apperr.CodeConflict, "A product with this barcode already exists.")
	}
	return nil
}

type barcodeIn struct {
	VariantID money.Int `json:"variantId"`
	Code      string    `json:"code"`
	UnitID    money.Int `json:"unitId"` // optional: one of the product's units
}

type scaleCodeIn struct {
	VariantID money.Int `json:"variantId"`
	ScaleCode *string   `json:"scaleCode"` // null or "": none
}

type barcodesIn struct {
	Barcodes []barcodeIn `json:"barcodes"` // replaces all; missing keeps them
	// ScaleCodes sets variants' scale codes (00044); missing keeps them.
	ScaleCodes []scaleCodeIn `json:"scaleCodes"`
}

// putBarcodes is PUT /api/products/{id}/barcodes: replaces the product's
// extra codes. It returns the product.
func (a *API) putBarcodes(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in barcodesIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := map[string]string{}
	if len(in.Barcodes) > MaxBarcodesPerProduct {
		f["barcodes"] = fmt.Sprintf("At most %d extra barcodes per product.", MaxBarcodesPerProduct)
	}
	seen := map[string]int{}
	for i := range in.Barcodes {
		b := &in.Barcodes[i]
		k := fmt.Sprintf("barcodes[%d]", i)
		b.Code = strings.TrimSpace(b.Code)
		switch prev, dup := seen[b.Code]; {
		case !barcodePattern.MatchString(b.Code):
			f[k+".code"] = "A barcode may use letters, digits and . _ / + - (at most 64)."
		case dup:
			f[k+".code"] = fmt.Sprintf("The same code as barcode %d.", prev+1)
		}
		seen[b.Code] = i
		if !b.VariantID.Valid || b.VariantID.V <= 0 {
			f[k+".variantId"] = "variantId is required."
		}
		if b.UnitID.Valid && b.UnitID.V <= 0 {
			f[k+".unitId"] = "unitId is invalid."
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
		variants, units := map[int64]bool{}, map[int64]bool{}
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
		rows, err = tx.Query(ctx, `SELECT unit_id FROM product_units WHERE product_id = $1`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var uid int64
			if err := rows.Scan(&uid); err != nil {
				rows.Close()
				return err
			}
			units[uid] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		var before []string
		if err := tx.QueryRow(ctx, `SELECT coalesce(array_agg(vb.code ORDER BY vb.id), '{}') FROM variant_barcodes vb
			JOIN product_variants v ON v.id = vb.variant_id WHERE v.product_id = $1`, id).Scan(&before); err != nil {
			return err
		}
		// Missing barcodes keep them (a request with only scaleCodes); [] clears.
		if in.Barcodes != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM variant_barcodes vb USING product_variants v
				WHERE v.id = vb.variant_id AND v.product_id = $1`, id); err != nil {
				return err
			}
		}
		after := make([]string, 0, len(in.Barcodes))
		if in.Barcodes == nil {
			after = append(after, before...) // kept
		}
		for i, b := range in.Barcodes {
			k := fmt.Sprintf("barcodes[%d]", i)
			if !variants[b.VariantID.V] {
				f[k+".variantId"] = "Not a variant of this product."
				continue
			}
			if b.UnitID.Valid && !units[b.UnitID.V] {
				f[k+".unitId"] = "Not one of this product's units (add it under Other units first)."
				continue
			}
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM product_variants WHERE item_barcode = $1 AND deleted_at IS NULL)
				OR EXISTS (SELECT 1 FROM variant_barcodes WHERE code = $1)`, b.Code).Scan(&taken); err != nil {
				return err
			}
			if taken {
				f[k+".code"] = "A product already has this barcode."
				continue
			}
			var unit *int64
			if b.UnitID.Valid {
				unit = &b.UnitID.V
			}
			if _, err := tx.Exec(ctx, `INSERT INTO variant_barcodes (variant_id, code, unit_id) VALUES ($1, $2, $3)`,
				b.VariantID.V, b.Code, unit); err != nil {
				return err
			}
			after = append(after, b.Code)
		}
		for i, sc := range in.ScaleCodes {
			k := fmt.Sprintf("scaleCodes[%d]", i)
			if !variants[sc.VariantID.V] {
				f[k+".variantId"] = "Not a variant of this product."
				continue
			}
			code := ""
			if sc.ScaleCode != nil {
				code = strings.TrimSpace(*sc.ScaleCode)
			}
			if code != "" && !scaleCodePattern.MatchString(code) {
				f[k+".scaleCode"] = "A scale code is the 4 to 6 digits the scale prints."
				continue
			}
			if code != "" {
				var taken bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM product_variants WHERE scale_code = $1 AND id <> $2 AND deleted_at IS NULL)`,
					code, sc.VariantID.V).Scan(&taken); err != nil {
					return err
				}
				if taken {
					f[k+".scaleCode"] = "Another product has this scale code."
					continue
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE product_variants SET scale_code = NULLIF($2, ''), updated_at = now() WHERE id = $1`,
				sc.VariantID.V, code); err != nil {
				return err
			}
			after = append(after, "scale:"+code)
		}
		if err := apperr.Fields(f); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "product.barcodes", Entity: "product",
			EntityID: strconv.FormatInt(id, 10), RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent(),
			Before: map[string]any{"codes": before}, After: map[string]any{"codes": after}}); err != nil {
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
