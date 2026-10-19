// Package users implements tenant users and roles with the anti-escalation
// rules of API_CONTRACT C-07: an actor can only grant what they hold, can
// never change their own role or status or delete themselves, cannot touch
// a more powerful user, cannot edit the owner/admin role definitions or
// assign the owner role, and the last owner always stays.
package users

import (
	"context"
	"errors"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Service implements users and roles.
type Service struct {
	sessions *tenancy.Sessions
	auth     *auth.Service
	hasher   *security.Hasher
	maxRows  int
}

// NewService creates the service. maxRows caps unpaged lists (Q-4).
func NewService(s *tenancy.Sessions, a *auth.Service, h *security.Hasher, maxRows int) *Service {
	return &Service{sessions: s, auth: a, hasher: h, maxRows: maxRows}
}

func (s *Service) rw(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return s.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, fn)
}

func (s *Service) ro(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return s.sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, fn)
}

func forbidden(msg string) error { return apperr.Forbidden(apperr.CodeForbidden, msg) }

type roleInfo struct {
	ID        int64
	Name      string
	SystemKey string
	Perms     map[string]bool
}

const qRole = `SELECT id, name::text, coalesce(system_key, '') FROM roles WHERE id = $1 AND deleted_at IS NULL`

func loadRole(ctx context.Context, tx tenancy.TenantTx, id int64) (roleInfo, error) {
	var r roleInfo
	err := tx.QueryRow(ctx, qRole, id).Scan(&r.ID, &r.Name, &r.SystemKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, apperr.Validation(map[string]string{"role_id": "unknown role"})
	}
	if err != nil {
		return r, err
	}
	r.Perms, err = permSet(ctx, tx, id)
	return r, err
}

func permSet(ctx context.Context, tx tenancy.TenantTx, roleID int64) (map[string]bool, error) {
	rows, err := tx.Query(ctx, `SELECT p.module || ':' || p.action FROM role_permissions rp JOIN permissions p ON p.id = rp.permission_id WHERE rp.role_id = $1`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		set[p] = true
	}
	return set, rows.Err()
}

func subset(a, b map[string]bool) bool {
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// grantable: the actor may give this role to someone.
func grantable(actor auth.Principal, r roleInfo) error {
	if r.SystemKey == "owner" {
		return forbidden("The owner role can only be assigned through an ownership transfer")
	}
	if !subset(r.Perms, actor.Perms) {
		return forbidden("You cannot grant permissions you do not have")
	}
	return nil
}

func notify(ctx context.Context, tx tenancy.TenantTx, channel, tenantID string, id int64) error {
	payload := tenantID + ":" + strconv.FormatInt(id, 10)
	var err error
	switch channel {
	case tenancy.ChanAuthChanged:
		_, err = tx.Exec(ctx, `SELECT pg_notify('auth_changed', $1)`, payload)
	case tenancy.ChanRoleChanged:
		_, err = tx.Exec(ctx, `SELECT pg_notify('role_changed', $1)`, payload)
	}
	return err
}

// revokeUser kills every session of a user (role/status change, delete).
func revokeUser(ctx context.Context, tx tenancy.TenantTx, tenantID string, uid int64, reason string) error {
	if _, err := tx.Exec(ctx, `UPDATE users SET token_version = token_version + 1 WHERE id = $1`, uid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now(), revoke_reason = $2 WHERE user_id = $1 AND revoked_at IS NULL`, uid, reason); err != nil {
		return err
	}
	return notify(ctx, tx, tenancy.ChanAuthChanged, tenantID, uid)
}

func activeOwners(ctx context.Context, tx tenancy.TenantTx) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM users u JOIN roles r ON r.id = u.role_id
		WHERE r.system_key = 'owner' AND u.status = 'Active' AND u.deleted_at IS NULL`).Scan(&n)
	return n, err
}

// ------------------------------------------------------------------ users

const qUserCols = `u.id, u.name, u.email::text, u.status, u.role_id, u.created_at, u.updated_at, coalesce(r.system_key, ''), r.name::text`

func scanUser(row pgx.Row) (auth.UserJSON, string, error) {
	var u auth.UserJSON
	var key, roleName string
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Status, &u.RoleID, &u.CreatedAt, &u.UpdatedAt, &key, &roleName)
	u.Role = auth.LegacyRole(key)
	u.RoleRef = &auth.RoleRef{ID: u.RoleID, Name: roleName}
	return u, key, err
}

