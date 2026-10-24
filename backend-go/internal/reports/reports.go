// Package reports serves the sales and purchase reports, their Excel and
// PDF exports, and the dashboard (PHASE1_DESIGN steps 13 and 14;
// API_CONTRACT C-10, C-16; PRD §3.2, §3.13). Sales figures are limited to
// the user's own sales without sales:view_all. Every query runs in a
// read-only tenant transaction with the longer report statement timeout.
package reports

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/settings"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// ReportTimeout is the statement timeout for report queries (PHASE1_DESIGN
// §6: reports 30 s).
const ReportTimeout = 30 * time.Second

// Deps are the report dependencies.
type Deps struct {
	Sessions *tenancy.Sessions
	Plans    *plans.Service
	MaxRows  int              // unpaged list cap
	Now      func() time.Time // tests; default time.Now
}

// API holds the handlers.
type API struct{ d Deps }

// New creates the API.
func New(d Deps) *API {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &API{d: d}
}

// Mount registers the report routes.
func (a *API) Mount(rt *rbac.Router) {
	req := rbac.Require
	rt.Handle(http.MethodGet, "/sales/report", req("reports", "view"), a.salesReport)
	rt.Handle(http.MethodGet, "/purchase/report", req("reports", "view"), a.purchaseReport)
}

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

// ownerScope is the user whose sales a principal sees: 0 (everyone) with
// sales:view_all, else the principal (C-10).
func ownerScope(p auth.Principal) int64 {
	if p.Has("sales:view_all") {
		return 0
	}
	return p.UserID
}

func (a *API) ro(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly, StatementTimeout: ReportTimeout}, fn)
}

func queryID(r *http.Request, name string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get(name)), 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// location is the tenant's time zone (UTC if unknown).
func location(b settings.Business) *time.Location {
	loc, err := time.LoadLocation(b.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// privateImage is the signed-in URL of a file (reports are never public).
func privateImage(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return "/api/files/" + id.String()
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
