package mailtmpl

// Platform user emails (S10 item 1).
const (
	PlatformInviteTemplate     = "platform_invite"
	PlatformUserNoticeTemplate = "platform_user_notice"
)

// PlatformInvite is the invite to the Super Admin console.
type PlatformInvite struct {
	PlatformName, Name, InvitedBy string
	Link, Expires                 string
}

type platformInviteView struct {
	page
	Name, InvitedBy, Link, Expires, PlatformName string
}

// RenderPlatformInvite renders the invite. The link is the only action.
func RenderPlatformInvite(d PlatformInvite) (Rendered, error) {
	v := platformInviteView{Name: oneLine(d.Name, 120), InvitedBy: oneLine(d.InvitedBy, 120), Link: d.Link,
		Expires: oneLine(d.Expires, 40), PlatformName: oneLine(d.PlatformName, 60)}
	subj := "You are invited to the " + v.PlatformName + " console"
	v.Title = subject(subj)
	v.Footnote = "If you did not expect this, ignore it: the link stops working on its own."
	text, html, err := render("platforminvite", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}

// PlatformUserNotice tells a console user about a change to their account.
type PlatformUserNotice struct {
	PlatformName, Name, Heading string
	Lines                       []string
}

type platformNoticeView struct {
	page
	Name, Heading string
	Lines         []string
}

// RenderPlatformUserNotice renders a change notice.
func RenderPlatformUserNotice(d PlatformUserNotice) (Rendered, error) {
	v := platformNoticeView{Name: oneLine(d.Name, 120), Heading: oneLine(d.Heading, 120)}
	for _, l := range d.Lines {
		v.Lines = append(v.Lines, oneLine(l, 600))
	}
	v.Title = subject(v.Heading)
	v.Footnote = "Sent by the " + oneLine(d.PlatformName, 60) + " console."
	text, html, err := render("platformnotice", v)
	return Rendered{Subject: subject(v.Heading), Text: text, HTML: html}, err
}
