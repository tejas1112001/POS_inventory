package shop

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Variant is a public variant: no barcode, cost, weight or stock count.
type Variant struct {
	ID          int64           `json:"id"`
	SKU         string          `json:"sku"`
	Price       decimal.Decimal `json:"price"`
	Attributes  json.RawMessage `json:"attributes"`
	InStock     bool            `json:"inStock"`
	StockStatus string          `json:"stockStatus"`
}

// Product is the public product allow-list (THREAT_MODEL §3.10).
type Product struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Description   string          `json:"description"`
	Image         string          `json:"image"`
	Slug          string          `json:"slug"`
	ProductType   string          `json:"productType"`
	Category      string          `json:"category"`
	CategoryID    *int64          `json:"categoryId"`
	Brand         string          `json:"brand"`
	BrandID       *int64          `json:"brandId"`
	Unit          string          `json:"unit"`
	Price         decimal.Decimal `json:"price"`
	FinalPrice    decimal.Decimal `json:"finalPrice"`
	HasDiscount   bool            `json:"hasDiscount"`
	DiscountType  *string         `json:"discountType"`
	DiscountValue decimal.Decimal `json:"discountValue"`
	TaxType       *string         `json:"taxType"`
	Tax           decimal.Decimal `json:"tax"`
	Warranties    string          `json:"warranties"`
	IsPublished   bool            `json:"isPublished"`
	IsFeatured    bool            `json:"isFeatured"`
	InStock       bool            `json:"inStock"`
	StockStatus   string          `json:"stockStatus"`
	Variants      []Variant       `json:"variants"`
}

func stockStatus(in bool) string {
	if in {
		return "In Stock"
	}
	return "Out of Stock"
}

// finalPrice applies the product discount to the base price (the first
// variant's price), rounded to the currency.
func finalPrice(base decimal.Decimal, typ *string, value decimal.Decimal, currency string) (decimal.Decimal, bool) {
	if typ == nil || !value.IsPositive() {
		return base, false
	}
	var p decimal.Decimal
	switch *typ {
	case "Percentage":
		p = base.Sub(base.Mul(value).Div(decimal.NewFromInt(100)))
	case "Fixed":
		p = base.Sub(value)
	default:
		return base, false
	}
	return money.Round(decimal.Max(p, decimal.Zero), currency), true
}

// The published products of store $1, each with its first live variant's
// price and the discounted price used for sorting.
const qPublished = `SELECT p.id, p.name, p.created_at, sp.is_featured,
		CASE p.discount_type
			WHEN 'Percentage' THEN greatest(0, fv.price - fv.price * coalesce(p.discount_value, 0) / 100)
			WHEN 'Fixed' THEN greatest(0, fv.price - coalesce(p.discount_value, 0))
			ELSE fv.price END AS final
	FROM products p
	JOIN store_products sp ON sp.product_id = p.id AND sp.store_id = $1 AND sp.is_published
	JOIN LATERAL (SELECT v.price FROM product_variants v WHERE v.product_id = p.id AND v.deleted_at IS NULL
		ORDER BY v.id LIMIT 1) fv ON true
	WHERE p.deleted_at IS NULL AND p.status = 'Active'
		AND ($2 = '' OR p.name ILIKE $2) AND ($3 = 0 OR p.category_id = $3) AND ($4 = 0 OR p.brand_id = $4)
		AND (NOT $5 OR sp.is_featured)`

