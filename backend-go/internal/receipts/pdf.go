package receipts

import (
	"bytes"
	_ "embed" // fonts
	"errors"
	"fmt"
	"strconv"

	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/col"
	"github.com/johnfercher/maroto/v2/pkg/components/image"
	"github.com/johnfercher/maroto/v2/pkg/components/line"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/extension"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/consts/pagesize"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/fontrepository"
	"github.com/johnfercher/maroto/v2/pkg/props"
)

// Noto Sans (SIL OFL 1.1, see fonts/OFL.txt) covers ₹ and the other
// currency symbols.
var (
	//go:embed fonts/NotoSans-Regular.ttf
	fontRegular []byte
	//go:embed fonts/NotoSans-Bold.ttf
	fontBold []byte
)

const family = "noto"

var (
	grey  = &props.Color{Red: 110, Green: 110, Blue: 110}
	red   = &props.Color{Red: 200, Green: 30, Blue: 30}
	shade = &props.Cell{BackgroundColor: &props.Color{Red: 240, Green: 240, Blue: 240}}
)

func tp(size float64, opts ...func(*props.Text)) props.Text {
	p := props.Text{Family: family, Size: size, Top: 0.5}
	for _, o := range opts {
		o(&p)
	}
	return p
}

func bold(p *props.Text)   { p.Style = fontstyle.Bold }
func right(p *props.Text)  { p.Align = align.Right }
func center(p *props.Text) { p.Align = align.Center }
func muted(p *props.Text)  { p.Color = grey }

// logoExt guesses the stored logo's format (logos are re-encoded to PNG or
// JPEG on upload).
func logoExt(b []byte) (extension.Type, bool) {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return extension.Png, true
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return extension.Jpg, true
	default:
		return "", false
	}
}

