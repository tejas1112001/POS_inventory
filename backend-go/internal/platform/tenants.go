package platform

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// SlugRedirectDays is how long an old slug redirects and stays reserved.
const SlugRedirectDays = 365

// TenantView is the console's tenant shape. It never includes secrets.
type TenantView struct {
	ID             string     `json:"id"`
	Slug           string     `json:"slug"`
	Name           string     `json:"name"`
	Status         string     `json:"status"`
	Plan           string     `json:"plan,omitempty"`
	OwnerEmail     string     `json:"ownerEmail"`
	BillingEmail   string     `json:"billingEmail,omitempty"`
	Notes          string     `json:"notes,omitempty"`
	SchemaVersion  int64      `json:"schemaVersion"`
	StorageBytes   int64      `json:"storageBytes"`
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
	SuspendedAt    *time.Time `json:"suspendedAt,omitempty"`
	SuspendReason  string     `json:"suspendReason,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	Subscription   *SubView   `json:"subscription,omitempty"`
	Provisioning   *ProvView  `json:"provisioning,omitempty"`
}

// SubView summarizes the subscription.
type SubView struct {
	Status           string     `json:"status"`
	TrialEnd         *time.Time `json:"trialEnd,omitempty"`
	CurrentPeriodEnd *time.Time `json:"currentPeriodEnd,omitempty"`
}

// ProvView summarizes the latest provisioning run.
type ProvView struct {
	Status    string `json:"status"`
	Step      int    `json:"step"`
	Attempts  int    `json:"attempts"`
	LastError string `json:"lastError,omitempty"`
}

const qTenantCols = `t.id, t.slug::text, t.name, t.status, coalesce(p.code, ''), t.owner_email::text,
	coalesce(t.billing_email::text, ''), coalesce(t.notes, ''), t.schema_version, t.storage_bytes,
	t.last_activity_at, t.suspended_at, coalesce(t.suspend_reason, ''), t.created_at`

func scanTenant(row pgx.Row) (TenantView, error) {
	var v TenantView
	err := row.Scan(&v.ID, &v.Slug, &v.Name, &v.Status, &v.Plan, &v.OwnerEmail, &v.BillingEmail, &v.Notes,
		&v.SchemaVersion, &v.StorageBytes, &v.LastActivityAt, &v.SuspendedAt, &v.SuspendReason, &v.CreatedAt)
	return v, err
}

// ListFilter filters the tenant list.
type ListFilter struct {
	Status string
	Q      string
	Limit  int
	Offset int
}

// ListTenants returns a page of tenants and the total count.
func (s *Service) ListTenants(ctx context.Context, f ListFilter) ([]TenantView, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	like := ""
	if q := strings.TrimSpace(f.Q); q != "" {
		like = "%" + escapeLike(strings.ToLower(q)) + "%"
	}
	var (
		out   []TenantView
		total int
	)
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM platform.tenants t
			WHERE ($1 = '' OR t.status = $1) AND ($2 = '' OR lower(t.slug::text) LIKE $2 OR lower(t.name) LIKE $2 OR lower(t.owner_email::text) LIKE $2)`,
			f.Status, like).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+qTenantCols+` FROM platform.tenants t LEFT JOIN platform.plans p ON p.id = t.plan_id
			WHERE ($1 = '' OR t.status = $1) AND ($2 = '' OR lower(t.slug::text) LIKE $2 OR lower(t.name) LIKE $2 OR lower(t.owner_email::text) LIKE $2)
			ORDER BY t.created_at DESC, t.id LIMIT $3 OFFSET $4`, f.Status, like, f.Limit, f.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanTenant(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, total, err
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// GetTenant returns one tenant with subscription and provisioning status.
func (s *Service) GetTenant(ctx context.Context, id string) (TenantView, error) {
	if !tenancy.ValidTenantID(id) {
		return TenantView{}, apperr.NotFound()
	}
	var v TenantView
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		var err error
		v, err = scanTenant(tx.QueryRow(ctx, `SELECT `+qTenantCols+` FROM platform.tenants t
			LEFT JOIN platform.plans p ON p.id = t.plan_id WHERE t.id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		var sub SubView
		err = tx.QueryRow(ctx, `SELECT status, trial_end, current_period_end FROM platform.subscriptions WHERE tenant_id = $1`, id).
			Scan(&sub.Status, &sub.TrialEnd, &sub.CurrentPeriodEnd)
		if err == nil {
			v.Subscription = &sub
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var pv ProvView
		err = tx.QueryRow(ctx, `SELECT status, step, attempts, coalesce(last_error, '') FROM platform.provisioning_runs
			WHERE tenant_id = $1 ORDER BY id DESC LIMIT 1`, id).Scan(&pv.Status, &pv.Step, &pv.Attempts, &pv.LastError)
		if err == nil {
			v.Provisioning = &pv
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return nil
	})
	return v, err
}

// CreateTenantInput is POST /tenants.
type CreateTenantInput struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	OwnerEmail string `json:"ownerEmail"`
	OwnerName  string `json:"ownerName"`
	PlanCode   string `json:"planCode"`
	Currency   string `json:"currency"`
	Locale     string `json:"locale"`
	Timezone   string `json:"timezone"`
}

func (s *Service) provisioner() (*provisioning.Orchestrator, error) {
	if s.cfg.Provisioner == nil {
		return nil, apperr.Unavailable()
	}
	return s.cfg.Provisioner, nil
}

// CreateTenant provisions a tenant (Q-5: only the Super Admin creates them).
func (s *Service) CreateTenant(ctx context.Context, a Actor, in CreateTenantInput, m Meta) (provisioning.Result, error) {
	p, err := s.provisioner()
	if err != nil {
		return provisioning.Result{}, err
	}
	if s.slugInHistory(ctx, strings.ToLower(strings.TrimSpace(in.Slug))) {
		return provisioning.Result{}, apperr.Conflict(apperr.CodeConflict, "Slug is already taken")
	}
	res, perr := p.Provision(ctx, provisioning.Request{
		Slug: in.Slug, Name: in.Name, OwnerEmail: in.OwnerEmail, OwnerName: in.OwnerName,
		PlanCode: in.PlanCode, Currency: in.Currency, Locale: in.Locale, Timezone: in.Timezone,
	})
	if res.TenantID != "" {
		_ = s.rw(context.WithoutCancel(ctx), func(tx tenancy.PlatformTx) error {
			return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.create",
				TargetTenant: res.TenantID, After: map[string]any{"slug": in.Slug, "name": in.Name, "status": res.Status},
				IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
		})
	}
	s.flush()
	return res, perr
}

// RetryProvisioning resumes a failed tenant.
func (s *Service) RetryProvisioning(ctx context.Context, a Actor, id string, m Meta) (provisioning.Result, error) {
	p, err := s.provisioner()
	if err != nil {
		return provisioning.Result{}, err
	}
	res, perr := p.Retry(ctx, id)
	if perr == nil || res.TenantID != "" {
		_ = s.rw(context.WithoutCancel(ctx), func(tx tenancy.PlatformTx) error {
			return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.provisioning_retry",
				TargetTenant: id, After: map[string]any{"status": res.Status}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
		})
	}
	s.flush()
	return res, perr
}

func (s *Service) slugInHistory(ctx context.Context, slug string) bool {
	var taken bool
	_ = s.ro(ctx, func(tx tenancy.PlatformTx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform.tenant_slug_history WHERE old_slug = $1 AND redirect_until > now())`, slug).Scan(&taken)
	})
	return taken
}

