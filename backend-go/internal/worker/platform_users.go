package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/platform"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

type platformInviteData struct {
	UserID    int64     `json:"userId"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedBy int64     `json:"createdBy"`
	InvitedBy string    `json:"invitedBy"`
}

// deliverPlatformInvite emails a console invite (S10 item 1). The emailed
// token is issued now, at send time, so no clear token is ever stored; the
// invite is skipped when it was accepted, revoked or has expired.
func (m *Mailer) deliverPlatformInvite(ctx context.Context, r row) outcome {
	var d platformInviteData
	if err := json.Unmarshal(r.data, &d); err != nil || d.UserID <= 0 {
		return outcome{status: "dead", class: "permanent", detail: "bad invite data"}
	}
	if !d.ExpiresAt.After(time.Now()) {
		return outcome{status: "cancel", detail: "invite expired"}
	}
	var (
		name  string
		token string
	)
	err := m.c.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.PlatformTx) error {
		var open bool
		if err := tx.QueryRow(ctx, `SELECT u.name, EXISTS (SELECT 1 FROM platform.platform_invites i WHERE i.user_id = u.id
				AND i.used_at IS NULL AND i.revoked_at IS NULL AND i.expires_at > now())
			FROM platform.platform_users u WHERE u.id = $1 AND u.status = 'Active' AND NOT u.mfa_enrolled`, d.UserID).Scan(&name, &open); err != nil {
			return err
		}
		if !open {
			return pgx.ErrNoRows
		}
		var err error
		token, err = platform.IssueInvite(ctx, tx, d.UserID, "email", d.CreatedBy, d.ExpiresAt)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return outcome{status: "cancel", detail: "invite no longer open"}
	}
	if err != nil {
		return outcome{status: "retry", class: "transient", detail: "invite lookup failed", retryAt: time.Now().Add(backoff(r.attempts))}
	}
	invitedBy := d.InvitedBy
	if invitedBy == "" {
		invitedBy = "An administrator"
	}
	rendered, err := mailtmpl.RenderPlatformInvite(mailtmpl.PlatformInvite{PlatformName: m.c.PlatformName, Name: name, InvitedBy: invitedBy,
		Link: platform.InviteURL(m.c.PublicScheme, m.c.BaseDomain, m.c.PublicPort, token), Expires: humanize(time.Until(d.ExpiresAt))})
	if err != nil {
		return outcome{status: "dead", class: "permanent", detail: "template failed"}
	}
	return m.sendPlatformNotice(ctx, r, rendered, mailer.Address{})
}

// deliverPlatformUserNotice emails a change to a console account.
func (m *Mailer) deliverPlatformUserNotice(ctx context.Context, r row) outcome {
	var d struct {
		Name    string   `json:"name"`
		Heading string   `json:"heading"`
		Lines   []string `json:"lines"`
	}
	if err := json.Unmarshal(r.data, &d); err != nil || d.Heading == "" {
		return outcome{status: "dead", class: "permanent", detail: "bad notice data"}
	}
	rendered, err := mailtmpl.RenderPlatformUserNotice(mailtmpl.PlatformUserNotice{PlatformName: m.c.PlatformName, Name: d.Name, Heading: d.Heading, Lines: d.Lines})
	if err != nil {
		return outcome{status: "dead", class: "permanent", detail: "template failed"}
	}
	return m.sendPlatformNotice(ctx, r, rendered, mailer.Address{})
}