const (
	qPublishedCount = `SELECT count(*) FROM (` + qPublished + `) x`
	qPublishedIDs   = `SELECT x.id FROM (` + qPublished + `) x
		ORDER BY CASE WHEN $6 = 'price_asc' THEN x.final END ASC NULLS LAST,
			CASE WHEN $6 = 'price_desc' THEN x.final END DESC NULLS LAST,
			CASE WHEN $6 = 'name_asc' THEN x.name END ASC,
			x.created_at DESC, x.id DESC
		LIMIT $7 OFFSET $8`
	qLoad = `SELECT p.id, p.name, coalesce(p.description, ''), p.image_file_id, coalesce(p.slug::text, ''), p.product_type,
			coalesce(c.name::text, ''), c.id, coalesce(b.name::text, ''), b.id, coalesce(u.name::text, ''),
			p.discount_type, coalesce(p.discount_value, 0), p.tax_type, coalesce(p.tax_rate, 0), coalesce(p.warranties, ''),
			sp.is_featured
		FROM products p
		JOIN store_products sp ON sp.product_id = p.id AND sp.store_id = $1 AND sp.is_published
		LEFT JOIN categories c ON c.id = p.category_id AND c.deleted_at IS NULL
		LEFT JOIN brands b ON b.id = p.brand_id AND b.deleted_at IS NULL
		LEFT JOIN units u ON u.id = p.unit_id AND u.deleted_at IS NULL
		WHERE p.id = ANY($2) AND p.deleted_at IS NULL AND p.status = 'Active'`
	qVariants = `SELECT v.id, v.product_id, v.sku::text, v.price, v.attributes, coalesce(i.qty, 0) > 0
		FROM product_variants v LEFT JOIN inventories i ON i.variant_id = v.id AND i.store_id = $1
		WHERE v.product_id = ANY($2) AND v.deleted_at IS NULL ORDER BY v.product_id, v.id`
)

// load returns public products in the order of ids.
func load(ctx context.Context, tx tenancy.TenantTx, storeID int64, ids []int64, currency string) ([]Product, error) {
	out := []Product{}
	if len(ids) == 0 {
		return out, nil
	}
	byID := map[int64]*Product{}
	rows, err := tx.Query(ctx, qLoad, storeID, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		p := &Product{IsPublished: true, Variants: []Variant{}}
		var img *uuid.UUID
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &img, &p.Slug, &p.ProductType, &p.Category, &p.CategoryID,
			&p.Brand, &p.BrandID, &p.Unit, &p.DiscountType, &p.DiscountValue, &p.TaxType, &p.Tax, &p.Warranties,
			&p.IsFeatured); err != nil {
			rows.Close()
			return nil, err
		}
		p.Image = publicURL(img, files.PurposeProductImage)
		byID[p.ID] = p
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = tx.Query(ctx, qVariants, storeID, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v Variant
		var pid int64
		if err := rows.Scan(&v.ID, &pid, &v.SKU, &v.Price, &v.Attributes, &v.InStock); err != nil {
			rows.Close()
			return nil, err
		}
		v.StockStatus = stockStatus(v.InStock)
		if p := byID[pid]; p != nil {
			p.Variants = append(p.Variants, v)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		p := byID[id]
		if p == nil || len(p.Variants) == 0 {
			continue
		}
		p.Price = p.Variants[0].Price
		p.FinalPrice, p.HasDiscount = finalPrice(p.Price, p.DiscountType, p.DiscountValue, currency)
		for _, v := range p.Variants {
			p.InStock = p.InStock || v.InStock
		}
		p.StockStatus = stockStatus(p.InStock)
		out = append(out, *p)
	}
	return out, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func intParam(r *http.Request, name string, def int64) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get(name)), 10, 64)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

var sorts = map[string]bool{"newest": true, "name_asc": true, "price_asc": true, "price_desc": true}

// Page is one page of products.
type Page struct {
	Data       []Product  `json:"data"`
	Pagination Pagination `json:"pagination"`
}

// Pagination describes a page.
type Pagination struct {
	Total      int   `json:"total"`
	Page       int64 `json:"page"`
	Limit      int64 `json:"limit"`
	TotalPages int64 `json:"totalPages"`
}

type listQuery struct {
	search          string
	category, brand int64
	featured        bool
	sort            string
	limit, offset   int64
}

