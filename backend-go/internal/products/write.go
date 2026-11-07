package products

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// inventoryIn is one store's stock in a product payload.
type inventoryIn struct {
	StoreID       money.Int `json:"storeId"`
	Qty           money.Int `json:"qty"`
	QuantityAlert money.Int `json:"quantityAlert"`
}

// variantIn is one variant in a product payload. The edit form sends "_id".
type variantIn struct {
	ID               money.Int      `json:"id"`
	IDAlt            money.Int      `json:"_id"`
	SKU              string         `json:"sku"`
	ItemBarcode      *string        `json:"itemBarcode"`
	Price            money.Dec      `json:"price"`
	Cost             money.Dec      `json:"cost"`
	Weight           money.Dec      `json:"weight"`
	Attributes       map[string]any `json:"attributes"`
	ExpiryDate       money.Date     `json:"expiryDate"`
	ManufacturedDate money.Date     `json:"manufacturedDate"`
	Inventories      []inventoryIn  `json:"inventories"`
	// The single-product edit path may send these instead of inventories.
	Quantity      money.Int `json:"quantity"`
	StoreID       money.Int `json:"storeId"`
	QuantityAlert money.Int `json:"quantityAlert"`
}

func (v variantIn) id() int64 {
	if v.ID.Valid {
		return v.ID.V
	}
	return v.IDAlt.V
}

// productIn is the POST/PUT /api/products body.
type productIn struct {
	Name             string      `json:"name"`
	Status           string      `json:"status"`
	Description      *string     `json:"description"`
	CategoryID       money.Int   `json:"categoryId"`
	SubCategoryID    money.Int   `json:"subCategoryId"`
	BrandID          money.Int   `json:"brandId"`
	UnitID           money.Int   `json:"unitId"`
	SupplierID       money.Int   `json:"supplierId"`
	ProductType      string      `json:"productType"`
	TaxType          *string     `json:"taxType"`
	Tax              money.Dec   `json:"tax"`
	HSNCode          *string     `json:"hsnCode"`
	DiscountType     *string     `json:"discountType"`
	DiscountValue    money.Dec   `json:"discountValue"`
	Warranties       *string     `json:"warranties"`
	BarcodeSymbology *string     `json:"barcodeSymbology"`
	SellingType      *string     `json:"sellingType"`
	Image            *string     `json:"image"`
	Slug             *string     `json:"slug"`
	Variants         []variantIn `json:"variants"`
	// Single products: the add form also sends these at the top level.
	SKU              *string    `json:"sku"`
	ItemBarcode      *string    `json:"itemBarcode"`
	Price            money.Dec  `json:"price"`
	Quantity         money.Int  `json:"quantity"`
	QuantityAlert    money.Int  `json:"quantityAlert"`
	StoreID          money.Int  `json:"storeId"`
	ExpiryDate       money.Date `json:"expiryDate"`
	ManufacturedDate money.Date `json:"manufacturedDate"`
	CreatedBy        any        `json:"createdBy"` // ignored: comes from the session
}

var (
	barcodePattern = regexp.MustCompile(`^[A-Za-z0-9._/+-]{1,64}$`)
	hsnPattern     = regexp.MustCompile(`^[0-9]{2,8}$`)
	hundred        = decimal.NewFromInt(100)
	maxPrice       = decimal.RequireFromString("99999999999999")
)

// clean is a validated product.
type clean struct {
	name, status, productType                                 string
	description, slug, taxType, hsn, discountType, warranties *string
	barcodeSymbology, sellingType                             *string
	tax, discountValue                                        *decimal.Decimal
	categoryID, subCategoryID, brandID, unitID, supplierID    *int64
	variants                                                  []cleanVariant
}

type cleanVariant struct {
	id                   int64 // 0 = new
	sku                  string
	barcode              *string
	price, cost, weight  decimal.Decimal
	attributes           []byte
	expiry, manufactured *string
	inventories          []cleanInventory
}

type cleanInventory struct {
	storeID int64
	qty     int
	alert   int
}

type fieldErrs map[string]string

func (f fieldErrs) add(field, msg string) {
	if _, ok := f[field]; !ok {
		f[field] = msg
	}
}

func (f fieldErrs) err() error {
	if len(f) == 0 {
		return nil
	}
	first := sortedKeys(f)[0] // deterministic headline message
	e := apperr.Invalid(first, f[first])
	e.Fields = f
	return e
}

func optEnum(f fieldErrs, field string, v *string, allowed ...string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	s := strings.TrimSpace(*v)
	for _, a := range allowed {
		if s == a {
			return &s
		}
	}
	f.add(field, field+" must be one of "+strings.Join(allowed, ", "))
	return nil
}

