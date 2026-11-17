// Package attributes implements typed product attributes (industry Phase 1
// item 4, docs/INDUSTRY_PHASE1_DESIGN.md DB5-DB6, A6, P2): a shop defines
// an attribute once (Size with its choices, Colour as free text, Season...)
// and a product uses some at product level and some as its variant
// dimensions. Products without variant dimensions and without product-level
// attributes keep today's free, unvalidated variant attributes.
package attributes

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Levels and kinds.
const (
	LevelProduct = "product"
	LevelVariant = "variant"
	KindChoice   = "choice"
	KindText     = "text"

	// MaxDims is the most variant dimensions a product may use.
	MaxDims = 4
	// MaxChoices is the most choices one attribute may list.
	MaxChoices = 200
	// MaxValue is the longest value (a choice or free text).
	MaxValue = 60

	// SizeKey and SizeSystemKey make footwear sizes read "UK 8".
	SizeKey       = "size"
	SizeSystemKey = "size_system"
)

// KeyPattern is the attribute key format (lower case, digits, underscore).
var KeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// Def is one attribute definition.
type Def struct {
	ID      int64    `json:"id"`
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Level   string   `json:"level"`
	Kind    string   `json:"kind"`
	Choices []string `json:"choices"`
	Sort    int      `json:"sort"`
	Status  string   `json:"status"`
	// Products is how many live products use the attribute (as a variant
	// dimension or a product-level value).
	Products int `json:"products"`
}

// Defs are the live definitions by key.
type Defs map[string]Def

const qLoadDefs = `SELECT d.id, d.key::text, d.label, d.level, d.kind, d.choices, d.sort, d.status,
		(SELECT count(*) FROM products p WHERE p.deleted_at IS NULL
		   AND (p.variant_dims @> ARRAY[d.key::text] OR p.attributes ? d.key::text))
	FROM attribute_defs d WHERE d.deleted_at IS NULL ORDER BY d.sort, lower(d.label), d.id`

