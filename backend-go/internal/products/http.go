package products

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/inventory"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Deps are the products dependencies.
type Deps struct {
	Sessions *tenancy.Sessions
	Files    *files.Service
	Plans    *plans.Service
	MaxRows  int
}

// API holds the handlers.
type API struct{ d Deps }

// New creates the API.
func New(d Deps) *API { return &API{d: d} }

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

func pathID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound()
	}
	return id, nil
}

func (a *API) rw(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, fn)
}

func (a *API) ro(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, fn)
}

func queryInt(r *http.Request, name string) (int64, bool) {
	v, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get(name)), 10, 64)
	return v, err == nil && v > 0
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// ----------------------------------------------------------------- list

const (
	qProductFilter = ` FROM products p WHERE p.deleted_at IS NULL
		AND ($1 = '' OR p.name ILIKE $1 OR EXISTS (SELECT 1 FROM product_variants v WHERE v.product_id = p.id
		     AND v.deleted_at IS NULL AND (v.sku ILIKE $1 OR v.item_barcode ILIKE $1)))
		AND ($2 = 0 OR p.category_id = $2) AND ($3 = 0 OR p.brand_id = $3) AND ($4 = '' OR p.status = $4)`
	qProductCount = `SELECT count(*)` + qProductFilter
	qProductIDs   = `SELECT p.id` + qProductFilter + ` ORDER BY p.created_at DESC, p.id DESC LIMIT $5 OFFSET $6`
)

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	q := r.URL.Query()
	like := ""
	if s := strings.TrimSpace(q.Get("search")); s != "" {
		like = "%" + escapeLike(s) + "%"
	}
	cat, _ := queryInt(r, "categoryId")
	brand, _ := queryInt(r, "brandId")
	status := q.Get("status")
	if status != "Active" && status != "Inactive" {
		status = ""
	}
	var (
		out   []ProductJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qProductCount, like, cat, brand, status).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qProductIDs, like, cat, brand, status, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		out, err = load(r.Context(), tx, ids, loadOpts{})
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

// pos lists active products stocked in a store, with that store's
// inventory and lock state.
func (a *API) pos(w http.ResponseWriter, r *http.Request) {
	storeID, ok := queryInt(r, "storeId")
	if !ok {
		httpx.WriteError(w, r, apperr.Invalid("storeId", "storeId query parameter is required."))
		return
	}
	var out []ProductJSON
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		rows, err := tx.Query(r.Context(), `SELECT p.id FROM products p
			WHERE p.deleted_at IS NULL AND p.status = 'Active' AND EXISTS (
				SELECT 1 FROM product_variants v JOIN inventories i ON i.variant_id = v.id AND i.store_id = $1
				WHERE v.product_id = p.id AND v.deleted_at IS NULL)
			ORDER BY p.name, p.id LIMIT $2`, storeID, a.d.MaxRows)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		out, err = load(r.Context(), tx, ids, loadOpts{storeID: storeID})
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if out == nil {
		out = []ProductJSON{}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// barcode finds the product of a barcode or SKU (both unique per tenant).
func (a *API) barcode(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "barcode"))
	storeID, _ := queryInt(r, "storeId")
	var (
		out   ProductJSON
		found bool
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var pid int64
		err := tx.QueryRow(r.Context(), `SELECT v.product_id FROM product_variants v JOIN products p ON p.id = v.product_id
			WHERE v.deleted_at IS NULL AND p.deleted_at IS NULL AND p.status = 'Active' AND (v.item_barcode = $1 OR v.sku = $1::citext)
			ORDER BY (v.item_barcode = $1) DESC LIMIT 1`, code).Scan(&pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out, found, err = loadOne(r.Context(), tx, pid, loadOpts{storeID: storeID})
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !found {
		httpx.WriteError(w, r, apperr.NotFound())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- write

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var in productIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := validate(in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ch, err := a.d.Files.Prepare(r.Context(), tenant(r), "image", in.Image, nil, files.PurposeProductImage)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out ProductJSON
	uid := principal(r).UserID
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := a.d.Plans.Check(r.Context(), tx, plans.Products); err != nil {
			return err
		}
		if err := checkRefs(r.Context(), tx, c); err != nil {
			return err
		}
		var img *uuid.UUID
		if !ch.Keep() {
			var err error
			if img, err = ch.Apply(r.Context(), tx, nil, uid); err != nil {
				return err
			}
		}
		id, err := createProduct(r.Context(), tx, c, img, uid)
		if err != nil {
			return err
		}
		out, _, err = loadOne(r.Context(), tx, id, loadOpts{})
		return err
	})
	ch.Done(r.Context(), err)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in productIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if in.Variants == nil {
		httpx.WriteError(w, r, apperr.Invalid("variants", "Variants must be provided as an array."))
		return
	}
	c, err := validate(in)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var before *uuid.UUID
	if err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		return tx.QueryRow(r.Context(), `SELECT image_file_id FROM products WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&before)
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = apperr.NotFound()
		}
		httpx.WriteError(w, r, err)
		return
	}
	ch, err := a.d.Files.Prepare(r.Context(), tenant(r), "image", in.Image, before, files.PurposeProductImage)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out ProductJSON
	uid := principal(r).UserID
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := checkRefs(r.Context(), tx, c); err != nil {
			return err
		}
		var cur *uuid.UUID
		if err := tx.QueryRow(r.Context(), `SELECT image_file_id FROM products WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&cur); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.NotFound()
			}
			return err
		}
		img := cur
		if !ch.Keep() {
			var err error
			if img, err = ch.Apply(r.Context(), tx, cur, uid); err != nil {
				return err
			}
		}
		if err := updateProduct(r.Context(), tx, id, c, img, uid); err != nil {
			return err
		}
		var err error
		out, _, err = loadOne(r.Context(), tx, id, loadOpts{})
		return err
	})
	ch.Done(r.Context(), err)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// remove soft-deletes a product (C-12). It is refused while any variant
