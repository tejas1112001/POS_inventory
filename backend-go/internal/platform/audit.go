package platform

import (
	"context"
	"encoding/json"
	"net/netip"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// AuditEntry is one platform_audit_log row. Before/After must never hold
// secrets; callers pass already-redacted maps.
type AuditEntry struct {
	ActorID      int64
	ActorEmail   string
	Action       string
	TargetTenant string
	Target       string
	Before       map[string]any
	After        map[string]any
	IP           netip.Addr
	UserAgent    string
	RequestID    string
}

const qAudit = `INSERT INTO platform.platform_audit_log
	(actor_id, actor_email, action, target_tenant, target, before, after, ip, user_agent, request_id)
	VALUES (NULLIF($1, 0), NULLIF($2, ''), $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, NULLIF($9, ''), NULLIF($10, ''))`

func writeAudit(ctx context.Context, tx tenancy.PlatformTx, e AuditEntry) error {
	var before, after []byte
	if e.Before != nil {
		before, _ = json.Marshal(e.Before)
	}
	if e.After != nil {
		after, _ = json.Marshal(e.After)
	}
	var ip *string
	if e.IP.IsValid() {
		s := e.IP.String()
		ip = &s
	}
	ua := e.UserAgent
	if len(ua) > 400 {
		ua = ua[:400]
	}
	_, err := tx.Exec(ctx, qAudit, e.ActorID, e.ActorEmail, e.Action, e.TargetTenant, e.Target,
		nullJSON(before), nullJSON(after), ip, ua, e.RequestID)
	return err
}

func nullJSON(b []byte) *string {
	if b == nil {
		return nil
	}
	s := string(b)
	return &s
}
