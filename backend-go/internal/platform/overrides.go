package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Limit overrides and feature flags (docs/S9_SECURITY_NOTE.md, approved):
// super_admin only (tenants:override), step-up MFA, a reason, a fixed list
// of keys with bounded values, an expiry (default 30 days, at most 366),
// append and revoke only, audited, and a notice to the platform inbox.

type overrideKey struct {
	Label string
	Flag  bool
	Cap   int64 // limits: hard upper bound
}

var overrideKeys = map[string]overrideKey{
	"max_stores":         {Label: "Stores", Cap: 100},
	"max_users":          {Label: "Users", Cap: 500},
	"max_products":       {Label: "Products", Cap: 1_000_000},
	"max_emails_per_day": {Label: "Emails a day", Cap: 10_000},
	"max_storage_mb":     {Label: "Storage (MB)", Cap: 100_000},
	"public_shop":        {Label: "Online shop", Flag: true},
	"csv_import":         {Label: "CSV import", Flag: true},
	"new_checkout":       {Label: "New checkout", Flag: true},
}

const (
	overrideDefaultDays = 30
	overrideMaxDays     = 366
	// ExpiringSoon is how long before the end an override is flagged.
	ExpiringSoon = 7 * 24 * time.Hour
	// OverrideNoticeTemplate is the platform inbox email of a change.
	OverrideNoticeTemplate = mailtmpl.OverrideNoticeTemplate
)

// Override is one row of a tenant's override history.
type Override struct {
	ID           int64           `json:"id"`
	Key          string          `json:"key"`
	Label        string          `json:"label"`
	Flag         bool            `json:"flag"`
	Value        json.RawMessage `json:"value"`
	Reason       string          `json:"reason"`
	ExpiresAt    time.Time       `json:"expiresAt"`
	CreatedAt    time.Time       `json:"createdAt"`
	CreatedBy    string          `json:"createdBy"`
	RevokedAt    *time.Time      `json:"revokedAt,omitempty"`
	RevokedBy    string          `json:"revokedBy,omitempty"`
	RevokeReason string          `json:"revokeReason,omitempty"`
	Active       bool            `json:"active"`
	ExpiringSoon bool            `json:"expiringSoon"`
}

// OverrideInput is the POST body.
type OverrideInput struct {
	Key    string          `json:"key"`
	Value  json.RawMessage `json:"value"`
	Reason string          `json:"reason"`
	Days   int             `json:"days"` // 0: 30
}

// OverrideKeyView lists the keys the console may offer.
type OverrideKeyView struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Flag  bool   `json:"flag"`
	Cap   int64  `json:"cap,omitempty"`
}

// OverrideKeys returns the fixed list, in a stable order.
func OverrideKeys() []OverrideKeyView {
	order := []string{"max_stores", "max_users", "max_products", "max_emails_per_day", "max_storage_mb", "public_shop", "csv_import", "new_checkout"}
	out := make([]OverrideKeyView, 0, len(order))
	for _, k := range order {
		d := overrideKeys[k]
		out = append(out, OverrideKeyView{Key: k, Label: d.Label, Flag: d.Flag, Cap: d.Cap})
	}
	return out
}

const qOverrideCols = `o.id, o.key, o.value, o.reason, o.expires_at, o.created_at, coalesce(c.email::text, ''),
	o.revoked_at, coalesce(r.email::text, ''), coalesce(o.revoke_reason, '')`

func scanOverride(row pgx.Row, now time.Time) (Override, error) {
	var o Override
	var raw []byte
	err := row.Scan(&o.ID, &o.Key, &raw, &o.Reason, &o.ExpiresAt, &o.CreatedAt, &o.CreatedBy, &o.RevokedAt, &o.RevokedBy, &o.RevokeReason)
	if err != nil {
		return o, err
	}
	o.Value = raw
	d := overrideKeys[o.Key]
	o.Label, o.Flag = d.Label, d.Flag
	o.Active = o.RevokedAt == nil && o.ExpiresAt.After(now)
	o.ExpiringSoon = o.Active && o.ExpiresAt.Sub(now) <= ExpiringSoon
	return o, nil
}

// ListOverrides returns a tenant's overrides, newest first (at most 100).
func (s *Service) ListOverrides(ctx context.Context, id string) ([]Override, error) {
	out := []Override{}
	now := s.now()
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		if err := tenantExists(ctx, tx, id); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+qOverrideCols+` FROM platform.tenant_overrides o
			LEFT JOIN platform.platform_users c ON c.id = o.created_by
			LEFT JOIN platform.platform_users r ON r.id = o.revoked_by
			WHERE o.tenant_id = $1 ORDER BY o.created_at DESC, o.id DESC LIMIT 100`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			o, err := scanOverride(rows, now)
			if err != nil {
				return err
			}
			out = append(out, o)
		}
		return rows.Err()
	})
	return out, err
}

