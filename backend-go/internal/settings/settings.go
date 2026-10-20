// Package settings serves the tenant's business settings and SMTP
// settings (API_CONTRACT §5: /api/settings, /api/settings/email) and loads
// them for receipts and the mail worker.
package settings

import (
	"context"
	"crypto/x509"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"golang.org/x/text/currency"
	"golang.org/x/text/language"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/audit"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/auth"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/files"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/httpx"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/plans"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/secrets"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/tenancy"
)

// Deps are the settings dependencies.
type Deps struct {
	Sessions   *tenancy.Sessions
	Files      *files.Service
	Plans      *plans.Service
	Envelope   *secrets.Envelope
	Guard      mailer.Guard   // tenant SMTP SSRF guard
	RootCAs    *x509.CertPool // nil: system roots
	MailDomain string         // Message-ID and EHLO domain
	Now        func() time.Time
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

func tenant(r *http.Request) tenancy.Tenant {
	t, _ := tenancy.TenantFrom(r.Context())
	return t
}

func principal(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context())
	return p
}

func (a *API) rw(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadWrite}, fn)
}

func (a *API) ro(ctx context.Context, t tenancy.Tenant, fn func(tenancy.TenantTx) error) error {
	return a.d.Sessions.InTenantTx(ctx, t, tenancy.TxOptions{Access: tenancy.ReadOnly}, fn)
}

// Mount registers the routes.
func (a *API) Mount(rt *rbac.Router) {
	req := rbac.Require
	rt.Handle(http.MethodGet, "/settings", req("settings", "view"), a.get)
	rt.Handle(http.MethodPut, "/settings", req("settings", "edit").Uploads(), a.put)
	rt.Handle(http.MethodGet, "/settings/email", req("email_settings", "view"), a.getEmail)
	rt.Handle(http.MethodPut, "/settings/email", req("email_settings", "edit"), a.putEmail)
	rt.Handle(http.MethodDelete, "/settings/email", req("email_settings", "edit"), a.deleteEmail)
	rt.Handle(http.MethodPost, "/settings/email/test", req("email_settings", "edit"), a.testEmail)
}

// ---------------------------------------------------------------- model

// Business is what receipts and emails show about the tenant.
type Business struct {
	Name, Currency, Locale, Timezone, TaxLabel, TaxSplit, InvoicePrefix string
	LegalName, Address, Phone, Email, TaxRegistrationNo                 *string
	ReceiptHeader, ReceiptFooter                                        *string
	LogoFileID                                                          *uuid.UUID
	PricesIncludeTax                                                    bool
	MaxDiscountPct                                                      decimal.Decimal
	DefaultCustomerID                                                   *int64
	UpdatedAt                                                           time.Time
}

const qLoad = `SELECT business_name, legal_name, address, phone, email::text, logo_file_id, currency, locale, timezone,
		tax_label, tax_registration_no, tax_split, prices_include_tax_default, max_discount_pct_without_override,
		invoice_prefix, receipt_header, receipt_footer, default_customer_id, updated_at FROM settings WHERE id = 1`

// LoadBusiness reads the settings row in the caller's tenant transaction.
func LoadBusiness(ctx context.Context, tx tenancy.TenantTx) (Business, error) {
	return scanBusiness(tx.QueryRow(ctx, qLoad))
}

func scanBusiness(row pgx.Row) (Business, error) {
	var b Business
	err := row.Scan(&b.Name, &b.LegalName, &b.Address, &b.Phone, &b.Email, &b.LogoFileID, &b.Currency, &b.Locale,
		&b.Timezone, &b.TaxLabel, &b.TaxRegistrationNo, &b.TaxSplit, &b.PricesIncludeTax, &b.MaxDiscountPct,
		&b.InvoicePrefix, &b.ReceiptHeader, &b.ReceiptFooter, &b.DefaultCustomerID, &b.UpdatedAt)
	return b, err
}

// Location returns the tenant's time zone (UTC if unknown).
func (b Business) Location() *time.Location {
	if l, err := time.LoadLocation(b.Timezone); err == nil {
		return l
	}
	return time.UTC
}