func optText(f fieldErrs, field string, v *string, max int) *string {
	if v == nil {
		return nil
	}
	s := strings.TrimSpace(*v)
	if s == "" {
		return nil
	}
	if len([]rune(s)) > max || strings.ContainsRune(s, 0) {
		f.add(field, fmt.Sprintf("%s must be at most %d characters", field, max))
		return nil
	}
	return &s
}

func decPtr(d money.Dec) *decimal.Decimal {
	if !d.Valid {
		return nil
	}
	v := d.D
	return &v
}

func idPtr(f fieldErrs, field string, v money.Int) *int64 {
	if !v.Valid {
		return nil
	}
	if v.V <= 0 {
		f.add(field, field+" is invalid")
		return nil
	}
	return v.Ptr()
}

func dateStr(d money.Date) *string {
	if !d.Valid {
		return nil
	}
	s := d.T.Format("2006-01-02")
	return &s
}

// validate checks the payload shape. References are checked in the DB
// transaction (checkRefs).
func validate(in productIn) (clean, error) {
	f := fieldErrs{}
	c := clean{}
	c.name = strings.TrimSpace(in.Name)
	if c.name == "" {
		f.add("name", "Product name is required.")
	} else if len([]rune(c.name)) > 200 {
		f.add("name", "name must be at most 200 characters")
	}
	c.status = "Active"
	if in.Status == "Inactive" {
		c.status = "Inactive"
	}
	c.productType = in.ProductType
	if c.productType != "single" && c.productType != "variable" {
		f.add("productType", "Invalid product type.")
	}
	c.description = optText(f, "description", in.Description, 5000)
	c.slug = optText(f, "slug", in.Slug, 200)
	c.warranties = optText(f, "warranties", in.Warranties, 500)
	c.barcodeSymbology = optText(f, "barcodeSymbology", in.BarcodeSymbology, 40)
	c.taxType = optEnum(f, "taxType", in.TaxType, "Exclusive", "Inclusive")
	c.discountType = optEnum(f, "discountType", in.DiscountType, "Percentage", "Fixed")
	c.sellingType = optEnum(f, "sellingType", in.SellingType, "Online", "POS")
	if h := optText(f, "hsnCode", in.HSNCode, 8); h != nil {
		if !hsnPattern.MatchString(*h) {
			f.add("hsnCode", "hsnCode must be 2-8 digits")
		}
		c.hsn = h
	}
	if c.tax = decPtr(in.Tax); c.tax != nil && (c.tax.IsNegative() || c.tax.GreaterThan(hundred)) {
		f.add("tax", "tax must be between 0 and 100")
	}
	if c.discountValue = decPtr(in.DiscountValue); c.discountValue != nil {
		if c.discountValue.IsNegative() {
			f.add("discountValue", "discountValue must not be negative")
		} else if c.discountType != nil && *c.discountType == "Percentage" && c.discountValue.GreaterThan(hundred) {
			f.add("discountValue", "a percentage discount must be at most 100")
		}
	}
	c.categoryID = idPtr(f, "categoryId", in.CategoryID)
	c.subCategoryID = idPtr(f, "subCategoryId", in.SubCategoryID)
	c.brandID = idPtr(f, "brandId", in.BrandID)
	c.unitID = idPtr(f, "unitId", in.UnitID)
	c.supplierID = idPtr(f, "supplierId", in.SupplierID)

	variants := in.Variants
	if c.productType == "single" && len(variants) == 0 && in.SKU != nil {
		// Old add form: single product at the top level.
		v := variantIn{SKU: *in.SKU, ItemBarcode: in.ItemBarcode, Price: in.Price,
			ExpiryDate: in.ExpiryDate, ManufacturedDate: in.ManufacturedDate}
		if in.StoreID.Valid {
			v.Inventories = []inventoryIn{{StoreID: in.StoreID, Qty: in.Quantity, QuantityAlert: in.QuantityAlert}}
		}
		variants = []variantIn{v}
	}
	switch {
	case len(variants) == 0:
		f.add("variants", "At least one variant is required.")
	case c.productType == "single" && len(variants) != 1:
		f.add("variants", "A single product has exactly one variant.")
	case len(variants) > 200:
		f.add("variants", "At most 200 variants per product.")
	}
	skus := map[string]bool{}
	for i, v := range variants {
		cv, err := validateVariant(f, fmt.Sprintf("variants[%d]", i), v)
		if err == nil {
			key := strings.ToLower(cv.sku)
			if skus[key] {
				f.add(fmt.Sprintf("variants[%d].sku", i), "SKU is used twice in this product")
			}
			skus[key] = true
			c.variants = append(c.variants, cv)
		}
	}
	return c, f.err()
}

