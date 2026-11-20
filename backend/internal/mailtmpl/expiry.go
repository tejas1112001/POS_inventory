package mailtmpl

// ExpiryDigestTemplate is the owner's daily expiry email (industry P3 item 7).
const ExpiryDigestTemplate = "expiry_digest"

// ExpiryItem is one batch in the email.
type ExpiryItem struct {
	Section, Name, Batch, Expiry, Store, Qty string
}

// ExpiryDigest is the email data. Items holds at most the first 50.
type ExpiryDigest struct {
	BusinessName, Name, Date, Link           string
	Expired, Expiring, Quarantined, Recalled string // units, formatted (H3: may have decimals)
	WindowDays, Count                        int
	Items                                    []ExpiryItem
}

type expiryView struct {
	page
	ExpiryDigest
	More int
}

// RenderExpiryDigest renders the daily expiry email.
func RenderExpiryDigest(d ExpiryDigest) (Rendered, error) {
	d.BusinessName, d.Name, d.Date = oneLine(d.BusinessName, 120), oneLine(d.Name, 120), oneLine(d.Date, 40)
	if d.Name == "" {
		d.Name = "there"
	}
	d.Link = webLink(d.Link)
	items := make([]ExpiryItem, 0, len(d.Items))
	for i, it := range d.Items {
		if i >= 50 {
			break
		}
		items = append(items, ExpiryItem{Section: oneLine(it.Section, 20), Name: oneLine(it.Name, 160), Batch: oneLine(it.Batch, 60),
			Expiry: oneLine(it.Expiry, 20), Store: oneLine(it.Store, 120), Qty: oneLine(it.Qty, 20)})
	}
	d.Items = items
	v := expiryView{ExpiryDigest: d, More: max(d.Count-len(items), 0)}
	subj := "Expiry check at " + d.BusinessName + ": " + oneLine(d.Expired, 20) + " expired, " + oneLine(d.Expiring, 20) + " expiring soon"
	v.Title = subject(subj)
	v.Footnote = "Sent every morning while stock is expired or expiring. Turn it off in Settings → Batches."
	text, html, err := render("expiry", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}
