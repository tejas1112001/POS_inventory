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

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/attributes"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend/internal/money"
	"github.com/tejas1112001/POS_inventory/backend/internal/qty"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// inventoryIn is one store's stock in a product payload.
type inventoryIn struct {
	StoreID       money.Int `json:"storeId"`
	Qty           qty.In    `json:"qty"`
	QuantityAlert qty.In    `json:"quantityAlert"`
}

// variantIn is one variant in a product payload. The edit form sends "_id".
type variantIn struct {
	ID          money.Int `json:"id"`
	IDAlt       money.Int `json:"_id"`
	SKU         string    `json:"sku"`
	ItemBarcode *string   `json:"itemBarcode"`
	Price       money.Dec `json:"price"`
	Cost        money.Dec `json:"cost"`
	Weight      money.Dec `json:"weight"`
	// MRP is the printed maximum retail price, tax included. Left out: kept
	// as it is (older clients never clear it); null clears it.
	MRP money.Dec `json:"mrp"`
	// MRPPack is the number of base units the MRP is printed for (a strip
	// of 10 tablets: 10). Left out: kept (new variants: 1).
	MRPPack          money.Dec      `json:"mrpPack"`
	Attributes       map[string]any `json:"attributes"`
	ExpiryDate       money.Date     `json:"expiryDate"`
	ManufacturedDate money.Date     `json:"manufacturedDate"`
	Inventories      []inventoryIn  `json:"inventories"`
	// The single-product edit path may send these instead of inventories.
	Quantity      qty.In    `json:"quantity"`
	StoreID       money.Int `json:"storeId"`
	QuantityAlert qty.In    `json:"quantityAlert"`
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
	MRP              money.Dec  `json:"mrp"`
	MRPPack          money.Dec  `json:"mrpPack"`
	Quantity         qty.In     `json:"quantity"`
	QuantityAlert    qty.In     `json:"quantityAlert"`
	StoreID          money.Int  `json:"storeId"`
	ExpiryDate       money.Date `json:"expiryDate"`
	ManufacturedDate money.Date `json:"manufacturedDate"`
	CreatedBy        any        `json:"createdBy"` // ignored: comes from the session
	// Industry P1 item 4 (A7). Left out (null) on an update: kept as they
	// are, so older clients never clear them.
	Attributes  *map[string]any `json:"attributes"`
	VariantDims *[]string       `json:"variantDims"`
	ArticleNo   *string         `json:"articleNo"`
	// Industry P3 item 1 (A25): null keeps the product as it is (a new
	// product follows the shop default: tracking on with the module on).
	TracksBatches *bool `json:"tracksBatches"`
	// H3: null keeps the product as it is (a new product: whole units).
	AllowFraction *bool `json:"allowFraction"`
	// H3: the other units; null keeps them, [] removes them.
	Units *[]unitIn `json:"units"`
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
	// Typed attributes (item 4). The *In fields are what the request sent
	// (nil: keep); attrs, dims and article are the values written.
	attrsIn   *map[string]any
	dimsIn    *[]string
	articleIn *string
	attrs     map[string]string
	dims      []string
	article   *string
}

type cleanVariant struct {
	id                   int64 // 0 = new
	sku                  string
	barcode              *string
	price, cost, weight  decimal.Decimal
	mrp                  *decimal.Decimal
	mrpSet               bool             // false: an update keeps the variant's MRP
	mrpPack              *decimal.Decimal // nil: kept (new variants: 1)
	attributes           []byte
	attrMap              map[string]any
	expiry, manufactured *string
	inventories          []cleanInventory
}

