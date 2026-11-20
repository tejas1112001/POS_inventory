package sales

// The shop's return policy (industry P1 item 9, A18; DB16). Defaults are
// today's rules. A refusal (time limit, a settlement the policy does not
// allow, a return without a sale) can be overridden by a user with
// sales_returns:override_policy and a written reason (10+ characters,
// audited).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// Return policy refusals (409).
const (
	CodeReturnWindowClosed = "RETURN_WINDOW_CLOSED"
	CodeReturnNotAllowed   = "RETURN_NOT_ALLOWED"
	CodeSaleRequired       = "SALE_REQUIRED"
)

// Settlements of a return's value (sale_returns.settlement).
const (
	SettleRefund      = "refund"
	SettleStoreCredit = "store_credit"
	SettleExchange    = "exchange"
)

// Conditions of a returned line (return_items.condition).
var conditions = []string{"resellable", "damaged", "defective"}

const minOverrideReason = 10

// ReturnPolicy is settings.return_policy.
type ReturnPolicy struct {
	Refund            bool     `json:"refund"`
	StoreCredit       bool     `json:"storeCredit"`
	Exchange          bool     `json:"exchange"`
	TimeLimitDays     *int     `json:"timeLimitDays"`
	RequireLinkedSale bool     `json:"requireLinkedSale"`
	Conditions        bool     `json:"conditions"`
	RestockConditions []string `json:"restockConditions"`
}

// DefaultPolicy is today's behaviour.
func DefaultPolicy() ReturnPolicy {
	return ReturnPolicy{Refund: true, RestockConditions: []string{"resellable"}}
}

func loadPolicy(ctx context.Context, tx tenancy.TenantTx) (ReturnPolicy, error) {
	var raw string
	if err := tx.QueryRow(ctx, `SELECT return_policy::text FROM settings WHERE id = 1`).Scan(&raw); err != nil {
		return ReturnPolicy{}, err
	}
	p := DefaultPolicy()
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return ReturnPolicy{}, err
	}
	if p.RestockConditions == nil {
		p.RestockConditions = []string{}
	}
	return p, nil
}

// restocks says whether a line in this condition goes back to stock.
func (pol ReturnPolicy) restocks(condition string) bool {
	return !pol.Conditions || slices.Contains(pol.RestockConditions, condition)
}

// violation is one policy refusal.
type violation struct{ code, msg string }

// windowClosed checks the time limit: days after the sale's day, both in
// the shop's time zone.
func (pol ReturnPolicy) windowClosed(saleDate *time.Time, now time.Time, loc *time.Location) *violation {
	if pol.TimeLimitDays == nil || saleDate == nil {
		return nil
	}
	sd, nd := saleDate.In(loc), now.In(loc)
	saleDay := time.Date(sd.Year(), sd.Month(), sd.Day(), 0, 0, 0, 0, time.UTC)
	today := time.Date(nd.Year(), nd.Month(), nd.Day(), 0, 0, 0, 0, time.UTC)
	days := int(today.Sub(saleDay).Hours() / 24)
	if days <= *pol.TimeLimitDays {
		return nil
	}
	return &violation{CodeReturnWindowClosed, fmt.Sprintf("The return window has closed: the sale was %d days ago and this shop takes returns within %d days.",
		days, *pol.TimeLimitDays)}
}

// settles checks that the policy allows giving value back this way.
func (pol ReturnPolicy) settles(mode string, amount decimal.Decimal) *violation {
	if !amount.IsPositive() {
		return nil
	}
	switch {
	case mode == SettleRefund && !pol.Refund:
		return &violation{CodeReturnNotAllowed, "This shop does not give refunds. Give store credit or an exchange instead."}
	case mode == SettleStoreCredit && !pol.StoreCredit:
		return &violation{CodeReturnNotAllowed, "This shop does not give store credit for returns."}
	case mode == SettleExchange && !pol.Exchange:
		return &violation{CodeReturnNotAllowed, "This shop does not do exchanges."}
	}
	return nil
}

