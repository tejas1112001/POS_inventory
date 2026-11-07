package mailtmpl

// RestoreNoticeTemplate is the platform outbox template name of the notice
// sent after a replace restore (OPERATIONS §6.3 step 8).
const RestoreNoticeTemplate = "tenant_restored"

// RestoreNotice tells a shop's owner that the shop was restored from a
// backup and that everyone must sign in again.
type RestoreNotice struct {
	TenantName   string
	PlatformName string
	BackupTime   string // when the export was taken, formatted (UTC)
	TenantURL    string
}

// RenderRestoreNotice renders the restore notice (the generic notice
// layout of the billing emails).
func RenderRestoreNotice(d RestoreNotice) (Rendered, error) {
	shop, platform := oneLine(d.TenantName, 120), oneLine(d.PlatformName, 60)
	v := billingView{page: page{Footnote: "Sent by " + platform + " about the account of " + shop + "."}}
	v.Heading = "Your shop was restored from a backup"
	v.Lines = []string{
		"The data of " + shop + " was restored from a backup taken on " + oneLine(d.BackupTime, 40) + ". Changes made after that time are not in the shop.",
		"Everyone has been signed out and must sign in again. Invitations and password-reset links sent before the restore no longer work; send them again.",
	}
	if link := webLink(d.TenantURL); link != "" {
		v.Action, v.Link = "Sign in", link
	}
	subj := shop + " was restored from a backup"
	v.Title = subject(subj)
	text, html, err := render("billing", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}
