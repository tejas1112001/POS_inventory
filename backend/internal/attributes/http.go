package attributes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Module is the industry module that owns these routes (modules.ProductAttributes).
const Module = "product_attributes"

// API serves /api/attributes (A6).
type API struct{ sessions *tenancy.Sessions }

// New creates the API.
func New(s *tenancy.Sessions) *API { return &API{sessions: s} }

// Mount registers the routes, all in the product_attributes module.
func (a *API) Mount(rt *rbac.Router) {
	req := func(action string) rbac.Requirement { return rbac.Require("attributes", action).InModule(Module) }
	rt.Handle(http.MethodGet, "/attributes", req("view"), a.list)
	rt.Handle(http.MethodGet, "/attributes/{id}", req("view"), a.getOne)
	rt.Handle(http.MethodPost, "/attributes", req("create"), a.create)
	rt.Handle(http.MethodPut, "/attributes/{id}", req("edit"), a.update)
	rt.Handle(http.MethodDelete, "/attributes/{id}", req("delete"), a.remove)
}

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

func (a *API) rw(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, fn)
}

func (a *API) ro(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, fn)
}

func idParam(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound()
	}
	return id, nil
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	var out []Def
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = List(r.Context(), tx)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func byID(ctx context.Context, tx tenancy.TenantTx, id int64) (Def, error) {
	list, err := List(ctx, tx)
	if err != nil {
		return Def{}, err
	}
	for _, d := range list {
		if d.ID == id {
			return d, nil
		}
	}
	return Def{}, apperr.NotFound()
}