// enforce refuses the first violation unless the principal may override
// the policy and wrote a reason; it returns the overridden violations.
func enforce(p auth.Principal, vs []*violation, reason string) ([]string, error) {
	var codes []string
	var first *violation
	for _, v := range vs {
		if v != nil {
			if first == nil {
				first = v
			}
			codes = append(codes, v.code)
		}
	}
	if first == nil {
		return nil, nil
	}
	can := p.Has("sales_returns:override_policy")
	if reason == "" {
		return nil, apperr.Conflict(first.code, first.msg).WithDetails(map[string]any{"overridable": can, "codes": codes})
	}
	if !can {
		return nil, apperr.Forbidden(apperr.CodeForbidden, "Overriding the return policy needs the override permission.")
	}
	if len([]rune(reason)) < minOverrideReason {
		return nil, apperr.Invalid("policyOverrideReason", "Write why the policy is overridden (at least 10 characters).")
	}
	return codes, nil
}

// policyMap is the policy as an audit value.
func policyMap(p ReturnPolicy) map[string]any {
	b, _ := json.Marshal(p)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

// ------------------------------------------------------------------- A18

type policyIn struct {
	Refund            *bool     `json:"refund"`
	StoreCredit       *bool     `json:"storeCredit"`
	Exchange          *bool     `json:"exchange"`
	TimeLimitDays     *int      `json:"timeLimitDays"`
	TimeLimitSet      bool      `json:"-"`
	RequireLinkedSale *bool     `json:"requireLinkedSale"`
	Conditions        *bool     `json:"conditions"`
	RestockConditions *[]string `json:"restockConditions"`
}

// getPolicy is GET /api/settings/returns.
func (a *API) getPolicy(w http.ResponseWriter, r *http.Request) {
	var out ReturnPolicy
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		out, err = loadPolicy(r.Context(), tx)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// putPolicy is PUT /api/settings/returns: the keys sent change; the rest
// stay. timeLimitDays null removes the limit.
func (a *API) putPolicy(w http.ResponseWriter, r *http.Request) {
	var raw map[string]json.RawMessage
	var in policyIn
	if err := httpx.DecodeJSON(r, &raw); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	f := fieldErrs{}
	known := map[string]any{"refund": &in.Refund, "storeCredit": &in.StoreCredit, "exchange": &in.Exchange,
		"timeLimitDays": &in.TimeLimitDays, "requireLinkedSale": &in.RequireLinkedSale, "conditions": &in.Conditions,
		"restockConditions": &in.RestockConditions}
	for k, v := range raw {
		dst, ok := known[k]
		if !ok {
			f[k] = "Unknown setting."
			continue
		}
		if err := json.Unmarshal(v, dst); err != nil {
			f[k] = "Wrong type."
		}
		if k == "timeLimitDays" {
			in.TimeLimitSet = true
		}
	}
	if err := f.err(); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p := principal(r)
	var out ReturnPolicy
	err := a.rw(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		if _, err := tx.Exec(ctx, `SELECT 1 FROM settings WHERE id = 1 FOR UPDATE`); err != nil {
			return err
		}
		before, err := loadPolicy(ctx, tx)
		if err != nil {
			return err
		}
		pol := before
		pol.RestockConditions = slices.Clone(before.RestockConditions)
		set := func(dst *bool, v *bool) {
			if v != nil {
				*dst = *v
			}
		}
		set(&pol.Refund, in.Refund)
		set(&pol.StoreCredit, in.StoreCredit)
		set(&pol.Exchange, in.Exchange)
		set(&pol.RequireLinkedSale, in.RequireLinkedSale)
		set(&pol.Conditions, in.Conditions)
		if in.TimeLimitSet {
			pol.TimeLimitDays = in.TimeLimitDays
		}
		if in.RestockConditions != nil {
			pol.RestockConditions = []string{}
			for _, c := range *in.RestockConditions {
				c = strings.TrimSpace(c)
				switch {
				case !slices.Contains(conditions, c):
					f["restockConditions"] = "Conditions are resellable, damaged or defective."
				case !slices.Contains(pol.RestockConditions, c):
					pol.RestockConditions = append(pol.RestockConditions, c)
				}
			}
		}
		if pol.TimeLimitDays != nil && (*pol.TimeLimitDays < 1 || *pol.TimeLimitDays > 3650) {
			f["timeLimitDays"] = "Between 1 and 3650 days, or empty for no limit."
		}
		if !pol.Refund && !pol.StoreCredit && !pol.Exchange {
			f["refund"] = "Allow at least one of refunds, store credit or exchanges."
		}
		if err := f.err(); err != nil {
			return err
		}
		b, err := json.Marshal(pol)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE settings SET return_policy = $1::jsonb, updated_at = now() WHERE id = 1`, string(b)); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "settings.return_policy", Entity: "settings",
			EntityID: "1", Before: policyMap(before), After: policyMap(pol), RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
			return err
		}
		out = pol
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