// validValue checks a value against its key; limits are whole numbers from
// 1 to the cap and at most ten times the plan's own limit.
func validValue(key string, raw json.RawMessage, planLimit int64) (json.RawMessage, error) {
	d := overrideKeys[key]
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, apperr.Validation(map[string]string{"value": "a value is required"})
	}
	if d.Flag {
		b, ok := v.(bool)
		if !ok {
			return nil, apperr.Validation(map[string]string{"value": "on or off"})
		}
		return json.RawMessage(strconv.FormatBool(b)), nil
	}
	f, ok := v.(float64)
	if !ok || f != float64(int64(f)) || f < 1 {
		return nil, apperr.Validation(map[string]string{"value": "a whole number of at least 1"})
	}
	n, upper := int64(f), d.Cap
	if planLimit > 0 && planLimit*10 < upper {
		upper = planLimit * 10
	}
	if n > upper {
		return nil, apperr.Validation(map[string]string{"value": fmt.Sprintf("at most %d (10 × the plan, capped at %d)", upper, d.Cap)})
	}
	return json.RawMessage(strconv.FormatInt(n, 10)), nil
}

// AddOverride sets a key for a tenant; an open override of the same key
// is revoked as "replaced" in the same transaction.
func (s *Service) AddOverride(ctx context.Context, a Actor, id string, in OverrideInput, m Meta) (Override, error) {
	in.Key, in.Reason = strings.TrimSpace(in.Key), strings.TrimSpace(strings.ReplaceAll(in.Reason, "\x00", ""))
	f := map[string]string{}
	if _, ok := overrideKeys[in.Key]; !ok {
		f["key"] = "not an override key"
	}
	if l := len([]rune(in.Reason)); l < 10 || l > 500 {
		f["reason"] = "10-500 characters"
	}
	if in.Days == 0 {
		in.Days = overrideDefaultDays
	}
	if in.Days < 1 || in.Days > overrideMaxDays {
		f["days"] = fmt.Sprintf("1-%d days", overrideMaxDays)
	}
	if len(f) > 0 {
		return Override{}, apperr.Validation(f)
	}
	now := s.now()
	var out Override
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		if err := tenantExists(ctx, tx, id); err != nil {
			return err
		}
		var limits []byte
		if err := tx.QueryRow(ctx, `SELECT coalesce(p.limits, '{}'::jsonb) FROM platform.tenants t LEFT JOIN platform.plans p ON p.id = t.plan_id
			WHERE t.id = $1`, id).Scan(&limits); err != nil {
			return err
		}
		plan := map[string]float64{}
		_ = json.Unmarshal(limits, &plan)
		value, err := validValue(in.Key, in.Value, int64(plan[in.Key]))
		if err != nil {
			return err
		}
		var before []byte
		err = tx.QueryRow(ctx, `UPDATE platform.tenant_overrides SET revoked_at = now(), revoked_by = $3, revoke_reason = 'replaced'
			WHERE tenant_id = $1 AND key = $2 AND revoked_at IS NULL RETURNING value`, id, in.Key, a.UserID).Scan(&before)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		expires := now.Add(time.Duration(in.Days) * 24 * time.Hour)
		var oid int64
		if err := tx.QueryRow(ctx, `INSERT INTO platform.tenant_overrides (tenant_id, key, value, reason, expires_at, created_by, created_at)
			VALUES ($1, $2, $3::text::jsonb, $4, $5, $6, $7) RETURNING id`, id, in.Key, string(value), in.Reason, expires, a.UserID, now).Scan(&oid); err != nil {
			return err
		}
		out, err = scanOverride(tx.QueryRow(ctx, `SELECT `+qOverrideCols+` FROM platform.tenant_overrides o
			LEFT JOIN platform.platform_users c ON c.id = o.created_by
			LEFT JOIN platform.platform_users r ON r.id = o.revoked_by WHERE o.id = $1`, oid), now)
		if err != nil {
			return err
		}
		b := map[string]any{"key": in.Key}
		if before != nil {
			b["value"] = json.RawMessage(before)
		}
		if err := writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.override_set", TargetTenant: id,
			Target: "override:" + strconv.FormatInt(oid, 10), Before: b,
			After: map[string]any{"key": in.Key, "value": value, "expiresAt": expires, "reason": in.Reason},
			IP:    m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID}); err != nil {
			return err
		}
		return s.queueOverrideNotice(ctx, tx, oid, "set")
	})
	if err == nil && s.cfg.OverridesChanged != nil {
		s.cfg.OverridesChanged(id)
	}
	return out, err
}

