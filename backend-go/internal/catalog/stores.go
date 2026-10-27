package catalog

import (
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// storeJSON keeps the old Store.toJSON() keys.
type storeJSON struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	Address         string    `json:"address"`
	Status          string    `json:"status"`
	Logo            *string   `json:"logo"`
	Banner          *string   `json:"banner"`
	Description     string    `json:"description"`
	Phone           string    `json:"phone"`
	OpeningHours    string    `json:"openingHours"`
	IsPublicEnabled bool      `json:"isPublicEnabled"`
	CreatedBy       *int64    `json:"created_by"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
	logoID          *uuid.UUID
	bannerID        *uuid.UUID
}

const (
	qStoreCols = `id, name, slug::text, coalesce(address, ''), status, logo_file_id, banner_file_id, coalesce(description, ''),
		coalesce(phone, ''), coalesce(opening_hours, ''), is_public_enabled, created_by, created_at, updated_at`
	qStoreCount   = `SELECT count(*) FROM stores WHERE deleted_at IS NULL AND status = 'Active'`
	qStoreList    = `SELECT ` + qStoreCols + ` FROM stores WHERE deleted_at IS NULL AND status = 'Active' ORDER BY id LIMIT $1 OFFSET $2`
	qStoreGet     = `SELECT ` + qStoreCols + ` FROM stores WHERE id = $1 AND deleted_at IS NULL`
	qStoreGetLock = `SELECT ` + qStoreCols + ` FROM stores WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	qStoreInsert  = `INSERT INTO stores (name, slug, address, status, created_by) VALUES ($1, $2, $3, $4, NULLIF($5, 0)) RETURNING ` + qStoreCols
	qStoreUpdate  = `UPDATE stores SET name = $2, address = $3, status = $4, updated_at = now() WHERE id = $1 RETURNING ` + qStoreCols
	qStoreShow    = `UPDATE stores SET slug = $2, logo_file_id = $3, banner_file_id = $4, description = $5, phone = $6,
		opening_hours = $7, is_public_enabled = $8, updated_at = now() WHERE id = $1 RETURNING ` + qStoreCols
	qStoreSlugTaken = `SELECT EXISTS (SELECT 1 FROM stores WHERE slug = $1 AND deleted_at IS NULL AND id <> $2)`
	qStoreDelete    = `UPDATE stores SET deleted_at = now(), status = 'Inactive' WHERE id = $1 AND deleted_at IS NULL`
	qStoreLiveCount = `SELECT count(*) FROM stores WHERE deleted_at IS NULL`
)

func scanStore(row pgx.Row) (storeJSON, error) {
	var s storeJSON
	err := row.Scan(&s.ID, &s.Name, &s.Slug, &s.Address, &s.Status, &s.logoID, &s.bannerID, &s.Description,
		&s.Phone, &s.OpeningHours, &s.IsPublicEnabled, &s.CreatedBy, &s.CreatedAt, &s.UpdatedAt)
	s.Logo = files.URLPtr(s.logoID, files.PurposeStoreLogo)
	s.Banner = files.URLPtr(s.bannerID, files.PurposeStoreBanner)
	return s, err
}