type cleanInventory struct {
	storeID int64
	qty     qty.Q
	alert   qty.Q
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
	c.attrsIn, c.dimsIn = in.Attributes, in.VariantDims
	if in.ArticleNo != nil {
		a := strings.TrimSpace(*in.ArticleNo)
		if len([]rune(a)) > 64 || strings.ContainsRune(a, 0) {
			f.add("articleNo", "articleNo must be at most 64 characters")
		}
		c.articleIn = &a
	}

	variants := in.Variants
	if c.productType == "single" && len(variants) == 0 && in.SKU != nil {
		// Old add form: single product at the top level.
		v := variantIn{SKU: *in.SKU, ItemBarcode: in.ItemBarcode, Price: in.Price, MRP: in.MRP, MRPPack: in.MRPPack,
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
	if v.MRP.Set {
		cv.mrpSet = true
		if v.MRP.Valid {
			if v.MRP.D.IsNegative() || v.MRP.D.GreaterThan(maxPrice) {
				f.add(prefix+".mrp", "MRP must be between 0 and 99999999999999")
			}
			m := v.MRP.D.Round(2)
			cv.mrp = &m
		}
	}
	if v.MRPPack.Set && v.MRPPack.Valid {
		pk := v.MRPPack.D
		if !pk.IsPositive() || pk.GreaterThan(maxMRPPack) || !pk.Round(3).Equal(pk) {
			f.add(prefix+".mrpPack", "The MRP pack must be a number of units above 0 with at most 3 decimals.")
		}
		cv.mrpPack = &pk
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
	cv.attrMap = v.Attributes
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
		if inv.Qty.Err != nil || inv.Qty.V.IsNegative() || inv.Qty.V.Greater(maxStock) {
			f.add(p+".qty", "qty must be between 0 and 1000000000")
		}
		if inv.QuantityAlert.Err != nil || inv.QuantityAlert.V.IsNegative() || inv.QuantityAlert.V.Greater(maxStock) {
			f.add(p+".quantityAlert", "quantityAlert must not be negative")
		}
		cv.inventories = append(cv.inventories, cleanInventory{storeID: inv.StoreID.V, qty: inv.Qty.V, alert: inv.QuantityAlert.V})
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
		brand_id, unit_id, supplier_id, created_by, attributes, variant_dims, article_no)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, NULLIF($20, 0),
		$21::jsonb, $22::text[], $23) RETURNING id`
	qInsertVariant = `INSERT INTO product_variants (product_id, sku, item_barcode, price, cost, weight, attributes, expiry_date,
		manufactured_date, created_by, mrp, mrp_pack) VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9::date, NULLIF($10, 0), $11,
		coalesce($12, 1)) RETURNING id`
	qUpdateProduct = `UPDATE products SET name = $2, description = $3, slug = $4, status = $5, image_file_id = $6, selling_type = $7,
		tax_type = $8, tax_rate = $9, hsn_code = $10, discount_type = $11, discount_value = $12, warranties = $13,
		barcode_symbology = $14, category_id = $15, sub_category_id = $16, brand_id = $17, unit_id = $18, supplier_id = $19,
		attributes = $20::jsonb, variant_dims = $21::text[], article_no = $22, updated_at = now() WHERE id = $1`
	qUpdateVariant = `UPDATE product_variants SET sku = $2, item_barcode = $3, price = $4, cost = $5, weight = $6, attributes = $7,
		expiry_date = $8::date, manufactured_date = $9::date, mrp = CASE WHEN $10 THEN $11 ELSE mrp END,
		mrp_pack = coalesce($12, mrp_pack), updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`
)

// insertVariant writes a variant and its opening stock.
func insertVariant(ctx context.Context, tx tenancy.TenantTx, productID int64, v cleanVariant, userID int64, reason string) (int64, error) {
	if err := extraCodeTaken(ctx, tx, v.barcode); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(ctx, qInsertVariant, productID, v.sku, v.barcode, v.price, v.cost, v.weight, string(v.attributes),
		v.expiry, v.manufactured, userID, v.mrp, v.mrpPack).Scan(&id)
	if err != nil {
		return 0, skuConflict(err)
	}
	if err := refuseAboveMRP(ctx, tx, []int64{id}, checkVariantPrice); err != nil {
		return 0, err
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
	if err := applyAttributes(ctx, tx, &c, nil, nil, nil); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(ctx, qInsertProduct, c.name, c.description, c.slug, c.status, img, c.sellingType, c.productType, c.taxType,
		c.tax, c.hsn, c.discountType, c.discountValue, c.warranties, c.barcodeSymbology, c.categoryID, c.subCategoryID,
		c.brandID, c.unitID, c.supplierID, userID, attrsJSON(c.attrs), c.dims, c.article).Scan(&id)
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
	id      int64
	price   decimal.Decimal
	mrp     *decimal.Decimal
	mrpPack decimal.Decimal
}

// updateProduct applies C-14: variant IDs must belong to the product,
// left-out variants are soft-deleted (refused while they hold stock), and
// every stock change is written to the ledger.
func updateProduct(ctx context.Context, tx tenancy.TenantTx, id int64, c clean, img *uuid.UUID, userID int64) error {
	var ptype string
	var curAttrs map[string]string
	var curDims []string
	var curArticle *string
	if err := tx.QueryRow(ctx, `SELECT product_type, attributes, variant_dims, article_no FROM products WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`,
		id).Scan(&ptype, &curAttrs, &curDims, &curArticle); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return err
	}
	if ptype != c.productType {
		return apperr.Invalid("productType", "productType cannot be changed")
	}
	if err := applyAttributes(ctx, tx, &c, curAttrs, curDims, curArticle); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id, price, mrp, mrp_pack FROM product_variants WHERE product_id = $1 AND deleted_at IS NULL FOR UPDATE`, id)
	if err != nil {
		return err
	}
	existing := map[int64]existingVariant{}
	for rows.Next() {
		var v existingVariant
		if err := rows.Scan(&v.id, &v.price, &v.mrp, &v.mrpPack); err != nil {
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
		var stock qty.Q
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(qty), 0) FROM inventories WHERE variant_id = $1`, vid).Scan(&stock); err != nil {
			return err
		}
		if stock.IsPositive() {
			return apperr.Conflict(apperr.CodeInvalidState, "A variant that still has stock cannot be removed. Set its stock to 0 first.")
		}
		if _, err := tx.Exec(ctx, `UPDATE product_variants SET deleted_at = now() WHERE id = $1`, vid); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, qUpdateProduct, id, c.name, c.description, c.slug, c.status, img, c.sellingType, c.taxType,
		c.tax, c.hsn, c.discountType, c.discountValue, c.warranties, c.barcodeSymbology, c.categoryID, c.subCategoryID,
		c.brandID, c.unitID, c.supplierID, attrsJSON(c.attrs), c.dims, c.article); err != nil {
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
		if err := extraCodeTaken(ctx, tx, v.barcode); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qUpdateVariant, v.id, v.sku, v.barcode, v.price, v.cost, v.weight, string(v.attributes),
			v.expiry, v.manufactured, v.mrpSet, v.mrp, v.mrpPack); err != nil {
			return skuConflict(err)
		}
		// A changed price, MRP or pack must stay at or below the MRP; a
		// lower MRP also covers the variant's store prices.
		old := existing[v.id]
		mrpChanged := v.mrpSet && !sameDec(old.mrp, v.mrp) || v.mrpPack != nil && !v.mrpPack.Equal(old.mrpPack)
		what := 0
		if !old.price.Equal(v.price) || mrpChanged {
			what |= checkVariantPrice
		}
		if mrpChanged {
			what |= checkStorePrices
		}
		if what != 0 {
			if err := refuseAboveMRP(ctx, tx, []int64{v.id}, what); err != nil {
				return err
			}
		}
		if old := existing[v.id]; !old.price.Equal(v.price) {
			if err := audit.Write(ctx, tx, audit.Entry{ActorID: userID, Action: "product.price_change", Entity: "variant",
				EntityID: strconv.FormatInt(v.id, 10), Before: map[string]any{"price": old.price}, After: map[string]any{"price": v.price}}); err != nil {
				return err
			}
		}
		if old := existing[v.id]; v.mrpSet && !sameDec(old.mrp, v.mrp) {
			if err := audit.Write(ctx, tx, audit.Entry{ActorID: userID, Action: "product.mrp_change", Entity: "variant",
				EntityID: strconv.FormatInt(v.id, 10), Before: map[string]any{"mrp": old.mrp}, After: map[string]any{"mrp": v.mrp}}); err != nil {
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

// sameDec reports whether two optional amounts are equal (both unset, or
// the same value).
func sameDec(a, b *decimal.Decimal) bool {
	return (a == nil) == (b == nil) && (a == nil || a.Equal(*b))
}

// attrsJSON is the products.attributes value.
func attrsJSON(m map[string]string) string {
	if m == nil {
		m = map[string]string{}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// applyAttributes settles a product's typed attributes (industry P1 item 4,
// A7): what the request left out keeps the current value; a product with
// variant dimensions or product-level attributes is validated against the
// shop's definitions and its variant values are stored as the definitions
// spell them. Others keep today's free variant attributes, unvalidated.
func applyAttributes(ctx context.Context, tx tenancy.TenantTx, c *clean, curAttrs map[string]string, curDims []string, curArticle *string) error {
	c.article = curArticle
	if c.articleIn != nil {
		c.article = nil
		if *c.articleIn != "" {
			c.article = c.articleIn
		}
	}
	attrs := map[string]any{}
	if c.attrsIn != nil {
		attrs = *c.attrsIn
	} else {
		for k, v := range curAttrs {
			attrs[k] = v
		}
	}
	dims := curDims
	if c.dimsIn != nil {
		dims = *c.dimsIn
	}
	c.attrs, c.dims = map[string]string{}, []string{}
	if len(dims) == 0 && len(attrs) == 0 {
		return nil
	}
	defs, err := attributes.Load(ctx, tx)
	if err != nil {
		return err
	}
	variants := make([]map[string]any, len(c.variants))
	for i, v := range c.variants {
		variants[i] = v.attrMap
	}
	checked, f := defs.Check(attrs, dims, variants)
	if len(f) > 0 {
		return apperr.Fields(f)
	}
	c.attrs, c.dims = checked.Attributes, checked.Dims
	if len(c.dims) > 0 {
		for i := range c.variants {
			c.variants[i].attrMap = checked.Variants[i]
			c.variants[i].attributes, _ = json.Marshal(checked.Variants[i])
		}
	}
	return nil
}