// RevokeOverride ends an override now. A revoked override cannot be
// revoked again or reopened (the database refuses it).
func (s *Service) RevokeOverride(ctx context.Context, a Actor, id string, oid int64, reason string, m Meta) error {
	reason = strings.TrimSpace(strings.ReplaceAll(reason, "\x00", ""))
	if l := len([]rune(reason)); l < 10 || l > 500 {
		return apperr.Validation(map[string]string{"reason": "10-500 characters"})
	}
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		if err := tenantExists(ctx, tx, id); err != nil {
			return err
		}
		var key string
		var value []byte
		err := tx.QueryRow(ctx, `UPDATE platform.tenant_overrides SET revoked_at = now(), revoked_by = $3, revoke_reason = $4
			WHERE tenant_id = $1 AND id = $2 AND revoked_at IS NULL RETURNING key, value`, id, oid, a.UserID, reason).Scan(&key, &value)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.Conflict(apperr.CodeInvalidState, "This override is not open (already revoked or unknown).")
		}
		if err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.override_revoke", TargetTenant: id,
			Target: "override:" + strconv.FormatInt(oid, 10), Before: map[string]any{"key": key, "value": json.RawMessage(value)},
			After: map[string]any{"reason": reason}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID}); err != nil {
			return err
		}
		return s.queueOverrideNotice(ctx, tx, oid, "revoke")
	})
	if err == nil && s.cfg.OverridesChanged != nil {
		s.cfg.OverridesChanged(id)
	}
	return err
}

// queueOverrideNotice queues the platform inbox email (when configured).
// The row carries only the override id and the change; the worker reads
// the rest when it sends.
func (s *Service) queueOverrideNotice(ctx context.Context, tx tenancy.PlatformTx, oid int64, change string) error {
	if s.cfg.InboxEmail == "" {
		return nil
	}
	data, _ := json.Marshal(map[string]any{"overrideId": oid, "change": change})
	_, err := tx.Exec(ctx, `INSERT INTO platform.email_outbox (idempotency_key, kind, to_addr, template, template_data)
		VALUES ($1, 'platform_notice', $2, '`+mailtmpl.OverrideNoticeTemplate+`', $3::text::jsonb) ON CONFLICT (idempotency_key) DO NOTHING`,
		"override:"+strconv.FormatInt(oid, 10)+":"+change, s.cfg.InboxEmail, string(data))
	return err
}

// overrideCounts fills the active-override fields of tenant views.
func overrideCounts(ctx context.Context, tx tenancy.PlatformTx, views []TenantView, now time.Time) error {
	if len(views) == 0 {
		return nil
	}
	ids := make([]string, len(views))
	at := map[string]int{}
	for i, v := range views {
		ids[i], at[v.ID] = v.ID, i
	}
	rows, err := tx.Query(ctx, `SELECT tenant_id, count(*), min(expires_at) FROM platform.tenant_overrides
		WHERE tenant_id = ANY($1) AND revoked_at IS NULL AND expires_at > $2 GROUP BY tenant_id`, ids, now)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		var first time.Time
		if err := rows.Scan(&id, &n, &first); err != nil {
			return err
		}
		v := &views[at[id]]
		v.ActiveOverrides, v.OverridesExpiringSoon = n, first.Sub(now) <= ExpiringSoon
	}
	return rows.Err()
}

// ------------------------------------------------------------------ HTTP

func (h *HTTP) mountOverrides(p chi.Router) {
	p.With(h.need(PermTenantsView)).Get("/override-keys", h.overrideKeys)
	p.With(h.need(PermTenantsView)).Get("/tenants/{id}/overrides", h.listOverrides)
	p.With(h.need(PermTenantsOverride), h.stepUpRequired).Post("/tenants/{id}/overrides", h.addOverride)
	p.With(h.need(PermTenantsOverride), h.stepUpRequired).Post("/tenants/{id}/overrides/{oid}/revoke", h.revokeOverride)
}

func (h *HTTP) overrideKeys(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, OverrideKeys())
}

func (h *HTTP) listOverrides(w http.ResponseWriter, r *http.Request) {
	l, err := h.svc.ListOverrides(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, l)
}

func (h *HTTP) addOverride(w http.ResponseWriter, r *http.Request) {
	var in OverrideInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	o, err := h.svc.AddOverride(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), in, h.meta(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, o)
}

func (h *HTTP) revokeOverride(w http.ResponseWriter, r *http.Request) {
	oid, err := strconv.ParseInt(chi.URLParam(r, "oid"), 10, 64)
	if err != nil || oid <= 0 {
		httpx.WriteError(w, r, apperr.NotFound())
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.RevokeOverride(r.Context(), actorFrom(r.Context()), chi.URLParam(r, "id"), oid, in.Reason, h.meta(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
