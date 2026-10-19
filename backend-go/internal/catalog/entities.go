package catalog

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// ================================================================ units

type unitJSON struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Status    string `json:"status"`
	stamp
}

type unitIn struct {
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Status    string `json:"status"`
	ignored
}

const (
	qUnitCols   = `x.id, x.name::text, x.short_name, x.status, x.created_by, coalesce(u.name, 'Unknown'), x.created_at, x.updated_at`
	qUnitCount  = `SELECT count(*) FROM units WHERE deleted_at IS NULL`
	qUnitList   = `SELECT ` + qUnitCols + ` FROM units x LEFT JOIN users u ON u.id = x.created_by WHERE x.deleted_at IS NULL ORDER BY x.created_at DESC, x.id DESC LIMIT $1 OFFSET $2`
	qUnitInsert = `WITH x AS (INSERT INTO units (name, short_name, status, created_by) VALUES ($1, $2, $3, NULLIF($4, 0)) RETURNING *) SELECT ` + qUnitCols + ` FROM x LEFT JOIN users u ON u.id = x.created_by`
	qUnitUpdate = `WITH x AS (UPDATE units SET name = $2, short_name = $3, status = $4, updated_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING *) SELECT ` + qUnitCols + ` FROM x LEFT JOIN users u ON u.id = x.created_by`
	qUnitDelete = `UPDATE units SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`
)

func scanUnit(row pgx.Row) (unitJSON, error) {
	var x unitJSON
	err := row.Scan(&x.ID, &x.Name, &x.ShortName, &x.Status, &x.CreatedBy, &x.CreatedByName, &x.CreatedAt, &x.UpdatedAt)
	return x, err
}

func (a *API) unitParse(r *http.Request) (unitIn, error) {
	var in unitIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return in, err
	}
	var err error
	if in.Name, err = requiredText("name", in.Name, 60, "Unit name is required."); err != nil {
		return in, err
	}
	if in.ShortName, err = optionalText("short_name", in.ShortName, 20); err != nil {
		return in, err
	}
	in.Status = normStatus(in.Status)
	return in, nil
}