// List returns the live definitions in display order.
func List(ctx context.Context, tx tenancy.TenantTx) ([]Def, error) {
	rows, err := tx.Query(ctx, qLoadDefs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Def{}
	for rows.Next() {
		var d Def
		var choices []byte
		if err := rows.Scan(&d.ID, &d.Key, &d.Label, &d.Level, &d.Kind, &choices, &d.Sort, &d.Status, &d.Products); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(choices, &d.Choices); err != nil {
			return nil, err
		}
		if d.Choices == nil {
			d.Choices = []string{}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Load returns the live definitions by key.
func Load(ctx context.Context, tx tenancy.TenantTx) (Defs, error) {
	list, err := List(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := make(Defs, len(list))
	for _, d := range list {
		out[d.Key] = d
	}
	return out, nil
}

// valueText turns a JSON value into its text (numbers like 8 or 8.5 too).
// ok is false for objects, arrays and null.
func valueText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x), true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case json.Number:
		return x.String(), true
	case bool:
		return strconv.FormatBool(x), true
	}
	return "", false
}

// Canonical returns the value as the definition stores it: the choice's
// own spelling (choices match case-insensitively) or the trimmed text.
func (d Def) Canonical(v any) (string, string) {
	s, ok := valueText(v)
	if !ok || s == "" {
		return "", "a value is required"
	}
	if len([]rune(s)) > MaxValue || strings.ContainsRune(s, 0) {
		return "", fmt.Sprintf("at most %d characters", MaxValue)
	}
	if d.Kind == KindChoice {
		for _, c := range d.Choices {
			if strings.EqualFold(c, s) {
				return c, ""
			}
		}
		return "", fmt.Sprintf("%q is not one of the choices of %s", s, d.Label)
	}
	return s, ""
}

// Checked is a product's validated attribute data.
type Checked struct {
	Attributes map[string]string // product-level values
	Dims       []string          // variant dimension keys, in order
	Variants   []map[string]any  // each variant's attributes (canonical when the product has dimensions)
}

// Uses reports whether a product opts in to typed attributes.
func (c Checked) Uses() bool { return len(c.Dims) > 0 || len(c.Attributes) > 0 }

// Check validates a product's attributes against the definitions. Only a
// product with dimensions or product-level attributes is validated; others
// keep their free variant attributes unchanged. Field errors use the API
// field names ("attributes.season", "variantDims", "variants[2].attributes.size").
func (defs Defs) Check(attrs map[string]any, dims []string, variants []map[string]any) (Checked, map[string]string) {
	f := map[string]string{}
	out := Checked{Attributes: map[string]string{}, Dims: []string{}, Variants: variants}
	seen := map[string]bool{}
	for _, k := range dims {
		k = strings.ToLower(strings.TrimSpace(k))
		d, ok := defs[k]
		switch {
		case !ok:
			f["variantDims"] = fmt.Sprintf("%q is not a defined attribute", k)
		case d.Level != LevelVariant:
			f["variantDims"] = fmt.Sprintf("%s is a product-level attribute, not a variant dimension", d.Label)
		case seen[k]:
			f["variantDims"] = fmt.Sprintf("%s is listed twice", d.Label)
		}
		seen[k] = true
		out.Dims = append(out.Dims, k)
	}
	if len(out.Dims) > MaxDims {
		f["variantDims"] = fmt.Sprintf("at most %d variant dimensions", MaxDims)
	}
	for k, v := range attrs {
		key := strings.ToLower(strings.TrimSpace(k))
		if s, ok := valueText(v); ok && s == "" {
			continue // an empty value clears it
		}
		d, ok := defs[key]
		if !ok {
			f["attributes."+k] = fmt.Sprintf("%q is not a defined attribute", k)
			continue
		}
		if d.Level != LevelProduct {
			f["attributes."+k] = d.Label + " is a variant dimension, not a product-level attribute"
			continue
		}
		val, msg := d.Canonical(v)
		if msg != "" {
			f["attributes."+k] = msg
			continue
		}
		out.Attributes[key] = val
	}
	if len(out.Dims) == 0 || len(f) > 0 {
		return out, f
	}
	combos := map[string]int{}
	out.Variants = make([]map[string]any, len(variants))
	for i, va := range variants {
		prefix := fmt.Sprintf("variants[%d].attributes", i)
		clean := map[string]any{}
		for k := range va {
			if !seen[strings.ToLower(k)] {
				f[prefix+"."+k] = fmt.Sprintf("%q is not a variant dimension of this product", k)
			}
		}
		parts := make([]string, 0, len(out.Dims))
		for _, k := range out.Dims {
			d := defs[k]
			raw, ok := va[k]
			if !ok {
				for vk, vv := range va { // keys match case-insensitively
					if strings.EqualFold(vk, k) {
						raw, ok = vv, true
					}
				}
			}
			if !ok {
				f[prefix+"."+k] = d.Label + " is required"
				continue
			}
			val, msg := d.Canonical(raw)
			if msg != "" {
				f[prefix+"."+k] = msg
				continue
			}
			clean[k] = val
			parts = append(parts, strings.ToLower(val))
		}
		out.Variants[i] = clean
		if len(parts) == len(out.Dims) {
			combo := strings.Join(parts, "\x1f")
			if j, dup := combos[combo]; dup {
				f[prefix] = fmt.Sprintf("the same %s as variant %d", strings.Join(out.Dims, " and "), j+1)
			}
			combos[combo] = i
		}
	}
	return out, f
}

// Label is a variant's display label: its dimension values in order, joined
// with " / ", a size prefixed with the product's size system ("UK 8 /
// Black"). Empty when the product has no dimensions. The SQL in
// sales.qInsertItem builds the same text for the sale line snapshot.
func Label(dims []string, productAttrs map[string]string, variantAttrs map[string]any) string {
	parts := make([]string, 0, len(dims))
	for _, k := range dims {
		s, ok := valueText(variantAttrs[k])
		if !ok || s == "" {
			continue
		}
		if k == SizeKey && productAttrs[SizeSystemKey] != "" {
			s = productAttrs[SizeSystemKey] + " " + s
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " / ")
}

// skuToken keeps the characters a SKU token may use, upper-cased.
var skuStrip = regexp.MustCompile(`[^A-Z0-9._/+-]+`)

// SKUToken is a value as it appears in a generated SKU: upper case, with
// spaces and other characters removed ("Navy blue" → "NAVYBLUE", "8.5" →
// "8.5").
func SKUToken(s string) string {
	return skuStrip.ReplaceAllString(strings.ToUpper(strings.TrimSpace(s)), "")
}
