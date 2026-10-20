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
	d := Welcome{Name: "Asha", TenantName: "Asha General Stores", TenantURL: "https://asha.pos.test", PlatformName: "ShopCloud",
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
