package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

type overrideData struct {
	OverrideID int64  `json:"overrideId"`
	Change     string `json:"change"`
}

var overrideLabels = map[string]string{
	"max_stores": "Stores", "max_users": "Users", "max_products": "Products", "max_emails_per_day": "Emails a day",
	"max_storage_mb": "Storage (MB)", "public_shop": "Online shop", "csv_import": "CSV import", "new_checkout": "New checkout",
}

const qOverrideNotice = `SELECT t.name, t.slug::text, o.key, o.value::text, o.reason, o.expires_at, o.created_at,
		coalesce(c.email::text, ''), o.revoked_at, coalesce(r.email::text, ''), coalesce(o.revoke_reason, '')
	FROM platform.tenant_overrides o JOIN platform.tenants t ON t.id = o.tenant_id
	LEFT JOIN platform.platform_users c ON c.id = o.created_by
	LEFT JOIN platform.platform_users r ON r.id = o.revoked_by
	WHERE o.id = $1`

// deliverOverrideNotice sends the platform inbox notice of an override
// change (UI S9). The row holds only the id; the details are read now.
func (m *Mailer) deliverOverrideNotice(ctx context.Context, r row) outcome {
	var d overrideData
	if err := json.Unmarshal(r.data, &d); err != nil || d.OverrideID <= 0 || (d.Change != "set" && d.Change != "revoke") {
		return outcome{status: "dead", class: "permanent", detail: "bad override notice data"}
	}
	var (
		n                   mailtmpl.OverrideNotice
		key, value          string
		expires, created    time.Time
		createdBy, revokeBy string
		revokedAt           *time.Time
	)
	err := m.c.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		return tx.QueryRow(ctx, qOverrideNotice, d.OverrideID).Scan(&n.TenantName, &n.Slug, &key, &value, &n.Reason, &expires, &created,
			&createdBy, &revokedAt, &revokeBy, &n.RevokeReason)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return outcome{status: "cancel", detail: "override gone"}
	}
	if err != nil {
		return outcome{status: "retry", class: "transient", detail: "override lookup failed", retryAt: time.Now().Add(backoff(r.attempts))}
	}
	n.PlatformName, n.Change, n.Label = m.c.PlatformName, d.Change, overrideLabels[key]
	switch value {
	case "true":
		n.Value = "On"
	case "false":
		n.Value = "Off"
	default:
		n.Value = strings.Trim(value, `"`)
	}
	n.Expires = expires.UTC().Format("02 Jan 2006 15:04") + " UTC"
	n.Actor, n.At = createdBy, created.UTC().Format("02 Jan 2006 15:04")+" UTC"
	if d.Change == "revoke" && revokedAt != nil {
		n.Actor, n.At = revokeBy, revokedAt.UTC().Format("02 Jan 2006 15:04")+" UTC"
	}
	rendered, err := mailtmpl.RenderOverrideNotice(n)
	if err != nil {
		return outcome{status: "dead", class: "permanent", detail: "template failed"}
	}
	return m.sendPlatformNotice(ctx, r, rendered, mailer.Address{})
}
