package platform

import (
	"context"
	"errors"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/migrate"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
)

// ErrAlreadyBootstrapped means a platform user exists already.
var ErrAlreadyBootstrapped = errors.New("platform: a platform user already exists; bootstrap refused")

// BootstrapSuperAdmin creates the first super_admin (PHASE1_DESIGN §7.3). It
// refuses if any platform user exists. The user must change the password
// and enrol TOTP at first login. No default password exists in code.
func BootstrapSuperAdmin(ctx context.Context, migratorDSN secrets.Secret, email, name, password string, h *security.Hasher) (int64, error) {
	email = strings.TrimSpace(email)
	if !validEmail(email) {
		return 0, errors.New("BOOTSTRAP_ADMIN_EMAIL must be a plain email address")
	}
	if name = strings.TrimSpace(name); name == "" {
		name = "Super Admin"
	}
	if err := security.CheckPassword(password, MinPasswordLen, email, name); err != nil {
		return 0, errors.New("BOOTSTRAP_ADMIN_PASSWORD " + err.Error())
	}
	hash, err := h.Hash(ctx, password)
	if err != nil {
		return 0, err
	}
	conn, err := migrate.PlatformConn(migratorDSN, "posctl-bootstrap")
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize concurrent bootstraps.
	if _, err := tx.ExecContext(ctx, `LOCK TABLE platform.platform_users IN EXCLUSIVE MODE`); err != nil {
		return 0, err
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM platform.platform_users)`).Scan(&exists); err != nil {
		return 0, err
	}
	if exists {
		return 0, ErrAlreadyBootstrapped
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO platform.platform_users (email, name, role, password_hash, must_change_password, mfa_enrolled)
		VALUES ($1, $2, 'super_admin', $3, true, false) RETURNING id`, email, name, hash).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO platform.platform_audit_log (actor_id, actor_email, action, after)
		VALUES ($1, $2, 'platform_user.bootstrap', '{"role":"super_admin"}')`, id, email); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}
