// Package audit writes the tenant audit log (append-only for the tenant
// role). Entries never contain passwords, tokens or other secrets.
package audit

import (
	"context"
	"encoding/json"
	"net/netip"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Actor types.
const (
	ActorUser          = "user"
	ActorImpersonation = "platform_impersonation"
	ActorSystem        = "system"
)

// Entry is one audit_log row.
type Entry struct {
	ActorType string // default ActorUser
	ActorID   int64
	Action    string
	Entity    string
	EntityID  string
	Before    map[string]any
	After     map[string]any
	IP        netip.Addr
	UserAgent string
	RequestID string
}

const qInsert = `INSERT INTO audit_log (actor_type, actor_id, action, entity, entity_id, before, after, ip, user_agent, request_id)
	VALUES ($1, NULLIF($2, 0), $3, $4, NULLIF($5, ''), $6, $7, $8, NULLIF($9, ''), NULLIF($10, ''))`

// Write inserts an entry in the caller's tenant transaction.
func Write(ctx context.Context, tx tenancy.TenantTx, e Entry) error {
	if e.ActorType == "" {
		e.ActorType = ActorUser
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
	_, err := tx.Exec(ctx, qInsert, e.ActorType, e.ActorID, e.Action, e.Entity, e.EntityID,
		jsonOrNil(e.Before), jsonOrNil(e.After), ip, ua, e.RequestID)
	return err
}

func jsonOrNil(m map[string]any) *string {
	if m == nil {
		return nil
	}
	b, _ := json.Marshal(m)
	s := string(b)
	return &s
}
