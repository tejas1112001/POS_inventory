// Package products implements products, variants, the POS product list,
// barcode lookup, POS locks, inventory, the stock ledger API, store
// showcase publication and CSV import (API_CONTRACT §2.2, §2.4, C-14,
// C-15, C-18, C-19).
package products

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// InventoryJSON keeps the old Inventory.toJSON() keys.
type InventoryJSON struct {
	ID            int64     `json:"id"`
	Qty           int       `json:"qty"`
	QuantityAlert int       `json:"quantityAlert"`
	VariantID     int64     `json:"variant_id"`
	StoreID       int64     `json:"store_id"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// VariantJSON keeps the old ProductVariant.toJSON() keys.
type VariantJSON struct {
	ID               int64           `json:"id"`
	SKU              string          `json:"sku"`
	ItemBarcode      *string         `json:"itemBarcode"`
	Price            decimal.Decimal `json:"price"`
	Cost             decimal.Decimal `json:"cost"`
	Weight           decimal.Decimal `json:"weight"`
	Attributes       map[string]any  `json:"attributes"`
	ExpiryDate       *string         `json:"expiryDate"`
	ManufacturedDate *string         `json:"manufacturedDate"`
	ProductID        int64           `json:"product_id"`
	CreatedBy        *int64          `json:"created_by"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	Inventories      []InventoryJSON `json:"Inventories"`
}

