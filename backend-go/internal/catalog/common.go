// Package catalog implements master data: categories, sub-categories,
// brands, units, suppliers, customers, warehouses and stores (+ showcase),
// with the old JSON shapes (API_CONTRACT §2.2, §2.3, §2.9) and the new
// rules: permissions on every route, soft deletes (C-12), validated images
// stored as files (C-17), plan limits (C-20).
package catalog

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Deps are the catalog dependencies.
type Deps struct {
	Sessions *tenancy.Sessions
	Files    *files.Service
	Plans    *plans.Service
	MaxRows  int
	// ShowcaseProducts lists products for a store's showcase page (step 7).
	ShowcaseProducts func(ctx context.Context, tx tenancy.TenantTx, storeID int64) (any, error)
}

// API holds the catalog handlers.
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

func idParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
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

// normStatus keeps the old rule: anything but Inactive is Active.
func normStatus(s string) string {
	if s == "Inactive" {
		return "Inactive"
	}
	return "Active"
}

// requiredText trims and checks length; msg is the old error text.
func requiredText(field, v string, max int, msg string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", apperr.Invalid(field, msg)
	}
	if len([]rune(v)) > max || strings.ContainsAny(v, "\x00") {
		return "", apperr.Invalid(field, field+" must be at most "+strconv.Itoa(max)+" characters")
	}
	return v, nil
}

// optionalText trims; "" stays "".
func optionalText(field, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if len([]rune(v)) > max || strings.ContainsAny(v, "\x00") {
		return "", apperr.Invalid(field, field+" must be at most "+strconv.Itoa(max)+" characters")
	}
	return v, nil
}

func optionalEmail(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	a, err := mail.ParseAddress(v)
	if err != nil || a.Address != v || len(v) > 254 {
		return "", apperr.Invalid("email", "email must be a valid email address")
	}
	return v, nil
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isFK(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// nullIfEmpty turns "" into SQL NULL for optional text columns.
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// stamp holds the common audit columns of master data rows.
type stamp struct {
	CreatedBy     *int64    `json:"created_by"`
	CreatedByName string    `json:"createdBy,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// imageOp runs an image change around a write transaction.
func (a *API) imageOp(r *http.Request, field string, v *string, current *uuid.UUID, purpose string) (*files.Change, error) {
	return a.d.Files.Prepare(r.Context(), tenant(r), field, v, current, purpose)
}

// writeErr is httpx.WriteError with nicer messages for DB constraint errors.
func writeErr(w http.ResponseWriter, r *http.Request, err error, label string) {
	switch {
	case isUnique(err):
		err = apperr.Conflict(apperr.CodeConflict, label+" with this name already exists.")
	case isFK(err):
		err = apperr.Conflict(apperr.CodeConflict, "A referenced record does not exist.")
	}
	httpx.WriteError(w, r, err)
}

// ignored lists request fields the old frontend sends that the server
// derives itself (createdBy comes from the session).
type ignored struct {
	CreatedBy any `json:"createdBy"`
}

// toID accepts a positive integer as a JSON number or numeric string (old
// forms send <select> values as strings).
func toID(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		if x >= 1 && x == float64(int64(x)) {
			return int64(x), true
		}
	case string:
		id, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err == nil && id > 0 {
			return id, true
		}
	}
	return 0, false
}