func (a *API) getOne(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out Def
	err = a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		out, err = byID(r.Context(), tx, id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// defIn is the POST/PUT body. Key, level and kind are fixed once created.
type defIn struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Level   string   `json:"level"`
	Kind    string   `json:"kind"`
	Choices []string `json:"choices"`
	Sort    *int     `json:"sort"`
	Status  string   `json:"status"`
}

var keyFromLabel = strings.NewReplacer(" ", "_", "-", "_", "/", "_", ".", "_")

// cleanDef validates a body. On create, key defaults from the label.
func cleanDef(in defIn, create bool) (Def, error) {
	f := map[string]string{}
	d := Def{Label: strings.TrimSpace(in.Label), Level: in.Level, Kind: in.Kind, Status: "Active", Choices: []string{}}
	if d.Label == "" || len([]rune(d.Label)) > 60 || strings.ContainsRune(d.Label, 0) {
		f["label"] = "Label is required (at most 60 characters)."
	}
	if create {
		d.Key = strings.ToLower(strings.TrimSpace(in.Key))
		if d.Key == "" {
			d.Key = keyFromLabel.Replace(strings.ToLower(d.Label))
		}
		if !KeyPattern.MatchString(d.Key) {
			f["key"] = "Key: a lower-case letter, then letters, digits or _ (at most 40)."
		}
		if d.Level != LevelProduct && d.Level != LevelVariant {
			f["level"] = "Level is product or variant."
		}
		if d.Kind != KindChoice && d.Kind != KindText {
			f["kind"] = "Kind is choice or text."
		}
	}
	if in.Status == "Inactive" {
		d.Status = "Inactive"
	}
	if in.Sort != nil {
		if *in.Sort < 0 || *in.Sort > 10000 {
			f["sort"] = "Sort is between 0 and 10000."
		}
		d.Sort = *in.Sort
	}
	seen := map[string]bool{}
	for _, c := range in.Choices {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if len([]rune(c)) > MaxValue || strings.ContainsRune(c, 0) {
			f["choices"] = fmt.Sprintf("A choice is at most %d characters.", MaxValue)
			continue
		}
		if seen[strings.ToLower(c)] {
			f["choices"] = fmt.Sprintf("%q is listed twice.", c)
			continue
		}
		seen[strings.ToLower(c)] = true
		d.Choices = append(d.Choices, c)
	}
	if len(d.Choices) > MaxChoices {
		f["choices"] = fmt.Sprintf("At most %d choices.", MaxChoices)
	}
	return d, apperr.Fields(f)
}

const (
	qInsertDef = `INSERT INTO attribute_defs (key, label, level, kind, choices, sort, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, 0)) RETURNING id`
	qLockDef   = `SELECT key::text, kind FROM attribute_defs WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	qUpdateDef = `UPDATE attribute_defs SET label = $2, choices = $3, sort = $4, status = $5, updated_at = now() WHERE id = $1`
	qDeleteDef = `UPDATE attribute_defs SET deleted_at = now(), updated_at = now() WHERE id = $1`
	// qChoiceUse counts live variants (dimension) and products (product
	// level) whose value is the choice, case-insensitively.
	qChoiceUse = `SELECT
		(SELECT count(*) FROM product_variants v JOIN products p ON p.id = v.product_id
		  WHERE v.deleted_at IS NULL AND p.deleted_at IS NULL AND p.variant_dims @> ARRAY[$1::text]
		    AND lower(v.attributes ->> $1) = lower($2))
		+ (SELECT count(*) FROM products p WHERE p.deleted_at IS NULL AND lower(p.attributes ->> $1) = lower($2))`
	qKeyUse = `SELECT count(*) FROM products p WHERE p.deleted_at IS NULL
		AND (p.variant_dims @> ARRAY[$1::text] OR p.attributes ? $1)`
)

func choicesJSON(c []string) string {
	b, _ := json.Marshal(c)
	return string(b)
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var in defIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	d, err := cleanDef(in, true)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if d.Kind == KindText {
		d.Choices = []string{}
	} else if len(d.Choices) == 0 {
		httpx.WriteError(w, r, apperr.Invalid("choices", "A choice attribute needs at least one choice."))
		return
	}
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	var out Def
	err = a.rw(ctx, tenant(r), func(tx tenancy.TenantTx) error {
		if err := tx.QueryRow(ctx, qInsertDef, d.Key, d.Label, d.Level, d.Kind, choicesJSON(d.Choices), d.Sort, d.Status, p.UserID).Scan(&d.ID); err != nil {
			if isUnique(err) {
				return apperr.Conflict(apperr.CodeConflict, "An attribute with this key already exists.")
			}
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "attribute.create", Entity: "attribute_def",
			EntityID: strconv.FormatInt(d.ID, 10), After: map[string]any{"key": d.Key, "label": d.Label, "level": d.Level,
				"kind": d.Kind, "choices": d.Choices}, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		out, err = byID(ctx, tx, d.ID)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in defIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	d, err := cleanDef(in, false)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	var out Def
	err = a.rw(ctx, tenant(r), func(tx tenancy.TenantTx) error {
		var key, kind string
		if err := tx.QueryRow(ctx, qLockDef, id).Scan(&key, &kind); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.NotFound()
			}
			return err
		}
		before, err := byID(ctx, tx, id)
		if err != nil {
			return err
		}
		if (in.Key != "" && !strings.EqualFold(in.Key, key)) || (in.Level != "" && in.Level != before.Level) || (in.Kind != "" && in.Kind != kind) {
			return apperr.Invalid("key", "The key, level and kind of an attribute cannot be changed. Create a new attribute instead.")
		}
		if kind == KindText {
			d.Choices = []string{}
		} else if len(d.Choices) == 0 {
			return apperr.Invalid("choices", "A choice attribute needs at least one choice.")
		}
		if in.Sort == nil {
			d.Sort = before.Sort
		}
		// A removed (or renamed) choice must not be in use.
		kept := map[string]bool{}
		for _, c := range d.Choices {
			kept[strings.ToLower(c)] = true
		}
		for _, c := range before.Choices {
			if kept[strings.ToLower(c)] {
				continue
			}
			var n int
			if err := tx.QueryRow(ctx, qChoiceUse, key, c).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				e := apperr.Conflict(apperr.CodeConflict, fmt.Sprintf("%q is used by %d variants or products; it cannot be removed or renamed while in use.", c, n))
				e.Details = map[string]any{"choice": c, "count": n}
				return e
			}
		}
		if _, err := tx.Exec(ctx, qUpdateDef, id, d.Label, choicesJSON(d.Choices), d.Sort, d.Status); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "attribute.update", Entity: "attribute_def",
			EntityID:  strconv.FormatInt(id, 10),
			Before:    map[string]any{"label": before.Label, "choices": before.Choices, "sort": before.Sort, "status": before.Status},
			After:     map[string]any{"label": d.Label, "choices": d.Choices, "sort": d.Sort, "status": d.Status},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		out, err = byID(ctx, tx, id)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) remove(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	err = a.rw(ctx, tenant(r), func(tx tenancy.TenantTx) error {
		var key, kind string
		if err := tx.QueryRow(ctx, qLockDef, id).Scan(&key, &kind); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.NotFound()
			}
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, qKeyUse, key).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			e := apperr.Conflict(apperr.CodeConflict, fmt.Sprintf("This attribute is used by %d products; it cannot be deleted while in use. Set it to Inactive instead.", n))
			e.Details = map[string]any{"count": n}
			return e
		}
		if _, err := tx.Exec(ctx, qDeleteDef, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "attribute.delete", Entity: "attribute_def",
			EntityID: strconv.FormatInt(id, 10), Before: map[string]any{"key": key},
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()})
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"message": "Attribute deleted"})
}
