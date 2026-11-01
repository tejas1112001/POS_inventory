package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// UI S9: contacts, a notes timeline and a read-only overview on the
// tenant page. Everything here reads and writes the platform schema only;
// nothing opens a tenant schema.

// Contact is a person to reach at a tenant.
type Contact struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Email     string    `json:"email"`
	Phone     string    `json:"phone"`
	IsPrimary bool      `json:"isPrimary"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ContactInput is the POST/PUT body.
type ContactInput struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	IsPrimary bool   `json:"isPrimary"`
}

// Note is one entry of the notes timeline.
type Note struct {
	ID          int64     `json:"id"`
	Body        string    `json:"body"`
	AuthorEmail string    `json:"authorEmail"`
	AuthorName  string    `json:"authorName"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Notice is a billing or account notice sent to the tenant.
type Notice struct {
	Kind      string    `json:"kind"`
	Period    string    `json:"period"`
	CreatedAt time.Time `json:"createdAt"`
}

// Overview is the read-only health and usage summary of a tenant, from
// platform data only.
type Overview struct {
	PlanName          string          `json:"planName"`
	Limits            json.RawMessage `json:"limits"`
	StorageBytes      int64           `json:"storageBytes"`
	StorageLimitBytes int64           `json:"storageLimitBytes"` // 0: no limit
	LastActivityAt    *time.Time      `json:"lastActivityAt"`
	SchemaVersion     int64           `json:"schemaVersion"`
	LatestSchema      int64           `json:"latestSchema"`
	MailPendingSince  *time.Time      `json:"mailPendingSince"` // receipts waiting to be sent
	Notices           []Notice        `json:"notices"`
	ContactCount      int             `json:"contactCount"`
	NoteCount         int             `json:"noteCount"`
	Usage             *Usage          `json:"usage"` // nil until the worker's first snapshot
}

// Usage is the worker's snapshot of a tenant (counts only).
type Usage struct {
	Users            int       `json:"users"`
	Stores           int       `json:"stores"`
	Products         int       `json:"products"`
	ReceiptsSent7d   int       `json:"receiptsSent7d"`
	ReceiptsFailed7d int       `json:"receiptsFailed7d"`
	ReceiptsWaiting  int       `json:"receiptsWaiting"`
	TakenAt          time.Time `json:"takenAt"`
}

const (
	qContactCols = `id, name, role, coalesce(email::text, ''), coalesce(phone, ''), is_primary, created_at, updated_at`
	maxContacts  = 20
)

func cleanContact(in ContactInput) (ContactInput, error) {
	f := map[string]string{}
	in.Name, in.Role = strings.TrimSpace(in.Name), strings.TrimSpace(in.Role)
	in.Email, in.Phone = strings.TrimSpace(in.Email), strings.TrimSpace(in.Phone)
	if l := len([]rune(in.Name)); l < 1 || l > 120 || strings.ContainsAny(in.Name, "\r\n\x00") {
		f["name"] = "1-120 characters"
	}
	if len([]rune(in.Role)) > 60 || strings.ContainsAny(in.Role, "\r\n\x00") {
		f["role"] = "at most 60 characters"
	}
	if in.Email != "" && !validEmail(in.Email) {
		f["email"] = "must be a plain email address"
	}
	if len(in.Phone) > 40 || strings.ContainsAny(in.Phone, "\r\n\x00") {
		f["phone"] = "at most 40 characters"
	}
	if in.Email == "" && in.Phone == "" {
		f["email"] = "give an email or a phone"
	}
	if len(f) > 0 {
		return in, apperr.Validation(f)
	}
	return in, nil
}

