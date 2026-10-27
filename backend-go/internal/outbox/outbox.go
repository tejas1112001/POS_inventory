// Package outbox writes email rows in the caller's transaction
// (EMAIL_AND_RECEIPTS §4). The row commits with the business change; the
// email_outbox trigger marks the tenant for the mail worker, and only if
// the transaction commits.
package outbox

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Kinds (email_outbox.kind).
const (
	KindReceipt        = "receipt"
	KindReceiptResend  = "receipt_resend"
	KindTest           = "test"
	KindCustomer       = "customer"
	KindPlatformNotice = "platform_notice"
)

// Sender policies.
const (
	PolicyTenantThenPlatform = "tenant_then_platform"
	PolicyPlatform           = "platform"
)

// Item is one email to queue. Data holds IDs and small fields only, never
// secrets or full bodies.
type Item struct {
	Key       string // idempotency key, e.g. "sale:12:receipt"
	Kind      string
	To        string
	ToName    string
	Template  string
	Data      map[string]any
	ReceiptID int64 // attachment (0: none)
	Policy    string
}

// ErrInvalid means the item cannot be queued.
var ErrInvalid = errors.New("outbox: invalid item")

const qEnqueue = `INSERT INTO email_outbox (idempotency_key, kind, to_addr, to_name, template, template_data,
		attachment_receipt_id, sender_policy)
	VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, NULLIF($7, 0), $8)
	ON CONFLICT (idempotency_key) DO NOTHING`

// Enqueue inserts the item unless its key exists. It reports whether a row
// was added.
func Enqueue(ctx context.Context, tx tenancy.TenantTx, it Item) (bool, error) {
	if it.Key == "" || it.Template == "" || !mailer.ValidEmail(it.To) {
		return false, ErrInvalid
	}
	if it.Policy == "" {
		it.Policy = PolicyTenantThenPlatform
	}
	if it.Data == nil {
		it.Data = map[string]any{}
	}
	data, err := json.Marshal(it.Data)
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, qEnqueue, it.Key, it.Kind, it.To, mailer.SanitizeName(it.ToName),
		it.Template, string(data), it.ReceiptID, it.Policy)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