func validateVariant(f fieldErrs, prefix string, v variantIn) (cleanVariant, error) {
	before := len(f)
	cv := cleanVariant{id: v.id()}
	cv.sku = strings.TrimSpace(v.SKU)
	if cv.sku == "" {
		f.add(prefix+".sku", "SKU is required.")
	} else if len(cv.sku) > 64 {
		f.add(prefix+".sku", "SKU must be at most 64 characters")
	}
	if v.ItemBarcode != nil && strings.TrimSpace(*v.ItemBarcode) != "" {
		b := strings.TrimSpace(*v.ItemBarcode)
		if !barcodePattern.MatchString(b) {
			f.add(prefix+".itemBarcode", "barcode may use letters, digits and . _ / + - (max 64)")
		}
		cv.barcode = &b
	}
	if !v.Price.Valid {
		f.add(prefix+".price", "price is required")
	} else if v.Price.D.IsNegative() || v.Price.D.GreaterThan(maxPrice) {
		f.add(prefix+".price", "price must be between 0 and 99999999999999")
	}
	cv.price = v.Price.D
	for _, m := range []struct {
		name string
		in   money.Dec
		out  *decimal.Decimal
	}{{"cost", v.Cost, &cv.cost}, {"weight", v.Weight, &cv.weight}} {
		if m.in.Valid {
			if m.in.D.IsNegative() || m.in.D.GreaterThan(maxPrice) {
				f.add(prefix+"."+m.name, m.name+" must not be negative")
			}
			*m.out = m.in.D
		}
	}
	if len(v.Attributes) > 20 {
		f.add(prefix+".attributes", "at most 20 attributes")
	}
	for k, val := range v.Attributes {
		switch val.(type) {
		case string, float64, bool, nil:
		default:
			f.add(prefix+".attributes", "attribute values must be text, numbers or booleans")
		}
		if len(k) > 60 {
			f.add(prefix+".attributes", "attribute names must be at most 60 characters")
		}
	}
	if v.Attributes == nil {
		v.Attributes = map[string]any{}
	}
	cv.attributes, _ = json.Marshal(v.Attributes)
	if len(cv.attributes) > 4000 {
		f.add(prefix+".attributes", "attributes are too large")
	}
	cv.expiry, cv.manufactured = dateStr(v.ExpiryDate), dateStr(v.ManufacturedDate)

	invs := v.Inventories
	if len(invs) == 0 && v.StoreID.Valid {
		invs = []inventoryIn{{StoreID: v.StoreID, Qty: v.Quantity, QuantityAlert: v.QuantityAlert}}
	}
	seen := map[int64]bool{}
	for j, inv := range invs {
		p := fmt.Sprintf("%s.inventories[%d]", prefix, j)
		if !inv.StoreID.Valid || inv.StoreID.V <= 0 {
			f.add(p+".storeId", "storeId is required")
			continue
		}
		if seen[inv.StoreID.V] {
			f.add(p+".storeId", "the same store is listed twice")
		}
		seen[inv.StoreID.V] = true
		if inv.Qty.V < 0 || inv.Qty.V > 1_000_000_000 {
			f.add(p+".qty", "qty must be between 0 and 1000000000")
		}
		if inv.QuantityAlert.V < 0 || inv.QuantityAlert.V > 1_000_000_000 {
			f.add(p+".quantityAlert", "quantityAlert must not be negative")
		}
		cv.inventories = append(cv.inventories, cleanInventory{storeID: inv.StoreID.V, qty: int(inv.Qty.V), alert: int(inv.QuantityAlert.V)})
	}
	if len(f) > before {
		return cv, errors.New("invalid variant")
	}
	return cv, nil
}