func scanContact(row pgx.Row) (Contact, error) {
	var c Contact
	err := row.Scan(&c.ID, &c.Name, &c.Role, &c.Email, &c.Phone, &c.IsPrimary, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// tenantExists checks a live tenant id inside a platform transaction.
func tenantExists(ctx context.Context, tx tenancy.PlatformTx, id string) error {
	if !tenancy.ValidTenantID(id) {
		return apperr.NotFound()
	}
	var ok bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform.tenants WHERE id = $1 AND status <> 'deleted')`, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return apperr.NotFound()
	}
	return nil
}

// ListContacts returns a tenant's contacts, primary first.
func (s *Service) ListContacts(ctx context.Context, id string) ([]Contact, error) {
	out := []Contact{}
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		if err := tenantExists(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+qContactCols+` FROM platform.tenant_contacts WHERE tenant_id = $1 ORDER BY is_primary DESC, id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanContact(rows)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// SaveContact adds (cid 0) or changes a contact. A new primary contact
// takes the mark from the old one.
func (s *Service) SaveContact(ctx context.Context, a Actor, id string, cid int64, in ContactInput, m Meta) (Contact, error) {
	in, err := cleanContact(in)
	if err != nil {
		return Contact{}, err
	}
	var out Contact
	err = s.rw(ctx, func(tx tenancy.PlatformTx) error {
		if err := tenantExists(ctx, tx, id); err != nil {
			return err
		}
		if cid == 0 {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.tenant_contacts WHERE tenant_id = $1`, id).Scan(&n); err != nil {
				return err
			}
			if n >= maxContacts {
				return apperr.Invalid("name", "A tenant can have at most 20 contacts.")
			}
		}
		if in.IsPrimary {
			if _, err := tx.Exec(ctx, `UPDATE platform.tenant_contacts SET is_primary = false WHERE tenant_id = $1 AND is_primary AND id <> $2`, id, cid); err != nil {
				return err
			}
		}
		email, phone := nullIfBlank(in.Email), nullIfBlank(in.Phone)
		var err error
		action := "tenant.contact_add"
		if cid == 0 {
			out, err = scanContact(tx.QueryRow(ctx, `INSERT INTO platform.tenant_contacts (tenant_id, name, role, email, phone, is_primary, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, 0)) RETURNING `+qContactCols, id, in.Name, in.Role, email, phone, in.IsPrimary, a.UserID))
		} else {
			action = "tenant.contact_edit"
			out, err = scanContact(tx.QueryRow(ctx, `UPDATE platform.tenant_contacts SET name = $3, role = $4, email = $5, phone = $6,
					is_primary = $7, updated_at = now()
				WHERE tenant_id = $1 AND id = $2 RETURNING `+qContactCols, id, cid, in.Name, in.Role, email, phone, in.IsPrimary))
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		// The audit row names the contact, never its email or phone.
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: action, TargetTenant: id,
			Target: "contact:" + strconv.FormatInt(out.ID, 10), After: map[string]any{"name": out.Name, "role": out.Role, "primary": out.IsPrimary},
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return out, err
}

// DeleteContact removes a contact.
func (s *Service) DeleteContact(ctx context.Context, a Actor, id string, cid int64, m Meta) error {
	return s.rw(ctx, func(tx tenancy.PlatformTx) error {
		if err := tenantExists(ctx, tx, id); err != nil {
			return err
		}
		var name string
		err := tx.QueryRow(ctx, `DELETE FROM platform.tenant_contacts WHERE tenant_id = $1 AND id = $2 RETURNING name`, id, cid).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.contact_delete", TargetTenant: id,
			Target: "contact:" + strconv.FormatInt(cid, 10), Before: map[string]any{"name": name},
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
}

// ListNotes returns the newest notes first (at most 200).
func (s *Service) ListNotes(ctx context.Context, id string) ([]Note, error) {
	out := []Note{}
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		if err := tenantExists(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT n.id, n.body, coalesce(u.email::text, ''), coalesce(u.name, ''), n.created_at
			FROM platform.tenant_notes n LEFT JOIN platform.platform_users u ON u.id = n.author_id
			WHERE n.tenant_id = $1 ORDER BY n.created_at DESC, n.id DESC LIMIT 200`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n Note
			if err := rows.Scan(&n.ID, &n.Body, &n.AuthorEmail, &n.AuthorName, &n.CreatedAt); err != nil {
				return err
			}
			out = append(out, n)
		}
		return rows.Err()
	})
	return out, err
}

// AddNote appends a note. Notes cannot be edited or deleted.
func (s *Service) AddNote(ctx context.Context, a Actor, id, body string, m Meta) (Note, error) {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\x00", ""))
	if l := len([]rune(body)); l < 1 || l > 2000 {
		return Note{}, apperr.Validation(map[string]string{"body": "1-2000 characters"})
	}
	n := Note{Body: body, AuthorEmail: a.Email, AuthorName: a.Name}
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		if err := tenantExists(ctx, tx, id); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO platform.tenant_notes (tenant_id, author_id, body) VALUES ($1, $2, $3) RETURNING id, created_at`,
			id, a.UserID, body).Scan(&n.ID, &n.CreatedAt); err != nil {
			return err
		}
		// The note text stays in the notes table; the audit row only records it.
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.note_add", TargetTenant: id,
			Target: "note:" + strconv.FormatInt(n.ID, 10), After: map[string]any{"length": len([]rune(body))},
			IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return n, err
}

// TenantOverview reads the tenant's plan limits, storage, activity, schema
// version, pending mail and recent notices.
func (s *Service) TenantOverview(ctx context.Context, id string) (Overview, error) {
	o := Overview{Notices: []Notice{}, Limits: json.RawMessage(`{}`)}
	latest, err := migrate.LatestTenantVersion()
	if err != nil {
		return o, err
	}
	o.LatestSchema = latest
	err = s.ro(ctx, func(tx tenancy.PlatformTx) error {
		if err := tenantExists(ctx, tx, id); err != nil {
			return err
		}
		var limits []byte
		if err := tx.QueryRow(ctx, `SELECT coalesce(p.name, ''), coalesce(p.limits, '{}'::jsonb), t.storage_bytes, t.last_activity_at, t.schema_version
				FROM platform.tenants t LEFT JOIN platform.plans p ON p.id = t.plan_id WHERE t.id = $1`, id).
			Scan(&o.PlanName, &limits, &o.StorageBytes, &o.LastActivityAt, &o.SchemaVersion); err != nil {
			return err
		}
		o.Limits = limits
		var l struct {
			StorageMB int64 `json:"max_storage_mb"`
		}
		if json.Unmarshal(limits, &l) == nil && l.StorageMB > 0 {
			o.StorageLimitBytes = l.StorageMB << 20
		}
		err := tx.QueryRow(ctx, `SELECT pending_since FROM platform.mail_dispatch WHERE tenant_id = $1`, id).Scan(&o.MailPendingSince)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT kind, period_key, created_at FROM platform.notification_log WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT 10`, id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n Notice
			if err := rows.Scan(&n.Kind, &n.Period, &n.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			o.Notices = append(o.Notices, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM platform.tenant_contacts WHERE tenant_id = $1),
				(SELECT count(*) FROM platform.tenant_notes WHERE tenant_id = $1)`, id).Scan(&o.ContactCount, &o.NoteCount); err != nil {
			return err
		}
		var u Usage
		err = tx.QueryRow(ctx, `SELECT users, stores, products, receipts_sent_7d, receipts_failed_7d, receipts_waiting, taken_at
			FROM platform.tenant_usage WHERE tenant_id = $1`, id).
			Scan(&u.Users, &u.Stores, &u.Products, &u.ReceiptsSent7d, &u.ReceiptsFailed7d, &u.ReceiptsWaiting, &u.TakenAt)
		if err == nil {
			o.Usage = &u
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return nil
	})
	return o, err
}

func nullIfBlank(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ------------------------------------------------------------------ HTTP

func (h *HTTP) mountTenantExtras(p chi.Router) {
	p.With(h.need(PermTenantsView)).Get("/tenants/{id}/overview", h.tenantOverview)
	p.With(h.need(PermTenantsView)).Get("/tenants/{id}/contacts", h.listContacts)
	p.With(h.need(PermTenantsEdit)).Post("/tenants/{id}/contacts", h.saveContact)
	p.With(h.need(PermTenantsEdit)).Put("/tenants/{id}/contacts/{cid}", h.saveContact)
	p.With(h.need(PermTenantsEdit)).Delete("/tenants/{id}/contacts/{cid}", h.deleteContact)
	p.With(h.need(PermTenantsView)).Get("/tenants/{id}/notes", h.listNotes)
	p.With(h.need(PermTenantsEdit)).Post("/tenants/{id}/notes", h.addNote)
}

func contactID(r *http.Request) (int64, error) {
	s := chi.URLParam(r, "cid")
	if s == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound()
	}
	return id, nil
}

func (h *HTTP) tenantOverview(w http.ResponseWriter, r *http.Request) {
	o, err := h.svc.TenantOverview(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}

func (h *HTTP) listContacts(w http.ResponseWriter, r *http.Request) {
	l, err := h.svc.ListContacts(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (h *HTTP) saveContact(w http.ResponseWriter, r *http.Request) {
	cid, err := contactID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in ContactInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := h.svc.SaveContact(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), cid, in, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	code := http.StatusOK
	if cid == 0 {
		code = http.StatusCreated
	}
	httpx.WriteJSON(w, code, c)
}

func (h *HTTP) deleteContact(w http.ResponseWriter, r *http.Request) {
	cid, err := contactID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.DeleteContact(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), cid, h.meta(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HTTP) listNotes(w http.ResponseWriter, r *http.Request) {
	l, err := h.svc.ListNotes(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (h *HTTP) addNote(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Body string `json:"body"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	n, err := h.svc.AddNote(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in.Body, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, n)
}
