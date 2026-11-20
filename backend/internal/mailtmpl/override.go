package mailtmpl

// OverrideNoticeTemplate is the platform inbox notice of a tenant limit
// override or feature flag change (UI S9).
const OverrideNoticeTemplate = "override_notice"

// OverrideNotice describes one change.
type OverrideNotice struct {
	PlatformName     string
	Change           string // "set" or "revoke"
	TenantName, Slug string
	Label, Value     string
	Reason, Expires  string
	Actor, At        string
	RevokeReason     string
}

type overrideView struct {
	page
	Heading                        string
	TenantName, Slug, Label, Value string
	Reason, Expires, Actor, At     string
	Revoked                        bool
	RevokeReason                   string
}

// RenderOverrideNotice renders the notice. All values are data.
func RenderOverrideNotice(d OverrideNotice) (Rendered, error) {
	v := overrideView{TenantName: oneLine(d.TenantName, 120), Slug: oneLine(d.Slug, 40), Label: oneLine(d.Label, 60),
		Value: oneLine(d.Value, 40), Reason: oneLine(d.Reason, 500), Expires: oneLine(d.Expires, 40),
		Actor: oneLine(d.Actor, 254), At: oneLine(d.At, 40), Revoked: d.Change == "revoke", RevokeReason: oneLine(d.RevokeReason, 500)}
	verb := "set"
	if v.Revoked {
		verb = "revoked"
	}
	v.Heading = "Override " + verb + ": " + v.Label
	subj := "Override " + verb + " for " + v.TenantName + ": " + v.Label
	v.Title = subject(subj)
	v.Footnote = "Sent by the " + oneLine(d.PlatformName, 60) + " console. Every override change sends this notice."
	text, html, err := render("override", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}