func (a *API) listUnits(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	var (
		out   []unitJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qUnitCount).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qUnitList, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanUnit(rows)
			if err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

func (a *API) createUnit(w http.ResponseWriter, r *http.Request) {
	in, err := a.unitParse(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out unitJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = scanUnit(tx.QueryRow(r.Context(), qUnitInsert, in.Name, in.ShortName, in.Status, principal(r).UserID))
		return err
	})
	if err != nil {
		writeErr(w, r, err, "Unit")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) updateUnit(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in, err := a.unitParse(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out unitJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = scanUnit(tx.QueryRow(r.Context(), qUnitUpdate, id, in.Name, in.ShortName, in.Status))
		return notFound(err)
	})
	if err != nil {
		writeErr(w, r, err, "Unit")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteUnit(w http.ResponseWriter, r *http.Request) {
	a.softDelete(w, r, func(tx tenancy.TenantTx, id int64) (bool, error) {
		tag, err := tx.Exec(r.Context(), qUnitDelete, id)
		return tag.RowsAffected() == 1, err
	})
}

// softDelete runs a delete closure and answers 204 / 404.
func (a *API) softDelete(w http.ResponseWriter, r *http.Request, del func(tenancy.TenantTx, int64) (bool, error)) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ok, err := del(tx, id)
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

// ============================================================ suppliers

type supplierJSON struct {
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	Phone   string  `json:"phone"`
	Address string  `json:"address"`
	Image   *string `json:"image"`
	Status  string  `json:"status"`
	stamp
	imageID *uuid.UUID
}

type supplierIn struct {
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	Phone   string  `json:"phone"`
	Address string  `json:"address"`
	Image   *string `json:"image"`
	Status  string  `json:"status"`
	ignored
}

const (
	qSupCols   = `x.id, x.name, coalesce(x.email::text, ''), coalesce(x.phone, ''), coalesce(x.address, ''), x.image_file_id, x.status, x.created_by, coalesce(u.name, 'Unknown'), x.created_at, x.updated_at`
	qSupCount  = `SELECT count(*) FROM suppliers WHERE deleted_at IS NULL`
	qSupList   = `SELECT ` + qSupCols + ` FROM suppliers x LEFT JOIN users u ON u.id = x.created_by WHERE x.deleted_at IS NULL ORDER BY x.created_at DESC, x.id DESC LIMIT $1 OFFSET $2`
	qSupInsert = `WITH x AS (INSERT INTO suppliers (name, email, phone, address, image_file_id, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, 0)) RETURNING *) SELECT ` + qSupCols + ` FROM x LEFT JOIN users u ON u.id = x.created_by`
	qSupCurrent = `SELECT image_file_id FROM suppliers WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	qSupUpdate  = `WITH x AS (UPDATE suppliers SET name = $2, email = $3, phone = $4, address = $5, image_file_id = $6, status = $7, updated_at = now()
		WHERE id = $1 RETURNING *) SELECT ` + qSupCols + ` FROM x LEFT JOIN users u ON u.id = x.created_by`
	qSupDelete = `UPDATE suppliers SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`
)

func scanSupplier(row pgx.Row) (supplierJSON, error) {
	var x supplierJSON
	err := row.Scan(&x.ID, &x.Name, &x.Email, &x.Phone, &x.Address, &x.imageID, &x.Status, &x.CreatedBy, &x.CreatedByName, &x.CreatedAt, &x.UpdatedAt)
	x.Image = files.URLPtr(x.imageID, files.PurposeSupplierImage)
	return x, err
}

func parseSupplier(r *http.Request) (supplierIn, error) {
	var in supplierIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return in, err
	}
	var err error
	if in.Name, err = requiredText("name", in.Name, 120, "Supplier name is required."); err != nil {
		return in, err
	}
	if in.Email, err = optionalEmail(in.Email); err != nil {
		return in, err
	}
	if in.Phone, err = optionalText("phone", in.Phone, 40); err != nil {
		return in, err
	}
	if in.Address, err = optionalText("address", in.Address, 500); err != nil {
		return in, err
	}
	in.Status = normStatus(in.Status)
	return in, nil
}

func (a *API) listSuppliers(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	var (
		out   []supplierJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qSupCount).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qSupList, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanSupplier(rows)
			if err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

func (a *API) createSupplier(w http.ResponseWriter, r *http.Request) {
	in, err := parseSupplier(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ch, err := a.imageOp(r, "image", in.Image, nil, files.PurposeSupplierImage)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out supplierJSON
	uid := principal(r).UserID
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var img *uuid.UUID
		if !ch.Keep() {
			var err error
			if img, err = ch.Apply(r.Context(), tx, nil, uid); err != nil {
				return err
			}
		}
		var err error
		out, err = scanSupplier(tx.QueryRow(r.Context(), qSupInsert, in.Name, nullIfEmpty(in.Email), nullIfEmpty(in.Phone), nullIfEmpty(in.Address), img, in.Status, uid))
		return err
	})
	ch.Done(r.Context(), err)
	if err != nil {
		writeErr(w, r, err, "Supplier")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) updateSupplier(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in, err := parseSupplier(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var before *uuid.UUID
	if err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		return tx.QueryRow(r.Context(), `SELECT image_file_id FROM suppliers WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&before)
	}); err != nil {
		httpx.WriteError(w, r, notFound(err))
		return
	}
	ch, err := a.imageOp(r, "image", in.Image, before, files.PurposeSupplierImage)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out supplierJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var cur *uuid.UUID
		if err := tx.QueryRow(r.Context(), qSupCurrent, id).Scan(&cur); err != nil {
			return notFound(err)
		}
		img := cur
		if !ch.Keep() {
			var err error
			if img, err = ch.Apply(r.Context(), tx, cur, principal(r).UserID); err != nil {
				return err
			}
		}
		var err error
		out, err = scanSupplier(tx.QueryRow(r.Context(), qSupUpdate, id, in.Name, nullIfEmpty(in.Email), nullIfEmpty(in.Phone), nullIfEmpty(in.Address), img, in.Status))
		return err
	})
	ch.Done(r.Context(), err)
	if err != nil {
		writeErr(w, r, err, "Supplier")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteSupplier(w http.ResponseWriter, r *http.Request) {
	a.softDelete(w, r, func(tx tenancy.TenantTx, id int64) (bool, error) {
		tag, err := tx.Exec(r.Context(), qSupDelete, id)
		return tag.RowsAffected() == 1, err
	})
}

// ============================================================ customers

type customerJSON struct {
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	Phone   string  `json:"phone"`
	Address string  `json:"address"`
	City    string  `json:"city"`
	Country string  `json:"country"`
	Image   *string `json:"image"`
	Status  string  `json:"status"`
	stamp
	imageID *uuid.UUID
}

type customerIn struct {
	Name    string  `json:"name"`
	Email   string  `json:"email"`
	Phone   string  `json:"phone"`
	Address string  `json:"address"`
	City    string  `json:"city"`
	Country string  `json:"country"`
	Image   *string `json:"image"`
	Status  string  `json:"status"`
	ignored
}

const (
	qCusCols  = `x.id, x.name, coalesce(x.email::text, ''), coalesce(x.phone, ''), coalesce(x.address, ''), coalesce(x.city, ''), coalesce(x.country, ''), x.image_file_id, x.status, x.created_by, coalesce(u.name, 'Unknown'), x.created_at, x.updated_at`
	qCusCount = `SELECT count(*) FROM customers WHERE deleted_at IS NULL AND ($1 = '' OR name ILIKE $1 OR phone ILIKE $1 OR email::text ILIKE $1)`
	qCusList  = `SELECT ` + qCusCols + ` FROM customers x LEFT JOIN users u ON u.id = x.created_by
		WHERE x.deleted_at IS NULL AND ($3 = '' OR x.name ILIKE $3 OR x.phone ILIKE $3 OR x.email::text ILIKE $3)
		ORDER BY x.created_at DESC, x.id DESC LIMIT $1 OFFSET $2`
	qCusInsert = `WITH x AS (INSERT INTO customers (name, email, phone, address, city, country, image_file_id, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, 0)) RETURNING *) SELECT ` + qCusCols + ` FROM x LEFT JOIN users u ON u.id = x.created_by`
	qCusCurrent = `SELECT image_file_id FROM customers WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	qCusUpdate  = `WITH x AS (UPDATE customers SET name = $2, email = $3, phone = $4, address = $5, city = $6, country = $7, image_file_id = $8, status = $9, updated_at = now()
		WHERE id = $1 RETURNING *) SELECT ` + qCusCols + ` FROM x LEFT JOIN users u ON u.id = x.created_by`
	qCusDelete = `UPDATE customers SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL
		AND id IS DISTINCT FROM (SELECT default_customer_id FROM settings WHERE id = 1)`
)

func scanCustomer(row pgx.Row) (customerJSON, error) {
	var x customerJSON
	err := row.Scan(&x.ID, &x.Name, &x.Email, &x.Phone, &x.Address, &x.City, &x.Country, &x.imageID, &x.Status, &x.CreatedBy, &x.CreatedByName, &x.CreatedAt, &x.UpdatedAt)
	x.Image = files.URLPtr(x.imageID, files.PurposeCustomerImage)
	return x, err
}

func parseCustomer(r *http.Request) (customerIn, error) {
	var in customerIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return in, err
	}
	var err error
	if in.Name, err = requiredText("name", in.Name, 120, "Customer name is required."); err != nil {
		return in, err
	}
	if in.Email, err = optionalEmail(in.Email); err != nil {
		return in, err
	}
	for _, f := range []struct {
		name string
		v    *string
		max  int
	}{{"phone", &in.Phone, 40}, {"address", &in.Address, 500}, {"city", &in.City, 120}, {"country", &in.Country, 120}} {
		if *f.v, err = optionalText(f.name, *f.v, f.max); err != nil {
			return in, err
		}
	}
	in.Status = normStatus(in.Status)
	return in, nil
}

func likePattern(q string) string {
	if q == "" {
		return ""
	}
	return "%" + escapeLike(q) + "%"
}

func escapeLike(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\\' || r == '%' || r == '_' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}

func (a *API) listCustomers(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	like := likePattern(r.URL.Query().Get("search"))
	var (
		out   []customerJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qCusCount, like).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qCusList, p.Limit, p.Offset, like)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanCustomer(rows)
			if err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

func (a *API) createCustomer(w http.ResponseWriter, r *http.Request) {
	in, err := parseCustomer(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ch, err := a.imageOp(r, "image", in.Image, nil, files.PurposeCustomerImage)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out customerJSON
	uid := principal(r).UserID
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var img *uuid.UUID
		if !ch.Keep() {
			var err error
			if img, err = ch.Apply(r.Context(), tx, nil, uid); err != nil {
				return err
			}
		}
		var err error
		out, err = scanCustomer(tx.QueryRow(r.Context(), qCusInsert, in.Name, nullIfEmpty(in.Email), nullIfEmpty(in.Phone),
			nullIfEmpty(in.Address), nullIfEmpty(in.City), nullIfEmpty(in.Country), img, in.Status, uid))
		return err
	})
	ch.Done(r.Context(), err)
	if err != nil {
		writeErr(w, r, err, "Customer")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) updateCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in, err := parseCustomer(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var before *uuid.UUID
	if err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		return tx.QueryRow(r.Context(), `SELECT image_file_id FROM customers WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&before)
	}); err != nil {
		httpx.WriteError(w, r, notFound(err))
		return
	}
	ch, err := a.imageOp(r, "image", in.Image, before, files.PurposeCustomerImage)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out customerJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var cur *uuid.UUID
		if err := tx.QueryRow(r.Context(), qCusCurrent, id).Scan(&cur); err != nil {
			return notFound(err)
		}
		img := cur
		if !ch.Keep() {
			var err error
			if img, err = ch.Apply(r.Context(), tx, cur, principal(r).UserID); err != nil {
				return err
			}
		}
		var err error
		out, err = scanCustomer(tx.QueryRow(r.Context(), qCusUpdate, id, in.Name, nullIfEmpty(in.Email), nullIfEmpty(in.Phone),
			nullIfEmpty(in.Address), nullIfEmpty(in.City), nullIfEmpty(in.Country), img, in.Status))
		return err
	})
	ch.Done(r.Context(), err)
	if err != nil {
		writeErr(w, r, err, "Customer")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteCustomer(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		tag, err := tx.Exec(r.Context(), qCusDelete, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 1 {
			return nil
		}
		var isDefault bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM settings WHERE id = 1 AND default_customer_id = $1)`, id).Scan(&isDefault); err != nil {
			return err
		}
		if isDefault {
			return apperr.Conflict(apperr.CodeInvalidState, "The walk-in customer cannot be deleted.")
		}
		return apperr.NotFound()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// =========================================================== warehouses

type warehouseJSON struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Status  string `json:"status"`
	stamp
}

type warehouseIn struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Status  string `json:"status"`
	ignored
}

const (
	qWhCols   = `x.id, x.name, coalesce(x.address, ''), x.status, x.created_by, coalesce(u.name, 'Unknown'), x.created_at, x.updated_at`
	qWhCount  = `SELECT count(*) FROM warehouses WHERE deleted_at IS NULL AND status = 'Active'`
	qWhList   = `SELECT ` + qWhCols + ` FROM warehouses x LEFT JOIN users u ON u.id = x.created_by WHERE x.deleted_at IS NULL AND x.status = 'Active' ORDER BY x.id LIMIT $1 OFFSET $2`
	qWhInsert = `WITH x AS (INSERT INTO warehouses (name, address, status, created_by) VALUES ($1, $2, $3, NULLIF($4, 0)) RETURNING *) SELECT ` + qWhCols + ` FROM x LEFT JOIN users u ON u.id = x.created_by`
	qWhUpdate = `WITH x AS (UPDATE warehouses SET name = $2, address = $3, status = $4, updated_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING *) SELECT ` + qWhCols + ` FROM x LEFT JOIN users u ON u.id = x.created_by`
	qWhDelete = `UPDATE warehouses SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`
)

func scanWarehouse(row pgx.Row) (warehouseJSON, error) {
	var x warehouseJSON
	err := row.Scan(&x.ID, &x.Name, &x.Address, &x.Status, &x.CreatedBy, &x.CreatedByName, &x.CreatedAt, &x.UpdatedAt)
	return x, err
}

func parseWarehouse(r *http.Request) (warehouseIn, error) {
	var in warehouseIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return in, err
	}
	var err error
	if in.Name, err = requiredText("name", in.Name, 120, "Warehouse name is required."); err != nil {
		return in, err
	}
	if in.Address, err = optionalText("address", in.Address, 500); err != nil {
		return in, err
	}
	in.Status = normStatus(in.Status)
	return in, nil
}

func (a *API) listWarehouses(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	var (
		out   []warehouseJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qWhCount).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qWhList, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanWarehouse(rows)
			if err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

func (a *API) createWarehouse(w http.ResponseWriter, r *http.Request) {
	in, err := parseWarehouse(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out warehouseJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = scanWarehouse(tx.QueryRow(r.Context(), qWhInsert, in.Name, nullIfEmpty(in.Address), in.Status, principal(r).UserID))
		return err
	})
	if err != nil {
		writeErr(w, r, err, "Warehouse")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) updateWarehouse(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	in, err := parseWarehouse(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out warehouseJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = scanWarehouse(tx.QueryRow(r.Context(), qWhUpdate, id, in.Name, nullIfEmpty(in.Address), in.Status))
		return notFound(err)
	})
	if err != nil {
		writeErr(w, r, err, "Warehouse")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) deleteWarehouse(w http.ResponseWriter, r *http.Request) {
	a.softDelete(w, r, func(tx tenancy.TenantTx, id int64) (bool, error) {
		tag, err := tx.Exec(r.Context(), qWhDelete, id)
		return tag.RowsAffected() == 1, err
	})
}

// ======================================================= sub-categories

type subCategoryJSON struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	CategoryID int64  `json:"category_id"`
	stamp
	Category struct {
		Name string `json:"name"`
	} `json:"Category"`
}

type subCategoryIn struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	CategoryID any    `json:"categoryId"` // the old form sends a string from a <select>
	ignored
}

const (
	qSubCols  = `x.id, x.name::text, x.status, x.category_id, x.created_by, coalesce(u.name, 'Unknown'), x.created_at, x.updated_at, c.name::text`
	qSubCount = `SELECT count(*) FROM sub_categories x JOIN categories c ON c.id = x.category_id AND c.deleted_at IS NULL WHERE x.deleted_at IS NULL`
	qSubList  = `SELECT ` + qSubCols + ` FROM sub_categories x JOIN categories c ON c.id = x.category_id AND c.deleted_at IS NULL
		LEFT JOIN users u ON u.id = x.created_by WHERE x.deleted_at IS NULL ORDER BY c.name, x.name LIMIT $1 OFFSET $2`
	qSubInsert = `WITH x AS (INSERT INTO sub_categories (name, status, category_id, created_by)
		SELECT $1, $2, c.id, NULLIF($4, 0) FROM categories c WHERE c.id = $3 AND c.deleted_at IS NULL RETURNING *)
		SELECT ` + qSubCols + ` FROM x JOIN categories c ON c.id = x.category_id LEFT JOIN users u ON u.id = x.created_by`
)

func scanSub(row pgx.Row) (subCategoryJSON, error) {
	var x subCategoryJSON
	err := row.Scan(&x.ID, &x.Name, &x.Status, &x.CategoryID, &x.CreatedBy, &x.CreatedByName, &x.CreatedAt, &x.UpdatedAt, &x.Category.Name)
	return x, err
}

func (a *API) listSubCategories(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParsePage(r, a.d.MaxRows)
	var (
		out   []subCategoryJSON
		total int
	)
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(r.Context(), qSubCount).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), qSubList, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			x, err := scanSub(rows)
			if err != nil {
				return err
			}
			out = append(out, x)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteList(w, p, out, total)
}

func (a *API) createSubCategory(w http.ResponseWriter, r *http.Request) {
	var in subCategoryIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	name, err := requiredText("name", in.Name, 120, "Sub-category name is required.")
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	catID, ok := toID(in.CategoryID)
	if !ok {
		httpx.WriteError(w, r, apperr.Invalid("categoryId", "Category is required."))
		return
	}
	var out subCategoryJSON
	err = a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = scanSub(tx.QueryRow(r.Context(), qSubInsert, name, normStatus(in.Status), catID, principal(r).UserID))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Invalid("categoryId", "Category not found.")
		}
		return err
	})
	if err != nil {
		writeErr(w, r, err, "Sub-category")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}
