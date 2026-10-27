package catalog

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// namedJSON is the category / brand shape: {id,name,status,image,...}.
type namedJSON struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Image  *string `json:"image"`
	stamp
	imageID *uuid.UUID
}

type namedIn struct {
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Image  *string `json:"image"`
	ignored
}

// namedStore runs one table's constant SQL.
type namedStore struct {
	label   string // "Category"
	purpose string
	list    func(ctx context.Context, tx tenancy.TenantTx, limit, offset int) ([]namedJSON, int, error)
	insert  func(ctx context.Context, tx tenancy.TenantTx, name, status string, img *uuid.UUID, by int64) (namedJSON, error)
	current func(ctx context.Context, tx tenancy.TenantTx, id int64) (*uuid.UUID, error) // locks the row
	update  func(ctx context.Context, tx tenancy.TenantTx, id int64, name, status string, img *uuid.UUID) (namedJSON, error)
	remove  func(ctx context.Context, tx tenancy.TenantTx, id int64) (bool, error)
}

func scanNamed(row pgx.Row) (namedJSON, error) {
	var n namedJSON
	err := row.Scan(&n.ID, &n.Name, &n.Status, &n.imageID, &n.CreatedBy, &n.CreatedByName, &n.CreatedAt, &n.UpdatedAt)
	return n, err
}

func collectNamed(rows pgx.Rows) ([]namedJSON, error) {
	defer rows.Close()
	var out []namedJSON
	for rows.Next() {
		n, err := scanNamed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s namedStore) withURL(n namedJSON) namedJSON {
	n.Image = files.URLPtr(n.imageID, s.purpose)
	return n
}

func (a *API) namedList(s namedStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := httpx.ParsePage(r, a.d.MaxRows)
		var (
			list  []namedJSON
			total int
		)
		err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
			var err error
			list, total, err = s.list(r.Context(), tx, p.Limit, p.Offset)
			return err
		})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		for i := range list {
			list[i] = s.withURL(list[i])
		}
		httpx.WriteList(w, p, list, total)
	}
}

func (a *API) namedCreate(s namedStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in namedIn
		if err := httpx.DecodeJSON(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		name, err := requiredText("name", in.Name, 120, s.label+" name is required.")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		ch, err := a.imageOp(r, "image", in.Image, nil, s.purpose)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var out namedJSON
		p := principal(r)
		err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
			var img *uuid.UUID
			if !ch.Keep() {
				var err error
				if img, err = ch.Apply(r.Context(), tx, nil, p.UserID); err != nil {
					return err
				}
			}
			var err error
			out, err = s.insert(r.Context(), tx, name, normStatus(in.Status), img, p.UserID)
			return err
		})
		ch.Done(r.Context(), err)
		if err != nil {
			writeErr(w, r, err, s.label)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, s.withURL(out))
	}
}

func (a *API) namedUpdate(s namedStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var in namedIn
		if err := httpx.DecodeJSON(r, &in); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		name, err := requiredText("name", in.Name, 120, s.label+" name is required.")
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var before *uuid.UUID
		// current() locks the row (FOR UPDATE), which needs a read-write tx.
		if err := a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
			var err error
			before, err = s.current(r.Context(), tx, id)
			return err
		}); err != nil {
			httpx.WriteError(w, r, notFound(err))
			return
		}
		ch, err := a.imageOp(r, "image", in.Image, before, s.purpose)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		var out namedJSON
		err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
			cur, err := s.current(r.Context(), tx, id)
			if err != nil {
				return notFound(err)
			}
			img := cur
			if !ch.Keep() {
				if img, err = ch.Apply(r.Context(), tx, cur, principal(r).UserID); err != nil {
					return err
				}
			}
			out, err = s.update(r.Context(), tx, id, name, normStatus(in.Status), img)
			return err
		})
		ch.Done(r.Context(), err)
		if err != nil {
			writeErr(w, r, err, s.label)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, s.withURL(out))
	}
}

func (a *API) namedDelete(s namedStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := idParam(r)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
			ok, err := s.remove(r.Context(), tx, id)
			if err == nil && !ok {
				return apperr.NotFound()
			}
			return err
		})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	return err
}

// ---------------------------------------------------------- categories