var (
	storeSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$`)
	nonSlug          = regexp.MustCompile(`[^a-z0-9]+`)
)

// slugify makes a store slug from a name ("Main Store #2" → "main-store-2").
func slugify(name string) string {
	s := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 50 {
		s = strings.Trim(s[:50], "-")
	}
	if s == "" {
		s = "store"
	}
	return s
}

// uniqueSlug appends -2, -3, ... until the slug is free.
func uniqueSlug(ctx context.Context, tx tenancy.TenantTx, base string, selfID int64) (string, error) {
	slug := base
	for i := 2; i < 1000; i++ {
		var taken bool
		if err := tx.QueryRow(ctx, qStoreSlugTaken, slug, selfID).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return slug, nil
		}
		slug = base + "-" + strconv.Itoa(i)
	}
	return "", apperr.Conflict(apperr.CodeConflict, "Could not find a free store slug")
}

func (a *API) listStores(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	var (
		out   []storeJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qStoreCount).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qStoreList, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			s, err := scanStore(rows)
			if err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

func (a *API) getStore(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var s storeJSON
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		s, err = scanStore(tx.QueryRow(r.Context(), qStoreGet, id))
		return notFound(err)
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}

type storeIn struct {
	Name    *string `json:"name"`
	Address *string `json:"address"`
	Status  *string `json:"status"`
	ignored
}

func (a *API) createStore(w http.ResponseWriter, r *http.Request) {
	var in storeIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if in.Name == nil {
		httpx.WriteError(w, r, apperr.Invalid("name", "Store name is required."))
		return
	}
	name, err := requiredText("name", *in.Name, 120, "Store name is required.")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	addr := ""
	if in.Address != nil {
		if addr, err = optionalText("address", *in.Address, 500); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
	}
	status := "Active"
	if in.Status != nil {
		status = normStatus(*in.Status)
	}
	var out storeJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := a.d.Plans.Check(r.Context(), tx, plans.Stores); err != nil {
			return err
		}
		slug, err := uniqueSlug(r.Context(), tx, slugify(name), 0)
		if err != nil {
			return err
		}
		out, err = scanStore(tx.QueryRow(r.Context(), qStoreInsert, name, slug, nullIfEmpty(addr), status, principal(r).UserID))
		return err
	})
	if err != nil {
		writeErr(w, r, err, "Store")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) updateStore(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in storeIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out storeJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		cur, err := scanStore(tx.QueryRow(r.Context(), qStoreGetLock, id))
		if err != nil {
			return notFound(err)
		}
		// Missing fields keep their value (P2-9).
		name, addr, status := cur.Name, cur.Address, cur.Status
		if in.Name != nil {
			if name, err = requiredText("name", *in.Name, 120, "Store name is required."); err != nil {
				return err
			}
		}
		if in.Address != nil {
			if addr, err = optionalText("address", *in.Address, 500); err != nil {
				return err
			}
		}
		if in.Status != nil {
			status = normStatus(*in.Status)
		}
		out, err = scanStore(tx.QueryRow(r.Context(), qStoreUpdate, id, name, nullIfEmpty(addr), status))
		return err
	})
	if err != nil {
		writeErr(w, r, err, "Store")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteStore(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var live int
		if err := tx.QueryRow(r.Context(), qStoreLiveCount).Scan(&live); err != nil {
			return err
		}
		tag, err := tx.Exec(r.Context(), qStoreDelete, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound()
		}
		if live <= 1 {
			return apperr.Conflict(apperr.CodeInvalidState, "The last store cannot be deleted.")
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"message": "Store deleted successfully"})
}

// ------------------------------------------------------------ showcase

func (a *API) getShowcase(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var (
		s         storeJSON
		products  any = []any{}
		isDefault bool
		homePage  string
	)
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		if s, err = scanStore(tx.QueryRow(r.Context(), qStoreGet, id)); err != nil {
			return notFound(err)
		}
		def, _, found, err := settings.DefaultStore(r.Context(), tx)
		if err != nil {
			return err
		}
		isDefault = found && def == id
		if err := tx.QueryRow(r.Context(), `SELECT home_page FROM settings WHERE id = 1`).Scan(&homePage); err != nil {
			return err
		}
		if a.d.ShowcaseProducts != nil {
			products, err = a.d.ShowcaseProducts(r.Context(), tx, id)
		}
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// GET never writes any more: every store got its slug at creation (C-08).
	// isDefault: this store is the default public store (shown at / when
	// homePage is "shop").
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"store": s, "products": products, "isDefault": isDefault, "homePage": homePage})
}

// setDefault is POST /api/stores/{id}/showcase/default: make this store the
// default public store (settings.shop_store_id). Only an active store with
// its showcase turned on can be chosen.
func (a *API) setDefault(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		s, err := scanStore(tx.QueryRow(ctx, qStoreGetLock, id))
		if err != nil {
			return notFound(err)
		}
		if s.Status != "Active" || !s.IsPublicEnabled {
			return apperr.Invalid("store", "Only an active store with its public showcase turned on can be the default store.")
		}
		var before *int64
		if err := tx.QueryRow(ctx, `SELECT shop_store_id FROM settings WHERE id = 1 FOR UPDATE`).Scan(&before); err != nil {
			return err
		}
		if before != nil && *before == id {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE settings SET shop_store_id = $1, updated_at = now() WHERE id = 1`, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "settings.update", Entity: "settings",
			EntityID: "1", After: map[string]any{"fields": []string{"shopStoreId"}, "shopStoreId": id},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"isDefault": true, "storeId": id})
}