// ProductJSON is the old transformProduct() shape: model fields plus
// category/brand/unit names, camelCase IDs, first-variant price, total qty.
type ProductJSON struct {
	ID               int64            `json:"id"`
	Name             string           `json:"name"`
	Description      *string          `json:"description"`
	Status           string           `json:"status"`
	Image            *string          `json:"image"`
	Slug             *string          `json:"slug"`
	SellingType      *string          `json:"sellingType"`
	ProductType      string           `json:"productType"`
	TaxType          *string          `json:"taxType"`
	Tax              *decimal.Decimal `json:"tax"`
	HSNCode          *string          `json:"hsnCode"`
	DiscountType     *string          `json:"discountType"`
	DiscountValue    *decimal.Decimal `json:"discountValue"`
	Warranties       *string          `json:"warranties"`
	BarcodeSymbology *string          `json:"barcodeSymbology"`
	IsLocked         bool             `json:"isLocked"`
	LockedAt         *time.Time       `json:"lockedAt"`
	LockedBy         *int64           `json:"locked_by"`
	IsPublished      bool             `json:"isPublished"`
	IsFeatured       bool             `json:"isFeatured"`
	CategoryID       *int64           `json:"category_id"`
	SubCategoryID    *int64           `json:"sub_category_id"`
	BrandID          *int64           `json:"brand_id"`
	UnitID           *int64           `json:"unit_id"`
	SupplierID       *int64           `json:"supplier_id"`
	CreatedByID      *int64           `json:"created_by"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
	ProductVariants  []VariantJSON    `json:"ProductVariants"`

	Category       string          `json:"category"`
	CategoryIDC    *int64          `json:"categoryId"`
	Brand          string          `json:"brand"`
	BrandIDC       *int64          `json:"brandId"`
	Unit           string          `json:"unit"`
	UnitIDC        *int64          `json:"unitId"`
	SubCategoryIDC *int64          `json:"subCategoryId"`
	SupplierIDC    *int64          `json:"supplierId"`
	CreatedByName  string          `json:"createdBy"`
	CreatedByIDC   *int64          `json:"createdById"`
	Price          decimal.Decimal `json:"price"`
	Qty            int             `json:"qty"`

	imageID *uuid.UUID
}

// ImageURL is public while the product is published in a store, otherwise
// behind login (the public route would refuse it).
func imageURL(id *uuid.UUID, published bool) *string {
	if id == nil {
		return nil
	}
	s := "/api/files/" + id.String()
	if published {
		s = files.URL(*id, files.PurposeProductImage)
	}
	return &s
}

// loadOpts selects how products are loaded.
type loadOpts struct {
	storeID int64 // > 0: inventories, publication and locks for this store only
}

const (
	qLoadProducts = `SELECT p.id, p.name, p.description, p.status, p.image_file_id, p.slug::text, p.selling_type, p.product_type,
		p.tax_type, p.tax_rate, p.hsn_code, p.discount_type, p.discount_value, p.warranties, p.barcode_symbology,
		p.category_id, p.sub_category_id, p.brand_id, p.unit_id, p.supplier_id, p.created_by, p.created_at, p.updated_at,
		coalesce(c.name::text, ''), coalesce(b.name::text, ''), coalesce(un.name::text, ''), coalesce(u.name, 'Unknown'),
		EXISTS (SELECT 1 FROM store_products sp JOIN stores s ON s.id = sp.store_id AND s.deleted_at IS NULL
		        WHERE sp.product_id = p.id AND sp.is_published AND ($2 = 0 OR sp.store_id = $2)),
		EXISTS (SELECT 1 FROM store_products sp WHERE sp.product_id = p.id AND sp.is_featured AND ($2 = 0 OR sp.store_id = $2)),
		EXISTS (SELECT 1 FROM store_products sp JOIN stores s ON s.id = sp.store_id AND s.deleted_at IS NULL
		        WHERE sp.product_id = p.id AND sp.is_published)
		FROM products p
		LEFT JOIN categories c ON c.id = p.category_id
		LEFT JOIN brands b ON b.id = p.brand_id
		LEFT JOIN units un ON un.id = p.unit_id
		LEFT JOIN users u ON u.id = p.created_by
		WHERE p.id = ANY($1) AND p.deleted_at IS NULL`
	qLoadVariants = `SELECT id, sku::text, item_barcode, price, cost, weight, attributes, to_char(expiry_date, 'YYYY-MM-DD'),
		to_char(manufactured_date, 'YYYY-MM-DD'), product_id, created_by, created_at, updated_at
		FROM product_variants WHERE product_id = ANY($1) AND deleted_at IS NULL ORDER BY id`
	qLoadInventories = `SELECT i.id, i.qty, i.quantity_alert, i.variant_id, i.store_id, i.created_at, i.updated_at
		FROM inventories i JOIN stores s ON s.id = i.store_id AND s.deleted_at IS NULL
		WHERE i.variant_id = ANY($1) AND ($2 = 0 OR i.store_id = $2) ORDER BY i.store_id`
	qLoadLocks = `SELECT product_id, user_id, locked_at FROM pos_locks
		WHERE store_id = $1 AND product_id = ANY($2) AND expires_at > now()`
)

// load returns products in the order of ids (missing or deleted ones are
// skipped). Four queries for any number of products: no N+1.
func load(ctx context.Context, tx tenancy.TenantTx, ids []int64, o loadOpts) ([]ProductJSON, error) {
	if len(ids) == 0 {
		return []ProductJSON{}, nil
	}
	byID := make(map[int64]*ProductJSON, len(ids))
	rows, err := tx.Query(ctx, qLoadProducts, ids, o.storeID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		p := &ProductJSON{ProductVariants: []VariantJSON{}}
		var anyPublished bool
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Status, &p.imageID, &p.Slug, &p.SellingType, &p.ProductType,
			&p.TaxType, &p.Tax, &p.HSNCode, &p.DiscountType, &p.DiscountValue, &p.Warranties, &p.BarcodeSymbology,
			&p.CategoryID, &p.SubCategoryID, &p.BrandID, &p.UnitID, &p.SupplierID, &p.CreatedByID, &p.CreatedAt, &p.UpdatedAt,
			&p.Category, &p.Brand, &p.Unit, &p.CreatedByName, &p.IsPublished, &p.IsFeatured, &anyPublished); err != nil {
			rows.Close()
			return nil, err
		}
		p.Image = imageURL(p.imageID, anyPublished)
		p.CategoryIDC, p.BrandIDC, p.UnitIDC = p.CategoryID, p.BrandID, p.UnitID
		p.SubCategoryIDC, p.SupplierIDC, p.CreatedByIDC = p.SubCategoryID, p.SupplierID, p.CreatedByID
		byID[p.ID] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	variantOwner := map[int64]*ProductJSON{}
	var variantIDs []int64
	rows, err = tx.Query(ctx, qLoadVariants, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v VariantJSON
		if err := rows.Scan(&v.ID, &v.SKU, &v.ItemBarcode, &v.Price, &v.Cost, &v.Weight, &v.Attributes, &v.ExpiryDate,
			&v.ManufacturedDate, &v.ProductID, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		if v.Attributes == nil {
			v.Attributes = map[string]any{}
		}
		v.Inventories = []InventoryJSON{}
		if p := byID[v.ProductID]; p != nil {
			p.ProductVariants = append(p.ProductVariants, v)
			variantOwner[v.ID] = p
			variantIDs = append(variantIDs, v.ID)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(variantIDs) > 0 {
		rows, err = tx.Query(ctx, qLoadInventories, variantIDs, o.storeID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var inv InventoryJSON
			if err := rows.Scan(&inv.ID, &inv.Qty, &inv.QuantityAlert, &inv.VariantID, &inv.StoreID, &inv.CreatedAt, &inv.UpdatedAt); err != nil {
				rows.Close()
				return nil, err
			}
			p := variantOwner[inv.VariantID]
			if p == nil {
				continue
			}
			for i := range p.ProductVariants {
				if p.ProductVariants[i].ID == inv.VariantID {
					p.ProductVariants[i].Inventories = append(p.ProductVariants[i].Inventories, inv)
				}
			}
			p.Qty += inv.Qty
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	if o.storeID > 0 {
		rows, err = tx.Query(ctx, qLoadLocks, o.storeID, ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var pid, uid int64
			var at time.Time
			if err := rows.Scan(&pid, &uid, &at); err != nil {
				rows.Close()
				return nil, err
			}
			if p := byID[pid]; p != nil {
				p.IsLocked, p.LockedAt, p.LockedBy = true, &at, &uid
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	out := make([]ProductJSON, 0, len(ids))
	for _, id := range ids {
		p := byID[id]
		if p == nil {
			continue
		}
		if len(p.ProductVariants) > 0 {
			p.Price = p.ProductVariants[0].Price
		}
		out = append(out, *p)
	}
	return out, nil
}

func loadOne(ctx context.Context, tx tenancy.TenantTx, id int64, o loadOpts) (ProductJSON, bool, error) {
	list, err := load(ctx, tx, []int64{id}, o)
	if err != nil || len(list) == 0 {
		return ProductJSON{}, false, err
	}
	return list[0], true, nil
}

// imageID is the type of a file reference column.
type imageID = uuid.UUID
