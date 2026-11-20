package mailtmpl

import "strings"

// Platform outbox template names of the landing page emails.
const (
	FindShopTemplate       = "find_shop"
	ContactRequestTemplate = "contact_request"
)

// FindShop lists the shops linked to an email address ("Find my shop" on
// the landing page). URL is each shop's staff sign-in page.
type FindShop struct {
	PlatformName string
	Shops        []ShopLink
}

// ShopLink is one shop in a FindShop email.
type ShopLink struct {
	Name, URL string
}

type findShopView struct {
	page
	PlatformName string
	Shops        []ShopLink
}

// RenderFindShop renders the "Find my shop" email. Shops with a link that
// is not a web address are left out.
func RenderFindShop(d FindShop) (Rendered, error) {
	platform := oneLine(d.PlatformName, 60)
	v := findShopView{page: page{Footnote: "Sent by " + platform + "."}, PlatformName: platform}
	for _, s := range d.Shops {
		if u := webLink(s.URL); u != "" {
			v.Shops = append(v.Shops, ShopLink{Name: oneLine(s.Name, 120), URL: u})
		}
	}
	subj := "Your shop address on " + platform
	if len(v.Shops) > 1 {
		subj = "Your shop addresses on " + platform
	}
	v.Title = subject(subj)
	text, html, err := render("findshop", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}

// ContactRequest is the notice to the platform inbox about a contact or
// demo request from the landing page. Every field is visitor text.
type ContactRequest struct {
	PlatformName                 string
	ID                           int64
	Demo                         bool
	Name, Email, Business, Phone string
	Message                      string
	Sent                         string // formatted time
}

type contactView struct {
	page
	Heading                            string
	ID                                 int64
	Name, Email, Business, Phone, Sent string
	Message                            []string
}

// RenderContactRequest renders the contact request notice.
func RenderContactRequest(d ContactRequest) (Rendered, error) {
	v := contactView{ID: d.ID, Name: oneLine(d.Name, 120), Email: oneLine(d.Email, 254),
		Business: oneLine(d.Business, 120), Phone: oneLine(d.Phone, 40), Sent: oneLine(d.Sent, 40)}
	for _, l := range strings.Split(strings.ReplaceAll(d.Message, "\r\n", "\n"), "\n") {
		v.Message = append(v.Message, oneLine(l, 2000))
	}
	kind := "Question"
	if d.Demo {
		kind = "Demo request"
	}
	v.Heading = kind + " from the website"
	subj := kind + " from " + v.Name
	if v.Business != "" {
		subj += " (" + v.Business + ")"
	}
	v.Title = subject(subj)
	v.Footnote = "Sent by the " + oneLine(d.PlatformName, 60) + " website contact form."
	text, html, err := render("contact", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}