// PatchTenantInput is PATCH /tenants/:id. Nil fields are unchanged.
type PatchTenantInput struct {
	Name         *string `json:"name"`
	BillingEmail *string `json:"billingEmail"`
	Notes        *string `json:"notes"`
}

// PatchTenant edits name, billing email and notes.
func (s *Service) PatchTenant(ctx context.Context, a Actor, id string, in PatchTenantInput, m Meta) (TenantView, error) {
	fields := map[string]string{}
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if l := len([]rune(n)); l < 1 || l > 120 || strings.ContainsAny(n, "\r\n\x00") {
			fields["name"] = "1-120 characters"
		}
		in.Name = &n
	}
	if in.BillingEmail != nil && *in.BillingEmail != "" && !validEmail(*in.BillingEmail) {
		fields["billingEmail"] = "must be a plain email address"
	}
	if in.Notes != nil && len(*in.Notes) > 2000 {
		fields["notes"] = "at most 2000 characters"
	}
	if len(fields) > 0 {
		return TenantView{}, apperr.Validation(fields)
	}
	before, err := s.GetTenant(ctx, id)
	if err != nil {
		return TenantView{}, err
	}
	err = s.rw(ctx, func(tx tenancy.PlatformTx) error {
		tag, err := tx.Exec(ctx, `UPDATE platform.tenants SET
				name = coalesce($2, name),
				billing_email = CASE WHEN $3::text IS NULL THEN billing_email ELSE NULLIF($3, '')::extensions.citext END,
				notes = CASE WHEN $4::text IS NULL THEN notes ELSE NULLIF($4, '') END,
				updated_at = now()
			WHERE id = $1 AND status <> 'deleted'`, id, in.Name, in.BillingEmail, in.Notes)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperr.NotFound()
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.edit", TargetTenant: id,
			Before: map[string]any{"name": before.Name, "billingEmail": before.BillingEmail},
			After:  map[string]any{"name": in.Name, "billingEmail": in.BillingEmail, "notesChanged": in.Notes != nil},
			IP:     m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	if err != nil {
		return TenantView{}, err
	}
	s.flush()
	return s.GetTenant(ctx, id)
}

// RenameSlug changes a tenant's subdomain. The old slug redirects and stays
// reserved for a year (TENANCY §3).
func (s *Service) RenameSlug(ctx context.Context, a Actor, id, newSlug string, m Meta) (TenantView, error) {
	newSlug = strings.ToLower(strings.TrimSpace(newSlug))
	if !httpx.ValidSlug(newSlug) || tenancy.ReservedSlugs[newSlug] {
		return TenantView{}, apperr.Validation(map[string]string{"slug": "3-30 lowercase letters, digits and single hyphens; not reserved"})
	}
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		var old string
		err := tx.QueryRow(ctx, `SELECT slug::text FROM platform.tenants WHERE id = $1 AND status NOT IN ('deleted','failed','provisioning') FOR UPDATE`, id).Scan(&old)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if old == newSlug {
			return apperr.Validation(map[string]string{"slug": "is the current slug"})
		}
		var blocked bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM platform.reserved_slugs WHERE slug = $1)
			OR EXISTS (SELECT 1 FROM platform.tenant_slug_history WHERE old_slug = $1 AND tenant_id <> $2 AND redirect_until > now())`,
			newSlug, id).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return apperr.Conflict(apperr.CodeConflict, "Slug is already taken")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM platform.tenant_slug_history WHERE old_slug = $1 AND tenant_id = $2`, newSlug, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE platform.tenants SET slug = $2, updated_at = now() WHERE id = $1`, id, newSlug); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return apperr.Conflict(apperr.CodeConflict, "Slug is already taken")
			}
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO platform.tenant_slug_history (old_slug, tenant_id, changed_by, redirect_until)
			VALUES ($1, $2, $3, now() + make_interval(days => $4))
			ON CONFLICT (old_slug) DO UPDATE SET tenant_id = EXCLUDED.tenant_id, changed_at = now(),
			  changed_by = EXCLUDED.changed_by, redirect_until = EXCLUDED.redirect_until`, old, id, a.UserID, SlugRedirectDays); err != nil {
			return err
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.slug_change", TargetTenant: id,
			Before: map[string]any{"slug": old}, After: map[string]any{"slug": newSlug}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	if err != nil {
		return TenantView{}, err
	}
	s.flush()
	return s.GetTenant(ctx, id)
}