// List returns users (bare array, capped, Q-4). Truncated reports whether
// the cap was hit.
func (s *Service) List(ctx context.Context, t tenancy.Tenant, q, status string) ([]auth.UserJSON, int, bool, error) {
	if status != "Active" && status != "Inactive" {
		status = ""
	}
	like := ""
	if q = strings.TrimSpace(q); q != "" {
		like = "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(q)) + "%"
	}
	var (
		out   []auth.UserJSON
		total int
	)
	err := s.ro(ctx, t, func(tx tenancy.TenantTx) error {
		const where = ` WHERE u.deleted_at IS NULL AND ($1 = '' OR u.status = $1)
			AND ($2 = '' OR lower(u.name) LIKE $2 OR lower(u.email::text) LIKE $2)`
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users u`+where, status, like).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT `+qUserCols+` FROM users u JOIN roles r ON r.id = u.role_id`+where+`
			ORDER BY u.created_at DESC, u.id DESC LIMIT $3`, status, like, s.maxRows)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			u, _, err := scanUser(rows)
			if err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	if out == nil {
		out = []auth.UserJSON{}
	}
	return out, total, total > len(out), err
}

// CreateInput is POST /api/users (C-05).
type CreateInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	RoleID   int64  `json:"role_id"`
	Status   string `json:"status"`
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && len(s) <= 254
}

func cleanName(n string) (string, bool) {
	n = strings.TrimSpace(n)
	l := len([]rune(n))
	return n, l >= 1 && l <= 120 && !strings.ContainsAny(n, "\r\n\x00")
}

// Create adds a user. Without a password an invite email is queued.
func (s *Service) Create(ctx context.Context, t tenancy.Tenant, actor auth.Principal, in CreateInput, m auth.Meta) (auth.UserJSON, error) {
	fields := map[string]string{}
	name, ok := cleanName(in.Name)
	if !ok {
		fields["name"] = "1-120 characters"
	}
	email := strings.TrimSpace(in.Email)
	if !validEmail(email) {
		fields["email"] = "must be a valid email address"
	}
	if in.Status == "" {
		in.Status = "Active"
	}
	if in.Status != "Active" && in.Status != "Inactive" {
		fields["status"] = "must be Active or Inactive"
	}
	if in.RoleID <= 0 {
		fields["role_id"] = "is required"
	}
	if len(fields) > 0 {
		return auth.UserJSON{}, apperr.Validation(fields)
	}
	hash := ""
	if in.Password != "" {
		if err := security.CheckPassword(in.Password, auth.MinPasswordLen, email, name, t.Name); err != nil {
			return auth.UserJSON{}, apperr.Validation(map[string]string{"password": err.Error()})
		}
		var err error
		if hash, err = s.hasher.Hash(ctx, in.Password); err != nil {
			return auth.UserJSON{}, err
		}
	}
	var out auth.UserJSON
	err := s.rw(ctx, t, func(tx tenancy.TenantTx) error {
		role, err := loadRole(ctx, tx, in.RoleID)
		if err != nil {
			return err
		}
		if err := grantable(actor, role); err != nil {
			return err
		}
		var id int64
		err = tx.QueryRow(ctx, `INSERT INTO users (name, email, password_hash, status, role_id) VALUES ($1, $2, NULLIF($3, ''), $4, $5) RETURNING id`,
			name, email, hash, in.Status, role.ID).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperr.Conflict(apperr.CodeConflict, "Email is already in use.")
		}
		if err != nil {
			return err
		}
		if hash == "" {
			if _, err := s.auth.IssueToken(ctx, tx, t, id, email, name, "invite", auth.InviteTokenTTL, "user_invite", true, m); err != nil {
				return err
			}
		}
		if out, _, err = scanUser(tx.QueryRow(ctx, `SELECT `+qUserCols+` FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = $1`, id)); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: actor.UserID, Action: "user.create", Entity: "user", EntityID: strconv.FormatInt(id, 10),
			After: map[string]any{"email": email, "role_id": role.ID, "status": in.Status, "invited": hash == ""},
			IP:    m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return out, err
}

// UpdateInput is PUT /api/users/:id. Empty email keeps the current one
// (C-07); "role" is the legacy string and ignored.
type UpdateInput struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	RoleID *int64 `json:"role_id"`
	Status string `json:"status"`
}

