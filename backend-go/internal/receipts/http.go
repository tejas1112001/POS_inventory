package receipts

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/outbox"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Limits (EMAIL_AND_RECEIPTS §3.1, §5.2).
const (
	syncRenderTimeout = 5 * time.Second
	resendsPerDay     = 5 // per recipient address
)

// API holds the receipt endpoints.
type API struct {
	svc      *Service
	sessions *tenancy.Sessions
}

// NewAPI creates the API.
func NewAPI(svc *Service, s *tenancy.Sessions) *API { return &API{svc: svc, sessions: s} }

// Mount registers GET /sales/{id}/receipt and POST /sales/{id}/receipt/resend.
func (a *API) Mount(rt *rbac.Router) {
	rt.Handle(http.MethodGet, "/sales/{id}/receipt", rbac.Require("receipts", "view"), a.download)
	rt.Handle(http.MethodPost, "/sales/{id}/receipt/resend", rbac.Require("receipts", "resend"), a.resend)
}

func saleID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, apperr.NotFound()
	}
	return id, nil
}

// ownerScope: without sales:view_all a user reaches only their own sales
// (C-10).
func ownerScope(p auth.Principal) int64 {
	if p.Has("sales:view_all") {
		return 0
	}
	return p.UserID
}

type saleRef struct {
	reference string
	version   int
	userID    int64
	email     *string
	customer  string
	custEmail *string
}

const qSaleRef = `SELECT s.reference, s.version, s.user_id, s.receipt_email::text, c.name, c.email::text
	FROM sales s JOIN customers c ON c.id = s.customer_id WHERE s.id = $1`

const qSaleRefLock = qSaleRef + ` FOR UPDATE OF s`

func lookup(ctx context.Context, tx tenancy.TenantTx, id int64, p auth.Principal, lock bool) (saleRef, error) {
	var s saleRef
	var row pgx.Row
	if lock {
		row = tx.QueryRow(ctx, qSaleRefLock, id)
	} else {
		row = tx.QueryRow(ctx, qSaleRef, id)
	}
	err := row.Scan(&s.reference, &s.version, &s.userID, &s.email, &s.customer, &s.custEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, apperr.NotFound()
	}
	if err != nil {
		return s, err
	}
	if scope := ownerScope(p); scope != 0 && s.userID != scope {
		return s, apperr.NotFound()
	}
	return s, nil
}

var safeName = regexp.MustCompile(`[^A-Za-z0-9-]`)

// download streams the receipt PDF for the sale's current version,
// rendering it synchronously (5 s cap) if needed.
func (a *API) download(w http.ResponseWriter, r *http.Request) {
	id, err := saleID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, _ := tenancy.TenantFrom(r.Context())
	p, _ := auth.PrincipalFrom(r.Context())
	var (
		ref       saleRef
		receiptID int64
	)
	err = a.sessions.InTenantTx(r.Context(), t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		var err error
		if ref, err = lookup(r.Context(), tx, id, p, false); err != nil {
			return err
		}
		receiptID, err = Create(r.Context(), tx, id, ref.version)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), syncRenderTimeout)
	defer cancel()
	pdf, err := a.svc.Open(ctx, t, receiptID)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			err = apperr.Unavailable()
		}
		httpx.WriteError(w, r, err)
		return
	}
	name := safeName.ReplaceAllString(ref.reference, "_")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.pdf"`, name))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(pdf)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdf)
}

type resendIn struct {
	Email *string `json:"email"`
}

// resend queues the receipt email again (202). A new address is saved on
// the sale and audited; each address gets at most 5 resends a day.
func (a *API) resend(w http.ResponseWriter, r *http.Request) {
	id, err := saleID(r)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var in resendIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, _ := tenancy.TenantFrom(r.Context())
	p, _ := auth.PrincipalFrom(r.Context())
	err = a.sessions.InTenantTx(r.Context(), t, tenancy.TxOptions{Access: tenancy.ReadWrite}, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		ref, err := lookup(ctx, tx, id, p, true)
		if err != nil {
			return err
		}
		to := ""
		switch {
		case in.Email != nil && strings.TrimSpace(*in.Email) != "":
			to = strings.TrimSpace(*in.Email)
		case ref.email != nil:
			to = *ref.email
		case ref.custEmail != nil:
			to = *ref.custEmail
		}
		if to == "" {
			return apperr.Invalid("email", "Enter the customer's email address.")
		}
		if !mailer.ValidEmail(to) {
			return apperr.Invalid("email", "Enter a valid email address.")
		}
		var recent, prior int
		if err := tx.QueryRow(ctx, `SELECT
				(SELECT count(*) FROM email_outbox WHERE kind = 'receipt_resend' AND to_addr = $1 AND created_at > now() - interval '24 hours'),
				(SELECT count(*) FROM email_outbox WHERE idempotency_key LIKE $2)`,
			to, fmt.Sprintf("sale:%d:receipt:resend:%%", id)).Scan(&recent, &prior); err != nil {
			return err
		}
		if recent >= resendsPerDay {
			return apperr.RateLimited(apperr.CodeRateLimited, 24*time.Hour)
		}
		meta := audit.Entry{ActorID: p.UserID, Entity: "sale", EntityID: strconv.FormatInt(id, 10),
			RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}
		if ref.email == nil || !strings.EqualFold(*ref.email, to) {
			if _, err := tx.Exec(ctx, `UPDATE sales SET receipt_email = $2 WHERE id = $1`, id, to); err != nil {
				return err
			}
			e := meta
			e.Action, e.Before, e.After = "sale.receipt_email", map[string]any{"set": ref.email != nil}, map[string]any{"domain": domainOf(to)}
			if err := audit.Write(ctx, tx, e); err != nil {
				return err
			}
		}
		receiptID, err := Create(ctx, tx, id, ref.version)
		if err != nil {
			return err
		}
		if _, err := outbox.Enqueue(ctx, tx, outbox.Item{
			Key: fmt.Sprintf("sale:%d:receipt:resend:%d", id, prior+1), Kind: outbox.KindReceiptResend,
			To: to, ToName: ref.customer, Template: "receipt",
			Data:      map[string]any{"sale_id": id, "receipt_id": receiptID},
			ReceiptID: receiptID, Policy: outbox.PolicyTenantThenPlatform,
		}); err != nil {
			return err
		}
		e := meta
		e.Action, e.After = "receipt.resend", map[string]any{"receipt_id": receiptID, "domain": domainOf(to)}
		return audit.Write(ctx, tx, e)
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"queued": true})
}

func domainOf(addr string) string {
	_, d, _ := strings.Cut(addr, "@")
	return d
}
