package labels

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"strings"

	libbarcode "github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/boombuler/barcode/ean"
	"github.com/phpdave11/gofpdf"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/pdffonts"
)

// MaxLabels is the most labels one PDF may hold (design note §5).
const MaxLabels = 2000

// Label is one printed label.
type Label struct {
	Name, Variant, Price, SKU, Code string
	Batch                           string // industry P3 item 2: "B <no> EXP <MM/YYYY>", printed on the variant/price line
}

// Labels are drawn with gofpdf directly (the library maroto wraps): label
// sheets need exact millimetre positions and one page per sheet, and
// maroto's row flow starts pages through gofpdf's automatic page break,
// which a full sheet ending on the bottom margin trips by float rounding.

// quiet is the blank margin each side of the bars, in modules (GS1 asks
// 11 left / 7 right for EAN-13 and 10 for Code 128).
const quiet = 11

// BarcodeImage renders code as black-and-white bars, one pixel per module
// times scale (EAN-13 when the code is a valid EAN-13, Code 128
// otherwise). It is what the label PDF embeds.
func BarcodeImage(code string, scale, height int) (image.Image, int, error) {
	var (
		bc  libbarcode.Barcode
		err error
	)
	if Symbology(code) == "ean13" {
		bc, err = ean.Encode(code)
	} else {
		bc, err = code128.Encode(code)
	}
	if err != nil {
		return nil, 0, err
	}
	modules := bc.Bounds().Dx()
	img, err := libbarcode.Scale(bc, modules*scale, height)
	if err != nil {
		return nil, 0, err
	}
	return img, modules, nil
}

// band is one horizontal strip of a label.
type band struct {
	kind   string // "name", "line", "bars", "under"
	y, h   float64
	font   float64 // pt
	bold   bool
	center bool
}

// bands splits the label height between the layout's fields; the bars
// take what the text lines leave.
func bands(lay Layout) []band {
	h := lay.LabelHeight
	line := math.Min(math.Max(h*0.15, 3.2), 6)
	font := math.Min(line*2.0, 11) // pt for a line of `line` mm
	small := math.Max(font*0.8, 5)
	pad := 1.0
	var out []band
	y := pad
	if lay.Has(FieldName) {
		out = append(out, band{kind: "name", y: y, h: line, font: font, bold: true})
		y += line
	}
	if lay.Has(FieldVariant) || lay.Has(FieldPrice) {
		out = append(out, band{kind: "line", y: y, h: line, font: font})
		y += line
	}
	tail := 0.0
	if lay.Has(FieldBarcodeText) || lay.Has(FieldSKU) {
		tail = math.Min(math.Max(h*0.12, 2.6), 4.5)
	}
	if lay.Has(FieldBarcode) {
		bh := math.Max(h-y-tail-pad, 4)
		out = append(out, band{kind: "bars", y: y, h: bh})
		y += bh
	}
	if tail > 0 {
		out = append(out, band{kind: "under", y: y, h: tail, font: small, center: true})
	}
	return out
}

func (b band) text(lay Layout, l Label) string {
	join := func(parts ...string) string {
		var keep []string
		for _, p := range parts {
			if p != "" {
				keep = append(keep, p)
			}
		}
		return strings.Join(keep, "   ")
	}
	pick := func(field, v string) string {
		if lay.Has(field) {
			return v
		}
		return ""
	}
	switch b.kind {
	case "name":
		return l.Name
	case "line":
		return join(pick(FieldVariant, l.Variant), pick(FieldPrice, l.Price), l.Batch)
	case "under":
		sku := pick(FieldSKU, l.SKU)
		if lay.Has(FieldBarcode) && sku == l.Code {
			sku = ""
		}
		return join(pick(FieldBarcodeText, l.Code), sku)
	}
	return ""
}

// fit shortens s with "…" until it fits width mm in the current font.
func fit(pdf *gofpdf.Fpdf, s string, width float64) string {
	if pdf.GetStringWidth(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && pdf.GetStringWidth(string(r)+"…") > width {
		r = r[:len(r)-1]
	}
	return strings.TrimSpace(string(r)) + "…"
}

// Render draws the labels on the layout's pages. Labels fill each page
// left to right, top to bottom.
func Render(lay Layout, items []Label) ([]byte, error) {
	if len(items) == 0 {
		return nil, errors.New("labels: nothing to print")
	}
	if len(items) > MaxLabels {
		return nil, errors.New("labels: too many labels")
	}
	pdf := gofpdf.NewCustom(&gofpdf.InitType{
		UnitStr:        "mm",
		OrientationStr: "P",
		Size:           gofpdf.SizeType{Wd: lay.PageWidth, Ht: lay.PageHeight},
	})
	pdf.SetMargins(0, 0, 0)
	pdf.SetCellMargin(0)
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetTitle("Labels", true)
	pdf.SetCreator("POS", true)
	regular, bold := pdffonts.TTF()
	pdf.AddUTF8FontFromBytes(pdffonts.Family, "", regular)
	pdf.AddUTF8FontFromBytes(pdffonts.Family, "B", bold)

	parts := bands(lay)
	images := map[string]bool{}
	perPage := lay.PerPage()
	for i, l := range items {
		if i%perPage == 0 {
			pdf.AddPage()
		}
		slot := i % perPage
		x := lay.MarginLeft + float64(slot%lay.Columns)*(lay.LabelWidth+lay.GapX)
		y := lay.MarginTop + float64(slot/lay.Columns)*(lay.LabelHeight+lay.GapY)
		for _, b := range parts {
			if b.kind == "bars" {
				if err := drawBars(pdf, images, l.Code, x, y+b.y, lay.LabelWidth, b.h); err != nil {
					return nil, err
				}
				continue
			}
			s := b.text(lay, l)
			if s == "" {
				continue
			}
			style, align := "", "LM"
			if b.bold {
				style = "B"
			}
			if b.center {
				align = "CM"
			}
			pdf.SetFont(pdffonts.Family, style, b.font)
			w := lay.LabelWidth - 2
			pdf.SetXY(x+1, y+b.y)
			pdf.CellFormat(w, b.h, fit(pdf, s, w), "", 0, align, false, 0, "")
		}
	}
	if err := pdf.Error(); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := pdf.Output(&out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// drawBars places the bars centred in the band, as wide as the label
// allows with the quiet zones.
func drawBars(pdf *gofpdf.Fpdf, registered map[string]bool, code string, x, y, width, height float64) error {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil
	}
	const scale = 4
	img, modules, err := BarcodeImage(code, scale, 60)
	if err != nil {
		// A code Code 128 cannot carry (non-ASCII): print it as text.
		pdf.SetFont(pdffonts.Family, "", 7)
		pdf.SetXY(x+1, y)
		pdf.CellFormat(width-2, height, fit(pdf, code, width-2), "", 0, "CM", false, 0, "")
		return nil //nolint:nilerr // shown on the label instead
	}
	name := "bc-" + code
	if !registered[name] {
		var buf bytes.Buffer
		// gofpdf reads 8-bit PNGs only.
		gray := image.NewGray(img.Bounds())
		draw.Draw(gray, gray.Bounds(), img, img.Bounds().Min, draw.Src)
		if err := png.Encode(&buf, gray); err != nil {
			return fmt.Errorf("labels: barcode image: %w", err)
		}
		pdf.RegisterImageOptionsReader(name, gofpdf.ImageOptions{ImageType: "PNG"}, &buf)
		registered[name] = true
	}
	module := (width - 2) / float64(modules+2*quiet)
	bw := module * float64(modules)
	pdf.ImageOptions(name, x+(width-bw)/2, y, bw, height, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	return nil
}