// transition changes tenant status if the current status is allowed.
type transitionKind int

const (
	tSuspend transitionKind = iota
	tReactivate
	tDelete
	tRestore
)

func (s *Service) transition(ctx context.Context, a Actor, id, action string, from []string, kind transitionKind, reason string, m Meta) (TenantView, error) {
	if !tenancy.ValidTenantID(id) {
		return TenantView{}, apperr.NotFound()
	}
	err := s.rw(ctx, func(tx tenancy.PlatformTx) error {
		var cur string
		err := tx.QueryRow(ctx, `SELECT status FROM platform.tenants WHERE id = $1 FOR UPDATE`, id).Scan(&cur)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		allowed := false
		for _, f := range from {
			allowed = allowed || f == cur
		}
		if !allowed {
			return apperr.Conflict(apperr.CodeInvalidState, "Not allowed in status "+cur)
		}
		switch kind {
		case tSuspend:
			_, err = tx.Exec(ctx, qSuspend, id, reason)
		case tReactivate:
			_, err = tx.Exec(ctx, qReactivate, id, reason)
		case tDelete:
			_, err = tx.Exec(ctx, qDelete, id, reason)
		case tRestore:
			_, err = tx.Exec(ctx, qRestore, id, reason)
		}
		if err != nil {
			return err
		}
		var next string
		if err := tx.QueryRow(ctx, `SELECT status FROM platform.tenants WHERE id = $1`, id).Scan(&next); err != nil {
			return err
		}
		after := map[string]any{"status": next}
		if reason != "" {
			after["reason"] = reason
		}
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: action, TargetTenant: id,
			Before: map[string]any{"status": cur}, After: after, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	if err != nil {
		return TenantView{}, err
	}
	s.flush()
	return s.GetTenant(ctx, id)
}

var liveStatuses = []string{"trialing", "active", "past_due", "grace", "read_only", "cancelled"}

const (
	qSuspend = `UPDATE platform.tenants SET status = 'suspended', suspended_at = now(), suspend_reason = $2, updated_at = now() WHERE id = $1`
	// Back to trialing while the trial still runs, otherwise active.
	qReactivate = `UPDATE platform.tenants t SET status = CASE WHEN EXISTS (
			SELECT 1 FROM platform.subscriptions s WHERE s.tenant_id = t.id AND s.status = 'trialing' AND s.trial_end > now())
		THEN 'trialing' ELSE 'active' END,
		suspended_at = NULL, suspend_reason = NULL, updated_at = now()
		WHERE t.id = $1 AND $2::text IS NOT NULL` // $2 (reason) is unused here
	qDelete = `UPDATE platform.tenants SET status = 'pending_deletion', deletion_requested_at = now(),
		suspend_reason = coalesce(NULLIF($2, ''), suspend_reason), updated_at = now() WHERE id = $1`
	// Restoring lands in suspended; reactivation is a separate, deliberate step.
	qRestore = `UPDATE platform.tenants SET status = 'suspended', deletion_requested_at = NULL, purge_after = NULL,
		updated_at = now() WHERE id = $1 AND $2::text IS NOT NULL` // $2 unused
)

// Suspend blocks every login of a tenant (Q-11).
func (s *Service) Suspend(ctx context.Context, a Actor, id, reason string, m Meta) (TenantView, error) {
	reason = strings.TrimSpace(reason)
	if l := len([]rune(reason)); l < 5 || l > 500 {
		return TenantView{}, apperr.Validation(map[string]string{"reason": "5-500 characters"})
	}
	return s.transition(ctx, a, id, "tenant.suspend", liveStatuses, tSuspend, reason, m)
}

// Reactivate lifts a suspension.
func (s *Service) Reactivate(ctx context.Context, a Actor, id string, m Meta) (TenantView, error) {
	return s.transition(ctx, a, id, "tenant.reactivate", []string{"suspended"}, tReactivate, "", m)
}

// Delete moves a tenant to pending_deletion. Nothing is dropped (O-4).
func (s *Service) Delete(ctx context.Context, a Actor, id, reason string, m Meta) (TenantView, error) {
	return s.transition(ctx, a, id, "tenant.delete", append([]string{"suspended"}, liveStatuses...), tDelete, strings.TrimSpace(reason), m)
}

// Restore cancels a pending deletion.
func (s *Service) Restore(ctx context.Context, a Actor, id string, m Meta) (TenantView, error) {
	return s.transition(ctx, a, id, "tenant.restore", []string{"pending_deletion"}, tRestore, "", m)
}

func (s *Service) flush() {
	if s.cfg.Registry != nil {
		s.cfg.Registry.Flush()
	}
}

// AuditRow is one audit log line for the console.
type AuditRow struct {
	ID           int64          `json:"id"`
	At           time.Time      `json:"at"`
	ActorID      *int64         `json:"actorId,omitempty"`
	ActorEmail   string         `json:"actorEmail,omitempty"`
	Action       string         `json:"action"`
	TargetTenant string         `json:"targetTenant,omitempty"`
	Target       string         `json:"target,omitempty"`
	Before       map[string]any `json:"before,omitempty"`
	After        map[string]any `json:"after,omitempty"`
	IP           string         `json:"ip,omitempty"`
	RequestID    string         `json:"requestId,omitempty"`
}

// AuditLog returns entries newest first. Without audit:view_all only the
// actor's own entries are visible (PHASE1_DESIGN §7.1).
func (s *Service) AuditLog(ctx context.Context, a Actor, tenantID string, beforeID int64, limit int) ([]AuditRow, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	own := int64(0)
	if !Can(a.Role, PermAuditViewAll) {
		own = a.UserID
	}
	var out []AuditRow
	err := s.ro(ctx, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, `SELECT id, at, actor_id, coalesce(actor_email::text, ''), action, coalesce(target_tenant, ''),
				coalesce(target, ''), before, after, coalesce(host(ip), ''), coalesce(request_id, '')
			FROM platform.platform_audit_log
			WHERE ($1 = 0 OR actor_id = $1) AND ($2 = '' OR target_tenant = $2) AND ($3 = 0 OR id < $3)
			ORDER BY id DESC LIMIT $4`, own, tenantID, beforeID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r AuditRow
			if err := rows.Scan(&r.ID, &r.At, &r.ActorID, &r.ActorEmail, &r.Action, &r.TargetTenant, &r.Target,
				&r.Before, &r.After, &r.IP, &r.RequestID); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// OwnerInvite returns a one-time link the Super Admin hands to the tenant
// owner so they can set a password. The token is stored only as a hash;
// the link is shown once and audited.
func (s *Service) OwnerInvite(ctx context.Context, a Actor, id string, m Meta) (string, time.Time, error) {
	if s.cfg.IssueOwnerInvite == nil {
		return "", time.Time{}, apperr.Unavailable()
	}
	if !tenancy.ValidTenantID(id) {
		return "", time.Time{}, apperr.NotFound()
	}
	url, exp, err := s.cfg.IssueOwnerInvite(ctx, id, m)
	if err != nil {
		return "", time.Time{}, err
	}
	_ = s.rw(context.WithoutCancel(ctx), func(tx tenancy.PlatformTx) error {
		return writeAudit(ctx, tx, AuditEntry{ActorID: a.UserID, ActorEmail: a.Email, Action: "tenant.owner_invite",
			TargetTenant: id, After: map[string]any{"expiresAt": exp}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return url, exp, nil
}
