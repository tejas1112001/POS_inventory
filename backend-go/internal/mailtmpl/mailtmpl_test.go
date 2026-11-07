package mailtmpl

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name string, r Rendered) {
	t.Helper()
	got := "Subject: " + r.Subject + "\n\n--- text ---\n" + r.Text + "\n--- html ---\n" + r.HTML
	path := filepath.Join("testdata", name+".golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run go test ./internal/mailtmpl -update to create golden files")
	require.Equal(t, string(want), got)
}

const hostile = "<script>alert('x')</script> {{.Secret}} \r\nBcc: victim@evil.test"

func checkSafe(t *testing.T, r Rendered) {
	t.Helper()
	m := mailer.Message{From: mailer.Address{Email: "a@b.test"}, To: []mailer.Address{{Email: "c@d.test"}},
		Subject: r.Subject, TextBody: r.Text, HTMLBody: r.HTML}
	require.NoError(t, m.Validate(), "the subject never carries CR/LF")
	require.NotContains(t, r.HTML, "<script>", "HTML is escaped")
	require.NotContains(t, r.Subject, "\n")
	require.NotContains(t, r.Text, "<no value>", "data is never executed as a template")
	require.NotContains(t, r.HTML, "<no value>")
}

// checkLiteral: hostile template syntax in data comes out as plain text.
func checkLiteral(t *testing.T, r Rendered) {
	t.Helper()
	require.Contains(t, r.Text, "{{.Secret}}")
	require.Contains(t, r.HTML, "{{.Secret}}")
}

func TestReceiptEmail(t *testing.T) {
	d := Receipt{BusinessName: "Asha General Stores", LogoURL: "https://asha.pos.test/files/t/0b0e1b4a-0000-4000-8000-000000000001",
		CustomerName: "Kiran", Reference: "POS-INV-000042", Date: "20 Oct 2026, 23:30", Total: "₹1,859.93",
		StoreName: "Main Store", StoreAddress: "12 MG Road, Pune", StorePhone: "020 1234",
		Header: []string{"Welcome!"}, Footer: []string{"No refunds after 7 days"}, Attached: true}
	r, err := RenderReceipt(d)
	require.NoError(t, err)
	require.Equal(t, "Your receipt from Asha General Stores (POS-INV-000042)", r.Subject)
	golden(t, "receipt", r)
	checkSafe(t, r)

	// Part payment (S10 item 5): each payment, the balance and its date.
	part := d
	part.Payments, part.Due, part.DueDate = []string{"Cash: ₹500.00", "UPI: ₹300.00 (txn 4411)"}, "₹1,059.93", "19 Nov 2026"
	pr, err := RenderReceipt(part)
	require.NoError(t, err)
	require.Contains(t, pr.Text, "Paid:    UPI: ₹300.00 (txn 4411)")
	require.Contains(t, pr.Text, "Balance due: ₹1,059.93 by 19 Nov 2026")
	require.Contains(t, pr.HTML, "<strong>₹1,059.93</strong> by 19 Nov 2026")
	checkSafe(t, pr)

	d.BusinessName, d.CustomerName, d.StoreName = hostile, hostile, hostile
	d.Header, d.Footer = []string{hostile}, []string{hostile}
	d.LogoURL = "javascript:alert(1)"
	d.Attached, d.Void = false, true
	r, err = RenderReceipt(d)
	require.NoError(t, err)
	golden(t, "receipt_hostile", r)
	checkSafe(t, r)
	checkLiteral(t, r)
	require.Contains(t, r.HTML, "&lt;script&gt;")
	require.NotContains(t, r.HTML, "javascript:", "unsafe URLs are neutralised")
	require.True(t, strings.HasPrefix(r.Subject, "Voided receipt from"))

	d.BusinessName = strings.Repeat("Long name ", 50)
	r, err = RenderReceipt(d)
	require.NoError(t, err)
	checkSafe(t, r)
}

func TestLinkEmails(t *testing.T) {
	d := Link{Name: "Ravi", BusinessName: "Asha General Stores", PlatformName: "ShopCloud",
		Link: "https://asha.pos.test/reset-password#token=abc", Expires: "1 hour"}
	r, err := RenderPasswordReset(d)
	require.NoError(t, err)
	require.Equal(t, "Reset your password for Asha General Stores", r.Subject)
	golden(t, "password_reset", r)
	checkSafe(t, r)

	d.Link, d.Expires = "https://asha.pos.test/accept-invite#token=abc", "7 days"
	r, err = RenderUserInvite(d)
	require.NoError(t, err)
	golden(t, "user_invite", r)
	checkSafe(t, r)

	d.Name, d.BusinessName = hostile, hostile
	r, err = RenderUserInvite(d)
	require.NoError(t, err)
	golden(t, "user_invite_hostile", r)
	checkSafe(t, r)
	checkLiteral(t, r)
}

