// Package mailtmpl renders the transactional emails (EMAIL_AND_RECEIPTS
// §5.4): one English template per message, HTML through html/template
// (contextual escaping) and plain text through text/template. Tenant text
// (business name, header, footer, logo) is data, never template source.
package mailtmpl

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"strings"
	texttemplate "text/template"
	"unicode/utf8"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/mailer"
)

//go:embed templates/*.tmpl
var fsys embed.FS

// Rendered is a ready message body.
type Rendered struct {
	Subject, Text, HTML string
}

var (
	htmlSets = map[string]*htmltemplate.Template{}
	textSets = map[string]*texttemplate.Template{}
)

func init() {
	for _, name := range []string{"receipt", "link", "welcome", "billing"} {
		htmlSets[name] = htmltemplate.Must(htmltemplate.ParseFS(fsys, "templates/layout.html.tmpl", "templates/"+name+".html.tmpl"))
		textSets[name] = texttemplate.Must(texttemplate.New(name+".txt.tmpl").ParseFS(fsys, "templates/"+name+".txt.tmpl"))
	}
}

// oneLine makes data safe for a single line (subject, greeting): control
// characters become spaces, runs of space collapse, length is capped.
func oneLine(s string, maxRunes int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxRunes {
		s = strings.TrimSpace(string([]rune(s)[:maxRunes-1])) + "…"
	}
	return s
}

func lines(in []string) []string {
	out := make([]string, 0, len(in))
	for _, l := range in {
		if l = oneLine(l, 300); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func subject(s string) string { return oneLine(s, mailer.MaxSubjectRunes) }

type page struct {
	Title, Logo, Footnote string
}

func render(name string, data any) (text, html string, err error) {
	var hb, tb bytes.Buffer
	if err := htmlSets[name].ExecuteTemplate(&hb, "layout", data); err != nil {
		return "", "", err
	}
	if err := textSets[name].Execute(&tb, data); err != nil {
		return "", "", err
	}
	return tb.String(), hb.String(), nil
}

// Receipt is the receipt email (kinds receipt and receipt_resend).
type Receipt struct {
	BusinessName, LogoURL, CustomerName string
	Reference, Date, Total              string
	StoreName, StoreAddress, StorePhone string
	Header, Footer                      []string
	Attached, Void                      bool
}

type receiptView struct {
	page
	Receipt
}

// RenderReceipt renders the receipt email.
func RenderReceipt(d Receipt) (Rendered, error) {
	d.BusinessName, d.CustomerName = oneLine(d.BusinessName, 120), oneLine(d.CustomerName, 120)
	if d.CustomerName == "" {
		d.CustomerName = "there"
	}
	d.Reference, d.Date, d.Total = oneLine(d.Reference, 60), oneLine(d.Date, 60), oneLine(d.Total, 60)
	d.StoreName, d.StoreAddress, d.StorePhone = oneLine(d.StoreName, 120), oneLine(d.StoreAddress, 300), oneLine(d.StorePhone, 40)
	d.Header, d.Footer = lines(d.Header), lines(d.Footer)
	subj := "Your receipt from " + d.BusinessName + " (" + d.Reference + ")"
	if d.Void {
		subj = "Voided receipt from " + d.BusinessName + " (" + d.Reference + ")"
	}
	v := receiptView{page{Title: subject(subj), Logo: d.LogoURL,
		Footnote: "You received this email because you made a purchase at " + d.BusinessName + "."}, d}
	text, html, err := render("receipt", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}

// Link is a set-password style email (password reset, invite).
type Link struct {
	Name, BusinessName, PlatformName, Link, Expires string
}

type linkView struct {
	page
	Heading, Name, Intro, Action, Link, Expires, Ignore string
}

func renderLink(d Link, subj string, v linkView) (Rendered, error) {
	v.Name = oneLine(d.Name, 120)
	if v.Name == "" {
		v.Name = "there"
	}
	v.Link, v.Expires = d.Link, oneLine(d.Expires, 40)
	v.page = page{Title: subject(subj), Footnote: "Sent by " + oneLine(d.PlatformName, 60) + " on behalf of " + oneLine(d.BusinessName, 120) + "."}
	text, html, err := render("link", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}

// RenderPasswordReset renders the password reset email.
func RenderPasswordReset(d Link) (Rendered, error) {
	b := oneLine(d.BusinessName, 120)
	return renderLink(d, "Reset your password for "+b, linkView{
		Heading: "Reset your password",
		Intro:   "Someone asked to reset the password of your " + b + " account.",
		Action:  "Choose a new password",
		Ignore:  "If you did not ask for this, ignore this email; your password stays the same.",
	})
}

// RenderUserInvite renders the staff invitation email.
func RenderUserInvite(d Link) (Rendered, error) {
	b := oneLine(d.BusinessName, 120)
	return renderLink(d, "You're invited to "+b, linkView{
		Heading: "You're invited to " + b,
		Intro:   "An account was created for you at " + b + ". Set a password to sign in.",
		Action:  "Set your password",
		Ignore:  "If you were not expecting this, you can ignore this email.",
	})
}

// Welcome is the platform's welcome email to a new tenant owner. Link is
// empty when the owner already has a password.
type Welcome struct {
	Name, TenantName, TenantURL, PlatformName, Link, Expires string
}

type welcomeView struct {
	page
	Welcome
}

// RenderWelcome renders the tenant welcome email.
func RenderWelcome(d Welcome) (Rendered, error) {
	d.Name, d.TenantName, d.PlatformName = oneLine(d.Name, 120), oneLine(d.TenantName, 120), oneLine(d.PlatformName, 60)
	if d.Name == "" {
		d.Name = "there"
	}
	d.Expires = oneLine(d.Expires, 40)
	subj := "Your shop " + d.TenantName + " is ready on " + d.PlatformName
	v := welcomeView{page{Title: subject(subj), Footnote: "Sent by " + d.PlatformName + "."}, d}
	text, html, err := render("welcome", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}