type showcaseIn struct {
	Slug            *string `json:"slug"`
	Logo            *string `json:"logo"`
	Banner          *string `json:"banner"`
	Description     *string `json:"description"`
	Phone           *string `json:"phone"`
	OpeningHours    *string `json:"openingHours"`
	IsPublicEnabled *bool   `json:"isPublicEnabled"`
	// The settings page also posts name and address; like the old backend,
	// the showcase endpoint ignores them (PUT /api/stores/{id} edits them).
	Name    any `json:"name"`
	Address any `json:"address"`
}

func (a *API) putShowcase(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in showcaseIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var cur storeJSON
	if err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		cur, err = scanStore(tx.QueryRow(r.Context(), qStoreGet, id))
		return notFound(err)
	}); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	slug := cur.Slug
	if in.Slug != nil && strings.TrimSpace(*in.Slug) != cur.Slug {
		slug = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(*in.Slug)), " ", "-")
		if !storeSlugPattern.MatchString(slug) || strings.Contains(slug, "--") {
			httpx.WriteError(w, r, apperr.Invalid("slug", "Shop URL may use lowercase letters, digits and single hyphens (max 60)."))
			return
		}
	}
	desc, phone, hours := cur.Description, cur.Phone, cur.OpeningHours
	for _, f := range []struct {
		name string
		in   *string
		out  *string
		max  int
	}{{"description", in.Description, &desc, 2000}, {"phone", in.Phone, &phone, 40}, {"openingHours", in.OpeningHours, &hours, 200}} {
		if f.in != nil {
			if *f.out, err = optionalText(f.name, *f.in, f.max); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
	}
	public := cur.IsPublicEnabled
	if in.IsPublicEnabled != nil {
		public = *in.IsPublicEnabled
	}
	logoCh, err := a.imageOp(r, "logo", in.Logo, cur.logoID, files.PurposeStoreLogo)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	bannerCh, err := a.imageOp(r, "banner", in.Banner, cur.bannerID, files.PurposeStoreBanner)
	if err != nil {
		logoCh.Done(r.Context(), err)
		httpx.WriteError(w, r, err)
		return
	}
	var out storeJSON
	p := principal(r)
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		locked, err := scanStore(tx.QueryRow(r.Context(), qStoreGetLock, id))
		if err != nil {
			return notFound(err)
		}
		var taken bool
		if err := tx.QueryRow(r.Context(), qStoreSlugTaken, slug, id).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return apperr.Conflict(apperr.CodeConflict, "This shop URL slug is already taken.")
		}
		logo, banner := locked.logoID, locked.bannerID
		if !logoCh.Keep() {
			if logo, err = logoCh.Apply(r.Context(), tx, locked.logoID, p.UserID); err != nil {
				return err
			}
		}
		if !bannerCh.Keep() {
			if banner, err = bannerCh.Apply(r.Context(), tx, locked.bannerID, p.UserID); err != nil {
				return err
			}
		}
		out, err = scanStore(tx.QueryRow(r.Context(), qStoreShow, id, slug, logo, banner, nullIfEmpty(desc), nullIfEmpty(phone), nullIfEmpty(hours), public))
		if err != nil {
			return err
		}
		return audit.Write(r.Context(), tx, audit.Entry{ActorID: p.UserID, Action: "store.showcase_update", Entity: "store",
			EntityID: strconv.FormatInt(id, 10), After: map[string]any{"slug": slug, "isPublicEnabled": public}})
	})
	logoCh.Done(r.Context(), err)
	bannerCh.Done(r.Context(), err)
	if err != nil {
		writeErr(w, r, err, "Store")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// --------------------------------------------------------------- files

func (a *API) uploadFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, httpx.UploadBodyLimit) // also capped by the route
	if err := r.ParseMultipartForm(4 << 20); err != nil {          //nolint:gosec // G120: body capped by MaxBytesReader above
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteError(w, r, apperr.TooLarge())
			return
		}
		httpx.WriteError(w, r, apperr.UnsupportedMedia())
		return
	}
	purpose := r.FormValue("purpose")
	allowed := false
	p := principal(r)
	for _, perm := range files.UploadPermissions[purpose] {
		allowed = allowed || p.Has(perm)
	}
	if _, known := files.UploadPermissions[purpose]; !known {
		httpx.WriteError(w, r, apperr.Invalid("purpose", "unknown purpose"))
		return
	}
	if !allowed {
		httpx.WriteError(w, r, apperr.Forbidden(apperr.CodeForbidden, "You do not have permission for this action"))
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, r, apperr.Invalid("file", "file is required"))
		return
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(io.LimitReader(f, files.MaxImageBytes+1))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id, err := a.d.Files.Upload(r.Context(), tenant(r), purpose, raw, p.UserID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]string{"id": id.String(), "url": files.URL(id, purpose)})
}