func page(ctx context.Context, tx tenancy.TenantTx, s store, q listQuery, currency string) ([]Product, int, error) {
	args := []any{s.id, q.search, q.category, q.brand, q.featured}
	var total int
	if err := tx.QueryRow(ctx, qPublishedCount, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.Query(ctx, qPublishedIDs, append(args, q.sort, q.limit, q.offset)...)
	if err != nil {
		return nil, 0, err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return nil, 0, err
	}
	out, err := load(ctx, tx, s.id, ids, currency)
	return out, total, err
}

// products is GET /api/public/shop/{slug}/products?page&limit(<=48)&search
// &categoryId&brandId&sort(newest|name_asc|price_asc|price_desc)&featured.
func (a *API) products(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := listQuery{category: intParam(r, "categoryId", 0), brand: intParam(r, "brandId", 0),
		featured: qs.Get("featured") == "true", sort: qs.Get("sort")}
	if !sorts[q.sort] {
		q.sort = "newest"
	}
	if s := []rune(strings.TrimSpace(qs.Get("search"))); len(s) > 0 {
		if len(s) > maxSearch {
			s = s[:maxSearch]
		}
		q.search = "%" + escapeLike(string(s)) + "%"
	}
	pg := intParam(r, "page", 1)
	q.limit = min(intParam(r, "limit", DefaultLimit), MaxLimit)
	pg = min(pg, 1_000_000/q.limit)
	q.offset = (pg - 1) * q.limit
	a.serve(w, r, true, func(tx tenancy.TenantTx, s store, b settings.Business) (any, error) {
		data, total, err := page(r.Context(), tx, s, q, b.Currency)
		if err != nil {
			return nil, err
		}
		return Page{Data: data, Pagination: Pagination{Total: total, Page: pg, Limit: q.limit,
			TotalPages: (int64(total) + q.limit - 1) / q.limit}}, nil
	})
}

// featured is GET /api/public/shop/{slug}/featured: up to 8, newest first.
func (a *API) featured(w http.ResponseWriter, r *http.Request) {
	a.serve(w, r, true, func(tx tenancy.TenantTx, s store, b settings.Business) (any, error) {
		data, _, err := page(r.Context(), tx, s, listQuery{featured: true, sort: "newest", limit: FeaturedLimit}, b.Currency)
		return data, err
	})
}

const (
	qByID = `SELECT p.id FROM products p
		JOIN store_products sp ON sp.product_id = p.id AND sp.store_id = $1 AND sp.is_published
		WHERE p.id = $2 AND p.deleted_at IS NULL AND p.status = 'Active'`
	qBySlug = `SELECT p.id FROM products p
		JOIN store_products sp ON sp.product_id = p.id AND sp.store_id = $1 AND sp.is_published
		WHERE p.slug = $2 AND p.deleted_at IS NULL AND p.status = 'Active'
		ORDER BY p.id LIMIT 1`
)

// ProductPage is a product with its shop.
type ProductPage struct {
	Product Product `json:"product"`
	Shop    Info    `json:"shop"`
}

// product is GET /api/public/shop/{slug}/products/{id or slug}: published
// in this store only (no development bypass, C-18).
func (a *API) product(w http.ResponseWriter, r *http.Request) {
	ref := chi.URLParam(r, "product")
	a.serve(w, r, true, func(tx tenancy.TenantTx, s store, b settings.Business) (any, error) {
		var id int64
		var err error
		if n, perr := strconv.ParseInt(ref, 10, 64); perr == nil && n > 0 {
			err = tx.QueryRow(r.Context(), qByID, s.id, n).Scan(&id)
		} else if len(ref) > 0 && len(ref) <= 200 {
			err = tx.QueryRow(r.Context(), qBySlug, s.id, ref).Scan(&id)
		} else {
			err = pgx.ErrNoRows
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.NotFound()
		}
		if err != nil {
			return nil, err
		}
		l, err := load(r.Context(), tx, s.id, []int64{id}, b.Currency)
		if err != nil {
			return nil, err
		}
		if len(l) == 0 {
			return nil, apperr.NotFound()
		}
		return ProductPage{Product: l[0], Shop: Info{Name: s.name, Slug: s.slug, Phone: str(s.phone), Address: str(s.address),
			OpeningHours: str(s.hours), IsPublicEnabled: true}}, nil
	})
}

// Category is one public category.
type Category struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

const qCategories = `SELECT DISTINCT c.id, c.name::text FROM categories c
	JOIN products p ON p.category_id = c.id AND p.deleted_at IS NULL AND p.status = 'Active'
	JOIN store_products sp ON sp.product_id = p.id AND sp.store_id = $1 AND sp.is_published
	WHERE c.deleted_at IS NULL AND c.status = 'Active'
	ORDER BY 2, 1`

// categories is GET /api/public/shop/{slug}/categories: only categories
// with published products in this store (was every category).
func (a *API) categories(w http.ResponseWriter, r *http.Request) {
	a.serve(w, r, true, func(tx tenancy.TenantTx, s store, _ settings.Business) (any, error) {
		rows, err := tx.Query(r.Context(), qCategories, s.id)
		if err != nil {
			return nil, err
		}
		out, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Category])
		if out == nil {
			out = []Category{}
		}
		return out, err
	})
}