// still holds stock, so stock never disappears silently (P0-9).
func (a *API) remove(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	uid := principal(r).UserID
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var name string
		if err := tx.QueryRow(r.Context(), `SELECT name FROM products WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&name); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.NotFound()
			}
			return err
		}
		var stock int
		if err := tx.QueryRow(r.Context(), `SELECT coalesce(sum(i.qty), 0) FROM inventories i JOIN product_variants v ON v.id = i.variant_id
			WHERE v.product_id = $1 AND v.deleted_at IS NULL`, id).Scan(&stock); err != nil {
			return err
		}
		if stock > 0 {
			return apperr.Conflict(apperr.CodeInvalidState, "A product that still has stock cannot be deleted. Set its stock to 0 first.")
		}
		if _, err := tx.Exec(r.Context(), `UPDATE products SET deleted_at = now(), status = 'Inactive' WHERE id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE product_variants SET deleted_at = now() WHERE product_id = $1 AND deleted_at IS NULL`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE store_products SET is_published = false, is_featured = false, updated_at = now() WHERE product_id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `DELETE FROM pos_locks WHERE product_id = $1`, id); err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: uid, Action: "product.delete", Entity: "product",
			EntityID: strconv.FormatInt(id, 10), Before: map[string]any{"name": name}})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --------------------------------------------------------------- locks

type lockIn struct {
	Lock    bool      `json:"lock"`
	StoreID money.Int `json:"storeId"`
}

const (
	// Atomic: takes a free or expired lock, refreshes our own, or returns
	// no row when someone else holds it (C-15).
	qTakeLock = `INSERT INTO pos_locks (store_id, product_id, user_id, locked_at, expires_at)
		VALUES ($1, $2, $3, now(), now() + interval '2 minutes')
		ON CONFLICT (store_id, product_id) DO UPDATE
		SET user_id = EXCLUDED.user_id, locked_at = now(), expires_at = EXCLUDED.expires_at
		WHERE pos_locks.expires_at < now() OR pos_locks.user_id = EXCLUDED.user_id
		RETURNING user_id`
	qReleaseLock = `DELETE FROM pos_locks WHERE store_id = $1 AND product_id = $2 AND (user_id = $3 OR expires_at < now())`
	qLockHolder  = `SELECT EXISTS (SELECT 1 FROM pos_locks WHERE store_id = $1 AND product_id = $2 AND expires_at >= now())`
)

func (a *API) lock(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in lockIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !in.StoreID.Valid || in.StoreID.V <= 0 {
		httpx.WriteError(w, r, apperr.Invalid("storeId", "storeId is required."))
		return
	}
	store := in.StoreID.V
	uid := principal(r).UserID
	var out ProductJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var ok bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM products WHERE id = $1 AND deleted_at IS NULL)
			AND EXISTS (SELECT 1 FROM stores WHERE id = $2 AND deleted_at IS NULL)`, id, store).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return apperr.NotFound()
		}
		if in.Lock {
			var holder int64
			err := tx.QueryRow(r.Context(), qTakeLock, store, id, uid).Scan(&holder)
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.Conflict(apperr.CodeProductLocked, "Product is currently locked by another user.")
			}
			if err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(r.Context(), qReleaseLock, store, id, uid)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				var held bool
				if err := tx.QueryRow(r.Context(), qLockHolder, store, id).Scan(&held); err != nil {
					return err
				}
				if held {
					return apperr.Forbidden(apperr.CodeForbidden, "You cannot unlock a product locked by another user.")
				}
			}
		}
		var err error
		out, _, err = loadOne(r.Context(), tx, id, loadOpts{storeID: store})
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// ------------------------------------------------------------- variants

