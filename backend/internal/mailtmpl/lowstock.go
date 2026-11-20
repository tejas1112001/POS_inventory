package mailtmpl

import "strconv"

// LowStockTemplate is the owner's daily low-stock list (S10 item 6).
const LowStockTemplate = "low_stock"

// LowStockItem is one line of the list.
type LowStockItem struct {
	Name, SKU, Store string
	Qty, Alert       string
}

// LowStock is the email data. Items holds at most the first 50; Count is
// the full number.
type LowStock struct {
	BusinessName, Name, Date, Link string
	Count                          int
	Items                          []LowStockItem
}

type lowStockView struct {
	page
	LowStock
	More int
}

// RenderLowStock renders the daily list.
func RenderLowStock(d LowStock) (Rendered, error) {
	d.BusinessName, d.Name, d.Date = oneLine(d.BusinessName, 120), oneLine(d.Name, 120), oneLine(d.Date, 40)
	if d.Name == "" {
		d.Name = "there"
	}
	d.Link = webLink(d.Link)
	items := make([]LowStockItem, 0, len(d.Items))
	for i, it := range d.Items {
		if i >= 50 {
			break
		}
		items = append(items, LowStockItem{Name: oneLine(it.Name, 160), SKU: oneLine(it.SKU, 60), Store: oneLine(it.Store, 120),
			Qty: oneLine(it.Qty, 20), Alert: oneLine(it.Alert, 20)})
	}
	d.Items = items
	v := lowStockView{LowStock: d, More: max(d.Count-len(items), 0)}
	subj := strconv.Itoa(d.Count) + " items are low on stock at " + d.BusinessName
	if d.Count == 1 {
		subj = "1 item is low on stock at " + d.BusinessName
	}
	v.Title = subject(subj)
	v.Footnote = "Sent every morning while items are low. Turn it off in Settings → Notifications."
	text, html, err := render("lowstock", v)
	return Rendered{Subject: subject(subj), Text: text, HTML: html}, err
}