func (a *API) getFile(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	a.d.Files.ServeAuthed(w, r, chi.URLParam(r, "id"), p.Has)
}

// PublicFile serves GET /files/t/{id} (mounted outside /api).
func (a *API) PublicFile(w http.ResponseWriter, r *http.Request) {
	a.d.Files.ServePublic(w, r, chi.URLParam(r, "id"))
}

// ---------------------------------------------------------------- mount

// Mount registers the catalog routes.
func (a *API) Mount(rt *rbac.Router) {
	req := rbac.Require
	get, post, put, del := http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete

	for _, e := range []struct {
		path, module string
		s            namedStore
	}{{"/categories", "categories", categories}, {"/brands", "brands", brands}} {
		rt.Handle(get, e.path, req(e.module, "view"), a.namedList(e.s))
		rt.Handle(post, e.path, req(e.module, "create").Uploads(), a.namedCreate(e.s))
		rt.Handle(put, e.path+"/{id}", req(e.module, "edit").Uploads(), a.namedUpdate(e.s))
		rt.Handle(del, e.path+"/{id}", req(e.module, "delete"), a.namedDelete(e.s))
	}
	rt.Handle(get, "/sub-categories", req("categories", "view"), a.listSubCategories)
	rt.Handle(post, "/sub-categories", req("categories", "create"), a.createSubCategory)

	rt.Handle(get, "/units", req("units", "view"), a.listUnits)
	rt.Handle(post, "/units", req("units", "create"), a.createUnit)
	rt.Handle(put, "/units/{id}", req("units", "edit"), a.updateUnit)
	rt.Handle(del, "/units/{id}", req("units", "delete"), a.deleteUnit)

	rt.Handle(get, "/suppliers", req("suppliers", "view"), a.listSuppliers)
	rt.Handle(post, "/suppliers", req("suppliers", "create").Uploads(), a.createSupplier)
	rt.Handle(put, "/suppliers/{id}", req("suppliers", "edit").Uploads(), a.updateSupplier)
	rt.Handle(del, "/suppliers/{id}", req("suppliers", "delete"), a.deleteSupplier)

	rt.Handle(get, "/customers", req("customers", "view"), a.listCustomers)
	rt.Handle(post, "/customers", req("customers", "create").Uploads(), a.createCustomer)
	rt.Handle(put, "/customers/{id}", req("customers", "edit").Uploads(), a.updateCustomer)
	rt.Handle(del, "/customers/{id}", req("customers", "delete"), a.deleteCustomer)

	// Warehouses use the stores permissions (C-19, Q-12).
	rt.Handle(get, "/warehouses", req("stores", "view"), a.listWarehouses)
	rt.Handle(post, "/warehouses", req("stores", "create"), a.createWarehouse)
	rt.Handle(put, "/warehouses/{id}", req("stores", "edit"), a.updateWarehouse)
	rt.Handle(del, "/warehouses/{id}", req("stores", "delete"), a.deleteWarehouse)

	rt.Handle(get, "/stores", req("stores", "view"), a.listStores)
	rt.Handle(post, "/stores", req("stores", "create"), a.createStore)
	rt.Handle(get, "/stores/{id}", req("stores", "view"), a.getStore)
	rt.Handle(put, "/stores/{id}", req("stores", "edit"), a.updateStore)
	rt.Handle(del, "/stores/{id}", req("stores", "delete"), a.deleteStore)
	rt.Handle(get, "/stores/{id}/showcase", req("stores", "view"), a.getShowcase)
	rt.Handle(put, "/stores/{id}/showcase", req("stores", "edit").Uploads(), a.putShowcase)
	rt.Handle(post, "/stores/{id}/showcase/default", req("stores", "edit"), a.setDefault)

	rt.Handle(post, "/files", rbac.Authenticated().Uploads(), a.uploadFile)
	rt.Handle(get, "/files/{id}", rbac.Authenticated(), a.getFile)
}