// RenderPDF draws an A4 receipt. logo may be nil.
func RenderPDF(v View, logo []byte) ([]byte, error) {
	fonts, err := fontrepository.New().
		AddUTF8FontFromBytes(family, fontstyle.Normal, fontRegular).
		AddUTF8FontFromBytes(family, fontstyle.Bold, fontBold).
		Load()
	if err != nil {
		return nil, err
	}
	cfg := config.NewBuilder().
		WithPageSize(pagesize.A4).
		WithLeftMargin(12).WithRightMargin(12).WithTopMargin(12).
		WithCustomFonts(fonts).
		WithDefaultFont(&props.Font{Family: family, Size: 9}).
		WithTitle(v.Title+" "+v.Reference, true).
		WithCreator(v.BusinessName, true).
		Build()
	m := maroto.New(cfg)

	if v.Void {
		m.AddAutoRow(text.NewCol(12, "VOID", tp(28, bold, center, func(p *props.Text) { p.Color = red })))
		msg := "This sale was voided"
		if v.VoidedAt != "" {
			msg += " on " + v.VoidedAt
		}
		if v.VoidReason != "" {
			msg += ": " + v.VoidReason
		}
		m.AddAutoRow(text.NewCol(12, msg+".", tp(9, center, func(p *props.Text) { p.Color = red })))
	}

	// Business header with the logo.
	var head []core.Col
	info := 12
	if ext, ok := logoExt(logo); ok {
		head = append(head, image.NewFromBytesCol(2, logo, ext, props.Rect{Percent: 90}))
		info = 10
	}
	block := []string{}
	if v.LegalName != "" && v.LegalName != v.BusinessName {
		block = append(block, v.LegalName)
	}
	block = append(block, v.Address...)
	if v.Phone != "" || v.Email != "" {
		block = append(block, joinNonEmpty(" · ", v.Phone, v.Email))
	}
	if v.TaxID != "" {
		block = append(block, v.TaxID)
	}
	c := col.New(info)
	c.Add(text.New(v.BusinessName, tp(15, bold)))
	for i, l := range block {
		c.Add(text.New(l, tp(8.5, muted, func(p *props.Text) { p.Top = 8 + float64(i)*4.2 })))
	}
	head = append(head, c)
	m.AddRow(max(18, 9+4.2*float64(len(block))), head...)
	m.AddRow(3, line.NewCol(12))

	m.AddAutoRow(
		text.NewCol(6, v.Title, tp(12, bold)),
		text.NewCol(6, v.Reference, tp(12, bold, right)),
	)
	m.AddAutoRow(
		text.NewCol(6, "Store: "+joinNonEmpty(", ", v.StoreName, v.StoreAddress, v.StorePhone), tp(8.5)),
		text.NewCol(6, v.Date, tp(8.5, right)),
	)
	cust := "Customer: " + v.Customer
	if v.CustomerEmail != "" {
		cust += " (" + v.CustomerEmail + ")"
	}
	m.AddAutoRow(
		text.NewCol(6, cust, tp(8.5)),
		text.NewCol(6, "Cashier: "+v.Cashier, tp(8.5, right)),
	)
	for _, h := range v.Header {
		m.AddAutoRow(text.NewCol(12, h, tp(8.5, center, muted)))
	}
	m.AddRow(3)

	// Items.
	m.AddAutoRow(
		text.NewCol(5, "Item", tp(8.5, bold)),
		text.NewCol(1, "Qty", tp(8.5, bold, right)),
		text.NewCol(2, "Price", tp(8.5, bold, right)),
		text.NewCol(1, "Disc.", tp(8.5, bold, right)),
		text.NewCol(1, "Tax", tp(8.5, bold, right)),
		text.NewCol(2, "Total", tp(8.5, bold, right)),
	).WithStyle(shade)
	for _, l := range v.Lines {
		name := l.Name
		if l.Attributes != "" {
			name += " (" + l.Attributes + ")"
		}
		if l.SKU != "" {
			name += " · SKU " + l.SKU
		}
		disc := ""
		if l.HasDiscount {
			disc = l.Discount
		}
		m.AddAutoRow(
			text.NewCol(5, name, tp(8.5)),
			text.NewCol(1, strconv.Itoa(l.Qty), tp(8.5, right)),
			text.NewCol(2, l.UnitPrice, tp(8.5, right)),
			text.NewCol(1, disc, tp(8.5, right)),
			text.NewCol(1, l.TaxRate, tp(8.5, right)),
			text.NewCol(2, l.Total, tp(8.5, right)),
		)
	}
	m.AddRow(2)
	m.AddRow(3, line.NewCol(12))

	// Totals.
	total := func(label, value string, opts ...func(*props.Text)) {
		m.AddAutoRow(col.New(6),
			text.NewCol(3, label, tp(9, opts...)),
			text.NewCol(3, value, tp(9, append(opts, right)...)))
	}
	total("Subtotal", v.Subtotal)
	if v.HasDiscount {
		total("Discount", "-"+v.Discount)
	}
	for _, r := range v.TaxRows {
		total(r.Label+" on "+r.Taxable, r.Amount, muted)
	}
	total("Tax", v.Tax)
	if v.HasShipping {
		total("Shipping", v.Shipping)
	}
	total("Total", v.Total, bold)
	total("Paid", v.Paid)
	if v.HasTendered {
		total("Tendered", v.Tendered)
		total("Change", v.Change)
	}
	if v.HasDue {
		total("Due", v.Due, bold)
	}
	if v.PaymentMethod != "" {
		total("Payment", v.PaymentMethod+" ("+v.PaymentStatus+")")
	}
	m.AddRow(5)
	for _, f := range v.Footer {
		m.AddAutoRow(text.NewCol(12, f, tp(8.5, center, muted)))
	}
	m.AddAutoRow(text.NewCol(12, "Thank you for your purchase.", tp(10, bold, center)))
	m.AddAutoRow(text.NewCol(12, fmt.Sprintf("Receipt %d · version %d", v.ReceiptID, v.Version), tp(7, center, muted)))

	doc, err := m.Generate()
	if err != nil {
		return nil, err
	}
	b := doc.GetBytes()
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		return nil, errors.New("receipts: renderer returned no PDF")
	}
	return b, nil
}

func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	s := ""
	for i, p := range out {
		if i > 0 {
			s += sep
		}
		s += p
	}
	return s
}