func TestWelcomeEmail(t *testing.T) {
	d := Welcome{Name: "Asha", TenantName: "Asha General Stores", TenantURL: "https://asha.pos.test/login", PlatformName: "ShopCloud",
		Link: "https://asha.pos.test/accept-invite#token=abc", Expires: "7 days"}
	r, err := RenderWelcome(d)
	require.NoError(t, err)
	golden(t, "welcome", r)
	checkSafe(t, r)
	d.Link = ""
	r, err = RenderWelcome(d)
	require.NoError(t, err)
	require.Contains(t, r.Text, "Sign in with the password you already set.")
	require.NotContains(t, r.HTML, "accept-invite")
}

func TestBillingNotices(t *testing.T) {
	base := BillingNotice{TenantName: "Asha General Stores", PlatformName: "POS Cloud", Date: "31 Oct 2026",
		TenantURL: "https://asha.pos.test/login", PayURL: "https://rzp.io/i/abc123"}
	kinds := map[string]string{
		"trial_ending_7":        "Your POS Cloud trial for Asha General Stores ends in 7 days",
		"trial_ending_1":        "Your POS Cloud trial for Asha General Stores ends in 1 day",
		"subscription_ending_3": "Your POS Cloud subscription for Asha General Stores ends in 3 days",
		"payment_failed":        "Payment failed for Asha General Stores",
		"grace_ending":          "Last day to pay for Asha General Stores",
		"suspended":             "Asha General Stores is suspended",
		"pending_deletion":      "Asha General Stores is scheduled for deletion",
		"deletion_warning_14":   "Asha General Stores will be deleted in 14 days",
		"deletion_warning_1":    "Asha General Stores will be deleted in 1 day",
		"reactivated":           "Asha General Stores is active again",
		"renewal_success":       "Payment received for Asha General Stores",
	}
	for kind, subj := range kinds {
		d := base
		d.Kind = kind
		r, err := RenderBillingNotice(d)
		require.NoError(t, err, kind)
		require.Equal(t, subj, r.Subject, kind)
		checkSafe(t, r)
	}
	d := base
	d.Kind = "payment_failed"
	r, err := RenderBillingNotice(d)
	require.NoError(t, err)
	golden(t, "billing_payment_failed", r)
	require.Contains(t, r.Text, "Pay now: https://rzp.io/i/abc123")

	// Suspended without a payment link: no sign-in link (nobody can sign in).
	d.Kind, d.PayURL = "suspended", ""
	r, err = RenderBillingNotice(d)
	require.NoError(t, err)
	require.NotContains(t, r.Text, "https://asha.pos.test")
	require.Contains(t, r.Text, "Reply to this email")

	_, err = RenderBillingNotice(BillingNotice{Kind: "free_money"})
	require.Error(t, err)

	h := base
	h.Kind, h.TenantName, h.PayURL = "suspended", hostile, "javascript:alert(1)"
	r, err = RenderBillingNotice(h)
	require.NoError(t, err)
	checkSafe(t, r)
	require.NotContains(t, r.Text+r.HTML, "javascript:", "only http(s) links")
	golden(t, "billing_hostile", r)
}

func TestRestoreNotice(t *testing.T) {
	r, err := RenderRestoreNotice(RestoreNotice{TenantName: "Asha General Stores", PlatformName: "POS Cloud",
		BackupTime: "24 Oct 2026 18:30 UTC", TenantURL: "https://asha.pos.test/login"})
	require.NoError(t, err)
	require.Equal(t, "Asha General Stores was restored from a backup", r.Subject)
	require.Contains(t, r.Text, "must sign in again")
	require.Contains(t, r.Text, "https://asha.pos.test")
	checkSafe(t, r)
	golden(t, "restore_notice", r)

	h, err := RenderRestoreNotice(RestoreNotice{TenantName: "<script>x</script>\r\nBcc: a@b", PlatformName: "P",
		BackupTime: "now", TenantURL: "javascript:alert(1)"})
	require.NoError(t, err)
	checkSafe(t, h)
	require.NotContains(t, h.HTML, "<script>")
	require.NotContains(t, h.Subject, "\n")
	require.NotContains(t, h.HTML, "javascript:")
}

func TestLowStockEmail(t *testing.T) {
	d := LowStock{BusinessName: "Asha General Stores", Name: "Asha", Date: "05 Jan 2030", Link: "https://asha.pos.test/inventory", Count: 52}
	for i := range 52 {
		d.Items = append(d.Items, LowStockItem{Name: "Item", SKU: "SKU", Store: "Main", Qty: "1", Alert: "5"})
		_ = i
	}
	r, err := RenderLowStock(d)
	require.NoError(t, err)
	require.Equal(t, "52 items are low on stock at Asha General Stores", r.Subject)
	require.Contains(t, r.Text, "… and 2 more.")
	require.Contains(t, r.HTML, "https://asha.pos.test/inventory")
	checkSafe(t, r)
	d.Count, d.Items, d.Link = 1, d.Items[:1], "javascript:alert(1)"
	r, err = RenderLowStock(d)
	require.NoError(t, err)
	require.Equal(t, "1 item is low on stock at Asha General Stores", r.Subject)
	require.NotContains(t, r.HTML, "javascript:")
}