const (
	qCatCount = `SELECT count(*) FROM categories WHERE deleted_at IS NULL`
	qCatList  = `SELECT c.id, c.name::text, c.status, c.image_file_id, c.created_by, coalesce(u.name, 'Unknown'), c.created_at, c.updated_at
		FROM categories c LEFT JOIN users u ON u.id = c.created_by WHERE c.deleted_at IS NULL
		ORDER BY c.created_at DESC, c.id DESC LIMIT $1 OFFSET $2`
	qCatInsert = `WITH x AS (INSERT INTO categories (name, status, image_file_id, created_by) VALUES ($1, $2, $3, NULLIF($4, 0)) RETURNING *)
		SELECT x.id, x.name::text, x.status, x.image_file_id, x.created_by, coalesce(u.name, 'Unknown'), x.created_at, x.updated_at
		FROM x LEFT JOIN users u ON u.id = x.created_by`
	qCatCurrent = `SELECT image_file_id FROM categories WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	qCatUpdate  = `WITH x AS (UPDATE categories SET name = $2, status = $3, image_file_id = $4, updated_at = now() WHERE id = $1 RETURNING *)
		SELECT x.id, x.name::text, x.status, x.image_file_id, x.created_by, coalesce(u.name, 'Unknown'), x.created_at, x.updated_at
		FROM x LEFT JOIN users u ON u.id = x.created_by`
	qCatDelete = `UPDATE categories SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`
)

var categories = namedStore{
	label: "Category", purpose: files.PurposeCategoryImage,
	list: func(ctx context.Context, tx tenancy.TenantTx, limit, offset int) ([]namedJSON, int, error) {
		var total int
		if err := tx.QueryRow(ctx, qCatCount).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err := tx.Query(ctx, qCatList, limit, offset)
		if err != nil {
			return nil, 0, err
		}
		list, err := collectNamed(rows)
		return list, total, err
	},
	insert: func(ctx context.Context, tx tenancy.TenantTx, name, status string, img *uuid.UUID, by int64) (namedJSON, error) {
		return scanNamed(tx.QueryRow(ctx, qCatInsert, name, status, img, by))
	},
	current: func(ctx context.Context, tx tenancy.TenantTx, id int64) (*uuid.UUID, error) {
		var img *uuid.UUID
		err := tx.QueryRow(ctx, qCatCurrent, id).Scan(&img)
		return img, err
	},
	update: func(ctx context.Context, tx tenancy.TenantTx, id int64, name, status string, img *uuid.UUID) (namedJSON, error) {
		return scanNamed(tx.QueryRow(ctx, qCatUpdate, id, name, status, img))
	},
	remove: func(ctx context.Context, tx tenancy.TenantTx, id int64) (bool, error) {
		tag, err := tx.Exec(ctx, qCatDelete, id)
		return tag.RowsAffected() == 1, err
	},
}

// -------------------------------------------------------------- brands

const (
	qBrandCount = `SELECT count(*) FROM brands WHERE deleted_at IS NULL`
	qBrandList  = `SELECT b.id, b.name::text, b.status, b.image_file_id, b.created_by, coalesce(u.name, 'Unknown'), b.created_at, b.updated_at
		FROM brands b LEFT JOIN users u ON u.id = b.created_by WHERE b.deleted_at IS NULL
		ORDER BY b.created_at DESC, b.id DESC LIMIT $1 OFFSET $2`
	qBrandInsert = `WITH x AS (INSERT INTO brands (name, status, image_file_id, created_by) VALUES ($1, $2, $3, NULLIF($4, 0)) RETURNING *)
		SELECT x.id, x.name::text, x.status, x.image_file_id, x.created_by, coalesce(u.name, 'Unknown'), x.created_at, x.updated_at
		FROM x LEFT JOIN users u ON u.id = x.created_by`
	qBrandCurrent = `SELECT image_file_id FROM brands WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	qBrandUpdate  = `WITH x AS (UPDATE brands SET name = $2, status = $3, image_file_id = $4, updated_at = now() WHERE id = $1 RETURNING *)
		SELECT x.id, x.name::text, x.status, x.image_file_id, x.created_by, coalesce(u.name, 'Unknown'), x.created_at, x.updated_at
		FROM x LEFT JOIN users u ON u.id = x.created_by`
	qBrandDelete = `UPDATE brands SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`
)

var brands = namedStore{
	label: "Brand", purpose: files.PurposeBrandImage,
	list: func(ctx context.Context, tx tenancy.TenantTx, limit, offset int) ([]namedJSON, int, error) {
		var total int
		if err := tx.QueryRow(ctx, qBrandCount).Scan(&total); err != nil {
			return nil, 0, err
		}
		rows, err := tx.Query(ctx, qBrandList, limit, offset)
		if err != nil {
			return nil, 0, err
		}
		list, err := collectNamed(rows)
		return list, total, err
	},
	insert: func(ctx context.Context, tx tenancy.TenantTx, name, status string, img *uuid.UUID, by int64) (namedJSON, error) {
		return scanNamed(tx.QueryRow(ctx, qBrandInsert, name, status, img, by))
	},
	current: func(ctx context.Context, tx tenancy.TenantTx, id int64) (*uuid.UUID, error) {
		var img *uuid.UUID
		err := tx.QueryRow(ctx, qBrandCurrent, id).Scan(&img)
		return img, err
	},
	update: func(ctx context.Context, tx tenancy.TenantTx, id int64, name, status string, img *uuid.UUID) (namedJSON, error) {
		return scanNamed(tx.QueryRow(ctx, qBrandUpdate, id, name, status, img))
	},
	remove: func(ctx context.Context, tx tenancy.TenantTx, id int64) (bool, error) {
		tag, err := tx.Exec(ctx, qBrandDelete, id)
		return tag.RowsAffected() == 1, err
	},
}
