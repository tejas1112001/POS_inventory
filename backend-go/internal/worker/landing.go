package worker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailtmpl"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// The landing page queues a find_shop row for every valid request, so the
// request itself takes the same path whether or not a shop exists. The
// lookup happens here; with no shop the row is cancelled and nothing is sent.
const (
	qFindShops = `SELECT slug::text, name FROM platform.tenants
		WHERE (owner_email = $1 OR billing_email = $1)
		  AND status IN ('trialing','active','past_due','grace','cancelled','read_only','suspended','pending_deletion')
		ORDER BY created_at LIMIT 20`
	qContactRequest = `SELECT topic, name, email::text, coalesce(business, ''), coalesce(phone, ''), message, created_at
		FROM platform.contact_requests WHERE id = $1`
)

// deliverFindShop emails the sign-in addresses of the shops whose owner or
// billing email is the row's recipient.
func (m *Mailer) deliverFindShop(ctx context.Context, r row) outcome {
	var shops []mailtmpl.ShopLink
	err := m.c.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		rows, err := tx.Query(ctx, qFindShops, r.to)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var slug, name string
			if err := rows.Scan(&slug, &name); err != nil {
				return err
			}
			shops = append(shops, mailtmpl.ShopLink{Name: name,
				URL: httpx.TenantURL(m.c.PublicScheme, slug, m.c.BaseDomain, m.c.PublicPort) + "/login"})
		}
		return rows.Err()
	})
	if err != nil {
		return outcome{status: "retry", class: "transient", detail: "shop lookup failed", retryAt: time.Now().Add(backoff(r.attempts))}
	}
	if len(shops) == 0 {
		return outcome{status: "cancel", detail: "no shop for this address"}
	}
	rendered, err := mailtmpl.RenderFindShop(mailtmpl.FindShop{PlatformName: m.c.PlatformName, Shops: shops})
	if err != nil {
		return outcome{status: "dead", class: "permanent", detail: "template failed"}
	}
	return m.sendPlatformNotice(ctx, r, rendered, mailer.Address{})
}

type contactData struct {
	RequestID int64 `json:"request_id"`
}

// deliverContactRequest sends a landing page contact request to the
// platform inbox (CONTACT_EMAIL), with Reply-To set to the visitor.
func (m *Mailer) deliverContactRequest(ctx context.Context, r row) outcome {
	var d contactData
	if err := json.Unmarshal(r.data, &d); err != nil || d.RequestID <= 0 {
		return outcome{status: "dead", class: "permanent", detail: "bad contact request data"}
	}
	var (
		c     mailtmpl.ContactRequest
		topic string
		at    time.Time
	)
	err := m.c.Jobs.InPlatformTx(ctx, tenancy.TxOptions{Access: tenancy.ReadOnly}, func(tx tenancy.PlatformTx) error {
		return tx.QueryRow(ctx, qContactRequest, d.RequestID).Scan(&topic, &c.Name, &c.Email, &c.Business, &c.Phone, &c.Message, &at)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return outcome{status: "cancel", detail: "contact request gone"}
	}
	if err != nil {
		return outcome{status: "retry", class: "transient", detail: "contact request lookup failed", retryAt: time.Now().Add(backoff(r.attempts))}
	}
	c.PlatformName, c.ID, c.Demo = m.c.PlatformName, d.RequestID, topic == "demo"
	c.Sent = at.UTC().Format("02 Jan 2006 15:04") + " UTC"
	rendered, err := mailtmpl.RenderContactRequest(c)
	if err != nil {
		return outcome{status: "dead", class: "permanent", detail: "template failed"}
	}
	var replyTo mailer.Address
	if mailer.ValidEmail(c.Email) {
		replyTo = mailer.Address{Name: mailer.SanitizeName(c.Name), Email: c.Email}
	}
	return m.sendPlatformNotice(ctx, r, rendered, replyTo)
}

// sendPlatformNotice sends one rendered platform email to the row's recipient.
func (m *Mailer) sendPlatformNotice(ctx context.Context, r row, rendered mailtmpl.Rendered, replyTo mailer.Address) outcome {
	if !refreshPlatform(ctx, m, r) {
		return outcome{status: "cancel", detail: "lease lost"}
	}
	name := ""
	if r.toName != nil {
		name = *r.toName
	}
	id, err := m.c.Platform.Send(ctx, mailer.Message{
		From:    mailer.Address{Name: mailer.SanitizeName(m.c.PlatformName), Email: m.c.MailFrom},
		ReplyTo: replyTo,
		To:      []mailer.Address{{Name: mailer.SanitizeName(name), Email: r.to}},
		Subject: rendered.Subject, TextBody: rendered.Text, HTMLBody: rendered.HTML,
		MessageID: m.messageID("platform", r.key),
	})
	if err != nil {
		return errOutcome(r, err)
	}
	return outcome{status: "sent", via: "platform", providerID: id}
}