type addVariantIn struct {
	ProductID money.Int `json:"productId"`
	variantIn
}

// addVariant is POST /api/product-variants (now products:edit, C-08).
func (a *API) addVariant(w http.ResponseWriter, r *http.Request) {
	var in addVariantIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if !in.ProductID.Valid || in.ProductID.V <= 0 {
		httpx.WriteError(w, r, apperr.Invalid("productId", "productId is required."))
		return
	}
	f := fieldErrs{}
	in.ID, in.IDAlt = money.Int{}, money.Int{}
	cv, verr := validateVariant(f, "variant", in.variantIn)
	if verr != nil {
		httpx.WriteError(w, r, f.err())
		return
	}
	uid := principal(r).UserID
	var out VariantJSON
	err := a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var ptype string
		if err := tx.QueryRow(r.Context(), `SELECT product_type FROM products WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, in.ProductID.V).Scan(&ptype); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.Invalid("productId", "Product not found.")
			}
			return err
		}
		if ptype == "single" {
			return apperr.Invalid("productId", "A single product has exactly one variant.")
		}
		if err := checkRefs(r.Context(), tx, clean{variants: []cleanVariant{cv}}); err != nil {
			return err
		}
		vid, err := insertVariant(r.Context(), tx, in.ProductID.V, cv, uid, inventory.ReasonOpening)
		if err != nil {
			return err
		}
		p, _, err := loadOne(r.Context(), tx, in.ProductID.V, loadOpts{})
		if err != nil {
			return err
		}
		for _, v := range p.ProductVariants {
			if v.ID == vid {
				out = v
			}
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

// ---------------------------------------------------------------- mount

// Mount registers the product, inventory and showcase routes.
func (a *API) Mount(rt *rbac.Router) {
	req := rbac.Require
	rt.Handle(http.MethodGet, "/products", req("products", "view"), a.list)
	rt.Handle(http.MethodGet, "/products/pos", req("pos", "view"), a.pos)
	rt.Handle(http.MethodGet, "/products/barcode/{barcode}", req("pos", "view"), a.barcode)
	rt.Handle(http.MethodPost, "/products", req("products", "create").Uploads(), a.create)
	rt.Handle(http.MethodPut, "/products/{id}", req("products", "edit").Uploads(), a.update)
	rt.Handle(http.MethodDelete, "/products/{id}", req("products", "delete"), a.remove)
	rt.Handle(http.MethodPut, "/products/{id}/lock", req("pos", "create"), a.lock)
	rt.Handle(http.MethodPost, "/product-variants", req("products", "edit"), a.addVariant)

	rt.Handle(http.MethodGet, "/inventory", req("inventory", "view"), a.listInventory)
	rt.Handle(http.MethodPost, "/inventory", req("inventory", "edit"), a.setInventory)
	rt.Handle(http.MethodGet, "/stock-movements", req("inventory", "view"), a.listMovements)

	rt.Handle(http.MethodPost, "/stores/{id}/showcase/toggle-product", req("products", "edit"), a.toggleShowcase)
	rt.Handle(http.MethodPost, "/stores/{id}/showcase/batch-publish", req("products", "edit"), a.batchPublish)

	rt.Handle(http.MethodPost, "/products/import", rbac.Requirement{Kind: rbac.KindPermission, Perm: "products:import", BodyLimit: importBodyLimit}, a.importCSV)
	rt.Handle(http.MethodGet, "/products/import/template", req("products", "import"), a.importTemplate)
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