// JSON is GET /api/settings.
type JSON struct {
	BusinessName      string          `json:"businessName"`
	LegalName         *string         `json:"legalName"`
	Address           *string         `json:"address"`
	Phone             *string         `json:"phone"`
	Email             *string         `json:"email"`
	Logo              *string         `json:"logo"`
	Currency          string          `json:"currency"`
	Locale            string          `json:"locale"`
	Timezone          string          `json:"timezone"`
	TaxLabel          string          `json:"taxLabel"`
	TaxRegistrationNo *string         `json:"taxRegistrationNo"`
	TaxSplit          string          `json:"taxSplit"`
	PricesIncludeTax  bool            `json:"pricesIncludeTax"`
	MaxDiscountPct    decimal.Decimal `json:"maxDiscountPct"`
	InvoicePrefix     string          `json:"invoicePrefix"`
	ReceiptHeader     *string         `json:"receiptHeader"`
	ReceiptFooter     *string         `json:"receiptFooter"`
	DefaultCustomerID *int64          `json:"defaultCustomerId"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

func toJSON(b Business) JSON {
	return JSON{BusinessName: b.Name, LegalName: b.LegalName, Address: b.Address, Phone: b.Phone, Email: b.Email,
		Logo: files.URLPtr(b.LogoFileID, files.PurposeTenantLogo), Currency: b.Currency, Locale: b.Locale,
		Timezone: b.Timezone, TaxLabel: b.TaxLabel, TaxRegistrationNo: b.TaxRegistrationNo, TaxSplit: b.TaxSplit,
		PricesIncludeTax: b.PricesIncludeTax, MaxDiscountPct: b.MaxDiscountPct, InvoicePrefix: b.InvoicePrefix,
		ReceiptHeader: b.ReceiptHeader, ReceiptFooter: b.ReceiptFooter, DefaultCustomerID: b.DefaultCustomerID,
		UpdatedAt: b.UpdatedAt}
}

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	var b Business
	err := a.ro(r.Context(), tenant(r), func(tx tenancy.TenantTx) error {
		var err error
		b, err = LoadBusiness(r.Context(), tx)
		return err
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toJSON(b))
}

// ------------------------------------------------------------------ PUT

type settingsIn struct {
	BusinessName      *string   `json:"businessName"`
	LegalName         *string   `json:"legalName"`
	Address           *string   `json:"address"`
	Phone             *string   `json:"phone"`
	Email             *string   `json:"email"`
	Logo              *string   `json:"logo"`
	Currency          *string   `json:"currency"`
	Locale            *string   `json:"locale"`
	Timezone          *string   `json:"timezone"`
	TaxLabel          *string   `json:"taxLabel"`
	TaxRegistrationNo *string   `json:"taxRegistrationNo"`
	TaxSplit          *string   `json:"taxSplit"`
	PricesIncludeTax  *bool     `json:"pricesIncludeTax"`
	MaxDiscountPct    money.Dec `json:"maxDiscountPct"`
	InvoicePrefix     *string   `json:"invoicePrefix"`
	ReceiptHeader     *string   `json:"receiptHeader"`
	ReceiptFooter     *string   `json:"receiptFooter"`
	DefaultCustomerID money.Int `json:"defaultCustomerId"`
}

var (
	localePattern = regexp.MustCompile(`^en-([A-Z]{2})$`)
	prefixPattern = regexp.MustCompile(`^[A-Za-z0-9-]{0,20}$`)
	taxRegPattern = regexp.MustCompile(`^[A-Za-z0-9/-]{1,30}$`)
)

// cleanText trims and checks length; newlines are allowed only where a
// field is multi-line (receipt header/footer, address). Other control
// characters are always refused.
func cleanText(s string, maxRunes int, multiline bool) (string, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	for _, r := range s {
		if (r < 0x20 && (!multiline || r != '\n')) || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return "", false
		}
	}
	return s, utf8.RuneCountInString(s) <= maxRunes
}

func optText(f map[string]string, key string, v *string, cur **string, maxRunes int, multiline bool) {
	if v == nil {
		return
	}
	s, ok := cleanText(*v, maxRunes, multiline)
	if !ok {
		f[key] = "Too long or contains control characters."
		return
	}
	if s == "" {
		*cur = nil
		return
	}
	*cur = &s
}

func (a *API) apply(in settingsIn, b *Business) (changed []string, err error) {
	f := map[string]string{}
	before := *b
	if in.BusinessName != nil {
		if s, ok := cleanText(*in.BusinessName, 120, false); ok && s != "" {
			b.Name = s
		} else {
			f["businessName"] = "Business name is required (at most 120 characters)."
		}
	}
	optText(f, "legalName", in.LegalName, &b.LegalName, 200, false)
	optText(f, "address", in.Address, &b.Address, 500, true)
	optText(f, "phone", in.Phone, &b.Phone, 40, false)
	optText(f, "receiptHeader", in.ReceiptHeader, &b.ReceiptHeader, 500, true)
	optText(f, "receiptFooter", in.ReceiptFooter, &b.ReceiptFooter, 500, true)
	if in.Email != nil {
		e := strings.TrimSpace(*in.Email)
		switch {
		case e == "":
			b.Email = nil
		case mailer.ValidEmail(e):
			b.Email = &e
		default:
			f["email"] = "Enter a valid email address."
		}
	}
	if in.Currency != nil {
		c := strings.ToUpper(strings.TrimSpace(*in.Currency))
		if u, err := currency.ParseISO(c); err == nil && u.String() == c {
			b.Currency = c
		} else {
			f["currency"] = "Use an ISO 4217 currency code such as INR."
		}
	}
	if in.Locale != nil {
		l := strings.TrimSpace(*in.Locale)
		m := localePattern.FindStringSubmatch(l)
		if m == nil {
			f["locale"] = "Use an English number format such as en-IN or en-US."
		} else if _, err := language.ParseRegion(m[1]); err != nil {
			f["locale"] = "Unknown region in locale."
		} else {
			b.Locale = l
		}
	}
	if in.Timezone != nil {
		tz := strings.TrimSpace(*in.Timezone)
		if _, err := time.LoadLocation(tz); err != nil || tz == "" || tz == "Local" || len(tz) > 64 {
			f["timezone"] = "Use an IANA time zone such as Asia/Kolkata."
		} else {
			b.Timezone = tz
		}
	}
	if in.TaxLabel != nil {
		if s, ok := cleanText(*in.TaxLabel, 20, false); ok && s != "" {
			b.TaxLabel = s
		} else {
			f["taxLabel"] = "Tax label is required (at most 20 characters)."
		}
	}
	if in.TaxRegistrationNo != nil {
		s := strings.ToUpper(strings.TrimSpace(*in.TaxRegistrationNo))
		switch {
		case s == "":
			b.TaxRegistrationNo = nil
		case taxRegPattern.MatchString(s):
			b.TaxRegistrationNo = &s
		default:
			f["taxRegistrationNo"] = "Letters, digits, / and - only (at most 30)."
		}
	}
	if in.TaxSplit != nil {
		switch *in.TaxSplit {
		case "none", "cgst_sgst", "igst":
			b.TaxSplit = *in.TaxSplit
		default:
			f["taxSplit"] = "Use none, cgst_sgst or igst."
		}
	}
	if in.PricesIncludeTax != nil {
		b.PricesIncludeTax = *in.PricesIncludeTax
	}
	if in.MaxDiscountPct.Set {
		d := in.MaxDiscountPct.D
		if !in.MaxDiscountPct.Valid || d.IsNegative() || d.GreaterThan(decimal.NewFromInt(100)) {
			f["maxDiscountPct"] = "Use a percentage between 0 and 100."
		} else {
			b.MaxDiscountPct = d.Round(2)
		}
	}
	if in.InvoicePrefix != nil {
		p := strings.TrimRight(strings.TrimSpace(*in.InvoicePrefix), "-")
		if prefixPattern.MatchString(p) {
			b.InvoicePrefix = p
		} else {
			f["invoicePrefix"] = "Letters, digits and - only (at most 20)."
		}
	}
	if in.DefaultCustomerID.Set {
		if in.DefaultCustomerID.Valid && in.DefaultCustomerID.V > 0 {
			v := in.DefaultCustomerID.V
			b.DefaultCustomerID = &v
		} else {
			b.DefaultCustomerID = nil
		}
	}
	if len(f) > 0 {
		return nil, apperr.Validation(f)
	}
	for _, c := range []struct {
		name string
		diff bool
	}{
		{"businessName", before.Name != b.Name}, {"legalName", neq(before.LegalName, b.LegalName)},
		{"address", neq(before.Address, b.Address)}, {"phone", neq(before.Phone, b.Phone)},
		{"email", neq(before.Email, b.Email)}, {"currency", before.Currency != b.Currency},
		{"locale", before.Locale != b.Locale}, {"timezone", before.Timezone != b.Timezone},
		{"taxLabel", before.TaxLabel != b.TaxLabel}, {"taxRegistrationNo", neq(before.TaxRegistrationNo, b.TaxRegistrationNo)},
		{"taxSplit", before.TaxSplit != b.TaxSplit}, {"pricesIncludeTax", before.PricesIncludeTax != b.PricesIncludeTax},
		{"maxDiscountPct", !before.MaxDiscountPct.Equal(b.MaxDiscountPct)}, {"invoicePrefix", before.InvoicePrefix != b.InvoicePrefix},
		{"receiptHeader", neq(before.ReceiptHeader, b.ReceiptHeader)}, {"receiptFooter", neq(before.ReceiptFooter, b.ReceiptFooter)},
		{"defaultCustomerId", neqInt(before.DefaultCustomerID, b.DefaultCustomerID)},
	} {
		if c.diff {
			changed = append(changed, c.name)
		}
	}
	return changed, nil
}

func neq(a, b *string) bool { return (a == nil) != (b == nil) || (a != nil && *a != *b) }

func neqInt(a, b *int64) bool { return (a == nil) != (b == nil) || (a != nil && *a != *b) }

const qUpdate = `UPDATE settings SET business_name = $1, legal_name = $2, address = $3, phone = $4, email = $5,
		logo_file_id = $6, currency = $7, locale = $8, timezone = $9, tax_label = $10, tax_registration_no = $11,
		tax_split = $12, prices_include_tax_default = $13, max_discount_pct_without_override = $14,
		invoice_prefix = $15, receipt_header = $16, receipt_footer = $17, default_customer_id = $18, updated_at = now()
	WHERE id = 1`

func (a *API) put(w http.ResponseWriter, r *http.Request) {
	var in settingsIn
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	t, p := tenant(r), principal(r)
	var logo *uuid.UUID
	if err := a.ro(r.Context(), t, func(tx tenancy.TenantTx) error {
		return tx.QueryRow(r.Context(), `SELECT logo_file_id FROM settings WHERE id = 1`).Scan(&logo)
	}); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ch, err := a.d.Files.Prepare(r.Context(), t, "logo", in.Logo, logo, files.PurposeTenantLogo)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	var out Business
	err = a.rw(r.Context(), t, func(tx tenancy.TenantTx) error {
		ctx := r.Context()
		b, err := scanBusiness(tx.QueryRow(ctx, qLoad+` FOR UPDATE`))
		if err != nil {
			return err
		}
		changed, err := a.apply(in, &b)
		if err != nil {
			return err
		}
		if in.DefaultCustomerID.Set && b.DefaultCustomerID != nil {
			var ok bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM customers WHERE id = $1 AND deleted_at IS NULL)`, *b.DefaultCustomerID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return apperr.Invalid("defaultCustomerId", "Customer not found.")
			}
		}
		if !ch.Keep() {
			if b.LogoFileID, err = ch.Apply(ctx, tx, b.LogoFileID, p.UserID); err != nil {
				return err
			}
			changed = append(changed, "logo")
		}
		if _, err := tx.Exec(ctx, qUpdate, b.Name, b.LegalName, b.Address, b.Phone, b.Email, b.LogoFileID, b.Currency,
			b.Locale, b.Timezone, b.TaxLabel, b.TaxRegistrationNo, b.TaxSplit, b.PricesIncludeTax, b.MaxDiscountPct,
			b.InvoicePrefix, b.ReceiptHeader, b.ReceiptFooter, b.DefaultCustomerID); err != nil {
			return err
		}
		if len(changed) > 0 {
			if err := audit.Write(ctx, tx, audit.Entry{ActorID: p.UserID, Action: "settings.update", Entity: "settings",
				EntityID: "1", After: map[string]any{"fields": changed}, RequestID: httpx.RequestID(ctx), UserAgent: r.UserAgent()}); err != nil {
				return err
			}
		}
		out, err = LoadBusiness(ctx, tx)
		return err
	})
	ch.Done(r.Context(), err)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toJSON(out))
}