// target loads a user to modify and enforces "not more powerful than me".
func target(ctx context.Context, tx tenancy.TenantTx, actor auth.Principal, id int64) (auth.UserJSON, string, map[string]bool, error) {
	u, key, err := scanUser(tx.QueryRow(ctx, `SELECT `+qUserCols+` FROM users u JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1 AND u.deleted_at IS NULL FOR UPDATE OF u`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return u, "", nil, apperr.NotFound()
	}
	if err != nil {
		return u, "", nil, err
	}
	perms, err := permSet(ctx, tx, u.RoleID)
	if err != nil {
		return u, "", nil, err
	}
	if u.ID != actor.UserID && !subset(perms, actor.Perms) {
		return u, "", nil, forbidden("You cannot change a user with more permissions than you")
	}
	return u, key, perms, nil
}

// Update edits a user.
func (s *Service) Update(ctx context.Context, t tenancy.Tenant, actor auth.Principal, id int64, in UpdateInput, m auth.Meta) (auth.UserJSON, error) {
	name, ok := cleanName(in.Name)
	if !ok {
		return auth.UserJSON{}, apperr.Validation(map[string]string{"name": "User name is required."})
	}
	email := strings.TrimSpace(in.Email)
	if email != "" && !validEmail(email) {
		return auth.UserJSON{}, apperr.Validation(map[string]string{"email": "must be a valid email address"})
	}
	if in.Status != "" && in.Status != "Active" && in.Status != "Inactive" {
		return auth.UserJSON{}, apperr.Validation(map[string]string{"status": "must be Active or Inactive"})
	}
	var out auth.UserJSON
	err := s.rw(ctx, t, func(tx tenancy.TenantTx) error {
		u, key, _, err := target(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		newStatus := u.Status
		if in.Status != "" {
			newStatus = in.Status
		}
		newRole := u.RoleID
		if in.RoleID != nil {
			newRole = *in.RoleID
		}
		roleChanged, statusChanged := newRole != u.RoleID, newStatus != u.Status
		if (roleChanged || statusChanged) && u.ID == actor.UserID {
			return forbidden("You cannot change your own role or status")
		}
		if roleChanged {
			role, err := loadRole(ctx, tx, newRole)
			if err != nil {
				return err
			}
			if err := grantable(actor, role); err != nil {
				return err
			}
		}
		if key == "owner" && (roleChanged || newStatus != "Active") {
			n, err := activeOwners(ctx, tx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return apperr.Conflict(apperr.CodeInvalidState, "The last owner cannot be removed")
			}
		}
		if email == "" {
			email = u.Email
		}
		_, err = tx.Exec(ctx, `UPDATE users SET name = $2, email = $3, status = $4, role_id = $5, updated_at = now() WHERE id = $1`,
			id, name, email, newStatus, newRole)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperr.Conflict(apperr.CodeConflict, "Email is already in use.")
		}
		if err != nil {
			return err
		}
		if roleChanged || statusChanged {
			if err := revokeUser(ctx, tx, t.ID, id, "role_or_status_changed"); err != nil {
				return err
			}
		}
		if out, _, err = scanUser(tx.QueryRow(ctx, `SELECT `+qUserCols+` FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = $1`, id)); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: actor.UserID, Action: "user.update", Entity: "user", EntityID: strconv.FormatInt(id, 10),
			Before: map[string]any{"name": u.Name, "email": u.Email, "status": u.Status, "role_id": u.RoleID},
			After:  map[string]any{"name": name, "email": email, "status": newStatus, "role_id": newRole},
			IP:     m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return out, err
}

// Delete soft-deletes a user and revokes their sessions (C-12).
func (s *Service) Delete(ctx context.Context, t tenancy.Tenant, actor auth.Principal, id int64, m auth.Meta) error {
	if id == actor.UserID {
		return forbidden("You cannot delete yourself")
	}
	return s.rw(ctx, t, func(tx tenancy.TenantTx) error {
		u, key, _, err := target(ctx, tx, actor, id)
		if err != nil {
			return err
		}
		if key == "owner" {
			n, err := activeOwners(ctx, tx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return apperr.Conflict(apperr.CodeInvalidState, "The last owner cannot be removed")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET deleted_at = now(), status = 'Inactive', updated_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if err := revokeUser(ctx, tx, t.ID, id, "deleted"); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: actor.UserID, Action: "user.delete", Entity: "user", EntityID: strconv.FormatInt(id, 10),
			Before: map[string]any{"email": u.Email, "role_id": u.RoleID}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
}

// ------------------------------------------------------------------ roles

// RoleJSON keeps the old /api/roles shape.
type RoleJSON struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Color         string    `json:"color"`
	IsSystem      bool      `json:"is_system"`
	CreatedAt     time.Time `json:"createdAt"`
	UserCount     int       `json:"userCount"`
	Permissions   []string  `json:"permissions"`
	PermissionIDs []int64   `json:"permissionIds"`
}

// PermissionJSON is one catalog row.
type PermissionJSON struct {
	ID     int64  `json:"id"`
	Module string `json:"module"`
	Action string `json:"action"`
	Label  string `json:"label"`
}

const qRoles = `SELECT r.id, r.name::text, coalesce(r.description, ''), r.color, r.is_system, r.created_at,
	(SELECT count(*) FROM users u WHERE u.role_id = r.id AND u.deleted_at IS NULL),
	coalesce(array_agg(p.module || ':' || p.action ORDER BY p.id) FILTER (WHERE p.id IS NOT NULL), '{}'),
	coalesce(array_agg(p.id ORDER BY p.id) FILTER (WHERE p.id IS NOT NULL), '{}')
	FROM roles r LEFT JOIN role_permissions rp ON rp.role_id = r.id LEFT JOIN permissions p ON p.id = rp.permission_id
	WHERE r.deleted_at IS NULL AND ($1 = 0 OR r.id = $1)
	GROUP BY r.id ORDER BY r.id`

func listRoles(ctx context.Context, tx tenancy.TenantTx, id int64) ([]RoleJSON, error) {
	rows, err := tx.Query(ctx, qRoles, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoleJSON{}
	for rows.Next() {
		var r RoleJSON
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Color, &r.IsSystem, &r.CreatedAt, &r.UserCount, &r.Permissions, &r.PermissionIDs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Roles lists roles with permissions and user counts.
func (s *Service) Roles(ctx context.Context, t tenancy.Tenant) ([]RoleJSON, error) {
	var out []RoleJSON
	err := s.ro(ctx, t, func(tx tenancy.TenantTx) error {
		var err error
		out, err = listRoles(ctx, tx, 0)
		return err
	})
	return out, err
}

// Permissions lists the catalog.
func (s *Service) Permissions(ctx context.Context, t tenancy.Tenant) ([]PermissionJSON, error) {
	out := []PermissionJSON{}
	err := s.ro(ctx, t, func(tx tenancy.TenantTx) error {
		rows, err := tx.Query(ctx, `SELECT id, module, action, coalesce(label, '') FROM permissions ORDER BY module, action`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p PermissionJSON
			if err := rows.Scan(&p.ID, &p.Module, &p.Action, &p.Label); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// RoleInput is POST/PUT /api/roles.
type RoleInput struct {
	Name          *string `json:"name"`
	Description   *string `json:"description"`
	Color         *string `json:"color"`
	PermissionIDs []int64 `json:"permissionIds"`
}

// checkPermIDs validates IDs and the subset rule; returns the keys.
func checkPermIDs(ctx context.Context, tx tenancy.TenantTx, actor auth.Principal, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id, module || ':' || action FROM permissions WHERE id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := map[int64]string{}
	for rows.Next() {
		var id int64
		var k string
		if err := rows.Scan(&id, &k); err != nil {
			return err
		}
		found[id] = k
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		k, ok := found[id]
		if !ok {
			return apperr.Validation(map[string]string{"permissionIds": "unknown permission " + strconv.FormatInt(id, 10)})
		}
		if !actor.Has(k) {
			return forbidden("You cannot grant permissions you do not have")
		}
	}
	return nil
}

func setRolePerms(ctx context.Context, tx tenancy.TenantTx, roleID int64, ids []int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission_id) SELECT $1, unnest($2::bigint[]) ON CONFLICT DO NOTHING`, roleID, ids)
	return err
}

func validColor(c string) bool {
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, r := range c[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// CreateRole adds a custom role.
func (s *Service) CreateRole(ctx context.Context, t tenancy.Tenant, actor auth.Principal, in RoleInput, m auth.Meta) (RoleJSON, error) {
	if in.Name == nil {
		return RoleJSON{}, apperr.Validation(map[string]string{"name": "Role name is required."})
	}
	name, ok := cleanName(*in.Name)
	if !ok || len([]rune(name)) > 60 {
		return RoleJSON{}, apperr.Validation(map[string]string{"name": "Role name is required (max 60 characters)."})
	}
	desc, color := "", "#6366f1"
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	if in.Color != nil {
		if !validColor(*in.Color) {
			return RoleJSON{}, apperr.Validation(map[string]string{"color": "must be #RRGGBB"})
		}
		color = *in.Color
	}
	var out RoleJSON
	err := s.rw(ctx, t, func(tx tenancy.TenantTx) error {
		if err := checkPermIDs(ctx, tx, actor, in.PermissionIDs); err != nil {
			return err
		}
		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO roles (name, description, color, is_system) VALUES ($1, $2, $3, false) RETURNING id`, name, desc, color).Scan(&id)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return apperr.Conflict(apperr.CodeConflict, "Role with this name already exists.")
		}
		if err != nil {
			return err
		}
		if err := setRolePerms(ctx, tx, id, in.PermissionIDs); err != nil {
			return err
		}
		list, err := listRoles(ctx, tx, id)
		if err != nil {
			return err
		}
		out = list[0]
		return audit.Write(ctx, tx, audit.Entry{ActorID: actor.UserID, Action: "role.create", Entity: "role", EntityID: strconv.FormatInt(id, 10),
			After: map[string]any{"name": name, "permissions": out.Permissions}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return out, err
}

// UpdateRole edits a custom role or the biller role.
func (s *Service) UpdateRole(ctx context.Context, t tenancy.Tenant, actor auth.Principal, id int64, in RoleInput, m auth.Meta) (RoleJSON, error) {
	var out RoleJSON
	err := s.rw(ctx, t, func(tx tenancy.TenantTx) error {
		before, err := loadRole(ctx, tx, id)
		if err != nil {
			return apperr.NotFound()
		}
		if before.SystemKey == "owner" || before.SystemKey == "admin" {
			return forbidden("The owner and admin roles cannot be changed")
		}
		if id == actor.RoleID {
			return forbidden("You cannot change your own role")
		}
		if !subset(before.Perms, actor.Perms) {
			return forbidden("You cannot change a role with permissions you do not have")
		}
		if in.Name != nil {
			name, ok := cleanName(*in.Name)
			if !ok || len([]rune(name)) > 60 {
				return apperr.Validation(map[string]string{"name": "Role name is required (max 60 characters)."})
			}
			if before.SystemKey != "" && name != before.Name {
				return forbidden("System roles cannot be renamed")
			}
			_, err := tx.Exec(ctx, `UPDATE roles SET name = $2, updated_at = now() WHERE id = $1`, id, name)
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return apperr.Conflict(apperr.CodeConflict, "Role with this name already exists.")
			}
			if err != nil {
				return err
			}
		}
		if in.Description != nil {
			if _, err := tx.Exec(ctx, `UPDATE roles SET description = $2, updated_at = now() WHERE id = $1`, id, strings.TrimSpace(*in.Description)); err != nil {
				return err
			}
		}
		if in.Color != nil {
			if !validColor(*in.Color) {
				return apperr.Validation(map[string]string{"color": "must be #RRGGBB"})
			}
			if _, err := tx.Exec(ctx, `UPDATE roles SET color = $2, updated_at = now() WHERE id = $1`, id, *in.Color); err != nil {
				return err
			}
		}
		if in.PermissionIDs != nil {
			if err := checkPermIDs(ctx, tx, actor, in.PermissionIDs); err != nil {
				return err
			}
			if err := setRolePerms(ctx, tx, id, in.PermissionIDs); err != nil {
				return err
			}
			if err := notify(ctx, tx, tenancy.ChanRoleChanged, t.ID, id); err != nil {
				return err
			}
		}
		list, err := listRoles(ctx, tx, id)
		if err != nil {
			return err
		}
		out = list[0]
		return audit.Write(ctx, tx, audit.Entry{ActorID: actor.UserID, Action: "role.update", Entity: "role", EntityID: strconv.FormatInt(id, 10),
			Before: map[string]any{"name": before.Name, "permissions": keys(before.Perms)},
			After:  map[string]any{"name": out.Name, "permissions": out.Permissions},
			IP:     m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
	return out, err
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// DeleteRole soft-deletes a custom role without users.
func (s *Service) DeleteRole(ctx context.Context, t tenancy.Tenant, actor auth.Principal, id int64, m auth.Meta) error {
	return s.rw(ctx, t, func(tx tenancy.TenantTx) error {
		r, err := loadRole(ctx, tx, id)
		if err != nil {
			return apperr.NotFound()
		}
		if r.SystemKey != "" {
			return apperr.Validation(map[string]string{"role": "System roles cannot be deleted."})
		}
		if !subset(r.Perms, actor.Perms) {
			return forbidden("You cannot delete a role with permissions you do not have")
		}
		var users int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE role_id = $1 AND deleted_at IS NULL`, id).Scan(&users); err != nil {
			return err
		}
		if users > 0 {
			return apperr.Validation(map[string]string{"role": "Cannot delete a role assigned to " + strconv.Itoa(users) + " user(s). Reassign them first."})
		}
		if _, err := tx.Exec(ctx, `UPDATE roles SET deleted_at = now() WHERE id = $1`, id); err != nil {
			return err
		}
		if err := notify(ctx, tx, tenancy.ChanRoleChanged, t.ID, id); err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{ActorID: actor.UserID, Action: "role.delete", Entity: "role", EntityID: strconv.FormatInt(id, 10),
			Before: map[string]any{"name": r.Name}, IP: m.IP, UserAgent: m.UserAgent, RequestID: m.RequestID})
	})
}

// AssignRole is POST /api/roles/assign-user.
func (s *Service) AssignRole(ctx context.Context, t tenancy.Tenant, actor auth.Principal, userID, roleID int64, m auth.Meta) (auth.UserJSON, error) {
	if userID <= 0 || roleID <= 0 {
		return auth.UserJSON{}, apperr.Validation(map[string]string{"userId": "userId and roleId are required."})
	}
	var current auth.UserJSON
	if err := s.ro(ctx, t, func(tx tenancy.TenantTx) error {
		var err error
		current, _, err = scanUser(tx.QueryRow(ctx, `SELECT `+qUserCols+` FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = $1 AND u.deleted_at IS NULL`, userID))
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return err
	}); err != nil {
		return auth.UserJSON{}, err
	}
	return s.Update(ctx, t, actor, userID, UpdateInput{Name: current.Name, RoleID: &roleID}, m)
}