// checkRefs verifies that referenced master data exists and is not deleted.
func checkRefs(ctx context.Context, tx tenancy.TenantTx, c clean) error {
	f := fieldErrs{}
	// Always Scan: an unread pgx row keeps the connection busy.
	exists := func(field string, id *int64, row pgx.Row) error {
		var ok bool
		if err := row.Scan(&ok); err != nil {
			return err
		}
		if id != nil && !ok {
			f.add(field, field+" not found")
		}
		return nil
	}
	// Each lookup runs only when the reference is set (the zero ID matches nothing).
	ref := func(id *int64) int64 {
		if id == nil {
			return 0
		}
		return *id
	}
	if err := exists("categoryId", c.categoryID, tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM categories WHERE id = $1 AND deleted_at IS NULL)`, ref(c.categoryID))); err != nil {
		return err
	}
	if err := exists("brandId", c.brandID, tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM brands WHERE id = $1 AND deleted_at IS NULL)`, ref(c.brandID))); err != nil {
		return err
	}
	if err := exists("unitId", c.unitID, tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM units WHERE id = $1 AND deleted_at IS NULL)`, ref(c.unitID))); err != nil {
		return err
	}
	if err := exists("supplierId", c.supplierID, tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM suppliers WHERE id = $1 AND deleted_at IS NULL)`, ref(c.supplierID))); err != nil {
		return err
	}
	var ok bool
	if c.subCategoryID != nil {
		var cat int64
		err := tx.QueryRow(ctx, `SELECT category_id FROM sub_categories WHERE id = $1 AND deleted_at IS NULL`, *c.subCategoryID).Scan(&cat)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			f.add("subCategoryId", "subCategoryId not found")
		case err != nil:
			return err
		case c.categoryID != nil && cat != *c.categoryID:
			f.add("subCategoryId", "the sub-category belongs to another category")
		}
	}
	stores := map[int64]bool{}
	for _, v := range c.variants {
		for _, inv := range v.inventories {
			stores[inv.storeID] = true
		}
	}
	for id := range stores {
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM stores WHERE id = $1 AND deleted_at IS NULL)`, id).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			f.add("storeId", "store "+strconv.FormatInt(id, 10)+" not found")
		}
	}
	return f.err()
}

// skuConflict turns unique violations into a 409 that names the SKU or
// barcode.
func skuConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "variants_sku_uq":
			return apperr.Conflict(apperr.CodeConflict, "A product with this SKU already exists.")
		case "variants_barcode_uq":
			return apperr.Conflict(apperr.CodeConflict, "A product with this barcode already exists.")
		}
	}
	return err
}

const (
	qInsertProduct = `INSERT INTO products (name, description, slug, status, image_file_id, selling_type, product_type, tax_type,
		tax_rate, hsn_code, discount_type, discount_value, warranties, barcode_symbology, category_id, sub_category_id,
		brand_id, unit_id, supplier_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, NULLIF($20, 0)) RETURNING id`
	qInsertVariant = `INSERT INTO product_variants (product_id, sku, item_barcode, price, cost, weight, attributes, expiry_date,
		manufactured_date, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9::date, NULLIF($10, 0)) RETURNING id`
	qUpdateProduct = `UPDATE products SET name = $2, description = $3, slug = $4, status = $5, image_file_id = $6, selling_type = $7,
		tax_type = $8, tax_rate = $9, hsn_code = $10, discount_type = $11, discount_value = $12, warranties = $13,
		barcode_symbology = $14, category_id = $15, sub_category_id = $16, brand_id = $17, unit_id = $18, supplier_id = $19,
		updated_at = now() WHERE id = $1`
	qUpdateVariant = `UPDATE product_variants SET sku = $2, item_barcode = $3, price = $4, cost = $5, weight = $6, attributes = $7,
		expiry_date = $8::date, manufactured_date = $9::date, updated_at = now() WHERE id = $1 AND deleted_at IS NULL`
)

// insertVariant writes a variant and its opening stock.
func insertVariant(ctx context.Context, tx tenancy.TenantTx, productID int64, v cleanVariant, userID int64, reason string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, qInsertVariant, productID, v.sku, v.barcode, v.price, v.cost, v.weight, string(v.attributes),
		v.expiry, v.manufactured, userID).Scan(&id)
	if err != nil {
		return 0, skuConflict(err)
	}
	for _, inv := range v.inventories {
		if _, err := inventory.Set(ctx, tx, inv.storeID, id, inv.qty, inv.alert, reason, inventory.Ref{Type: "product", ID: productID}, userID); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func createProduct(ctx context.Context, tx tenancy.TenantTx, c clean, img *uuid.UUID, userID int64) (int64, error) {
	return createProductWith(ctx, tx, c, img, userID, inventory.ReasonOpening, "product.create")
}

// createProductWith inserts a product, its variants and opening stock.
func createProductWith(ctx context.Context, tx tenancy.TenantTx, c clean, img *uuid.UUID, userID int64, reason, action string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, qInsertProduct, c.name, c.description, c.slug, c.status, img, c.sellingType, c.productType, c.taxType,
		c.tax, c.hsn, c.discountType, c.discountValue, c.warranties, c.barcodeSymbology, c.categoryID, c.subCategoryID,
		c.brandID, c.unitID, c.supplierID, userID).Scan(&id)
	if err != nil {
		return 0, err
	}
	for _, v := range c.variants {
		if _, err := insertVariant(ctx, tx, id, v, userID, reason); err != nil {
			return 0, err
		}
	}
	return id, audit.Write(ctx, tx, audit.Entry{ActorID: userID, Action: action, Entity: "product",
		EntityID: strconv.FormatInt(id, 10), After: map[string]any{"name": c.name, "variants": len(c.variants)}})
}

type existingVariant struct {
	id    int64
	price decimal.Decimal
}

// updateProduct applies C-14: variant IDs must belong to the product,
// left-out variants are soft-deleted (refused while they hold stock), and
// every stock change is written to the ledger.
func updateProduct(ctx context.Context, tx tenancy.TenantTx, id int64, c clean, img *uuid.UUID, userID int64) error {
	var ptype string
	if err := tx.QueryRow(ctx, `SELECT product_type FROM products WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&ptype); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return err
	}
	if ptype != c.productType {
		return apperr.Invalid("productType", "productType cannot be changed")
	}
	rows, err := tx.Query(ctx, `SELECT id, price FROM product_variants WHERE product_id = $1 AND deleted_at IS NULL FOR UPDATE`, id)
	if err != nil {
		return err
	}
	existing := map[int64]existingVariant{}
	for rows.Next() {
		var v existingVariant
		if err := rows.Scan(&v.id, &v.price); err != nil {
			rows.Close()
			return err
		}
		existing[v.id] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	incoming := map[int64]bool{}
	for _, v := range c.variants {
		if v.id == 0 {
			continue
		}
		if _, ok := existing[v.id]; !ok {
			return apperr.Invalid("variants", "variant "+strconv.FormatInt(v.id, 10)+" does not belong to this product")
		}
		incoming[v.id] = true
	}
	// Remove left-out variants (soft), never while they hold stock.
	for vid := range existing {
		if incoming[vid] {
			continue
		}
		var stock int
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(qty), 0) FROM inventories WHERE variant_id = $1`, vid).Scan(&stock); err != nil {
			return err
		}
		if stock > 0 {
			return apperr.Conflict(apperr.CodeInvalidState, "A variant that still has stock cannot be removed. Set its stock to 0 first.")
		}
		if _, err := tx.Exec(ctx, `UPDATE product_variants SET deleted_at = now() WHERE id = $1`, vid); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, qUpdateProduct, id, c.name, c.description, c.slug, c.status, img, c.sellingType, c.taxType,
		c.tax, c.hsn, c.discountType, c.discountValue, c.warranties, c.barcodeSymbology, c.categoryID, c.subCategoryID,
		c.brandID, c.unitID, c.supplierID); err != nil {
		return err
	}
	ref := inventory.Ref{Type: "product", ID: id}
	for _, v := range c.variants {
		if v.id == 0 {
			if _, err := insertVariant(ctx, tx, id, v, userID, inventory.ReasonProductEdit); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(ctx, qUpdateVariant, v.id, v.sku, v.barcode, v.price, v.cost, v.weight, string(v.attributes),
			v.expiry, v.manufactured); err != nil {
			return skuConflict(err)
		}
		if old := existing[v.id]; !old.price.Equal(v.price) {
			if err := audit.Write(ctx, tx, audit.Entry{ActorID: userID, Action: "product.price_change", Entity: "variant",
				EntityID: strconv.FormatInt(v.id, 10), Before: map[string]any{"price": old.price}, After: map[string]any{"price": v.price}}); err != nil {
				return err
			}
		}
		// Listed stores are set; stores not listed keep their stock.
		for _, inv := range v.inventories {
			if _, err := inventory.Set(ctx, tx, inv.storeID, v.id, inv.qty, inv.alert, inventory.ReasonProductEdit, ref, userID); err != nil {
				return err
			}
		}
	}
	var live int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM product_variants WHERE product_id = $1 AND deleted_at IS NULL`, id).Scan(&live); err != nil {
		return err
	}
	if live == 0 || (ptype == "single" && live != 1) {
		return apperr.Invalid("variants", "A single product has exactly one variant; a variable product at least one.")
	}
	return audit.Write(ctx, tx, audit.Entry{ActorID: userID, Action: "product.update", Entity: "product",
		EntityID: strconv.FormatInt(id, 10), After: map[string]any{"name": c.name, "variants": len(c.variants)}})
}
