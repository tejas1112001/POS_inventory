package labels

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Fields a label can show.
const (
	FieldName        = "name"
	FieldVariant     = "variant"
	FieldPrice       = "price"
	FieldSKU         = "sku"
	FieldBarcode     = "barcode"
	FieldBarcodeText = "barcodeText" // the digits under the bars
)

// AllFields is every field, in print order.
var AllFields = []string{FieldName, FieldVariant, FieldPrice, FieldBarcode, FieldBarcodeText, FieldSKU}

// Layout is a label sheet or roll (DB10, A11). Sizes are in millimetres.
type Layout struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	PageWidth   float64  `json:"pageWidth"`
	PageHeight  float64  `json:"pageHeight"`
	MarginTop   float64  `json:"marginTop"`
	MarginLeft  float64  `json:"marginLeft"`
	LabelWidth  float64  `json:"labelWidth"`
	LabelHeight float64  `json:"labelHeight"`
	Columns     int      `json:"columns"`
	Rows        int      `json:"rows"`
	GapX        float64  `json:"gapX"`
	GapY        float64  `json:"gapY"`
	Fields      []string `json:"fields"`
	Builtin     bool     `json:"builtin"`
}

// Presets are the built-in layouts (final sizes come from the hardware
// checklist, design note §0.1).
var Presets = []Layout{
	{ID: "a4-3x8", Name: "A4 sheet, 3 × 8 (65 × 34 mm)", PageWidth: 210, PageHeight: 297, MarginTop: 12.5, MarginLeft: 5,
		LabelWidth: 65, LabelHeight: 34, Columns: 3, Rows: 8, GapX: 2.5, GapY: 0, Fields: defaultFields(), Builtin: true},
	{ID: "roll-50x25", Name: "Label roll 50 × 25 mm", PageWidth: 50, PageHeight: 25, LabelWidth: 50, LabelHeight: 25,
		Columns: 1, Rows: 1, Fields: defaultFields(), Builtin: true},
	{ID: "roll-38x25", Name: "Label roll 38 × 25 mm", PageWidth: 38, PageHeight: 25, LabelWidth: 38, LabelHeight: 25,
		Columns: 1, Rows: 1, Fields: []string{FieldName, FieldPrice, FieldBarcode, FieldBarcodeText}, Builtin: true},
}

func defaultFields() []string {
	return []string{FieldName, FieldVariant, FieldPrice, FieldBarcode, FieldBarcodeText, FieldSKU}
}

// MaxCustomLayouts is how many layouts a shop may add.
const MaxCustomLayouts = 20

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Validate checks a custom layout and returns field errors keyed by
// prefix+"."+field.
func (l *Layout) Validate(prefix string) map[string]string {
	f := map[string]string{}
	add := func(k, msg string) { f[prefix+"."+k] = msg }
	l.ID = strings.ToLower(strings.TrimSpace(l.ID))
	l.Name = strings.TrimSpace(l.Name)
	l.Builtin = false
	if !idPattern.MatchString(l.ID) {
		add("id", "id: lower-case letters, digits and -, at most 40")
	}
	for _, p := range Presets {
		if p.ID == l.ID {
			add("id", "id "+l.ID+" is a built-in layout")
		}
	}
	if l.Name == "" || len([]rune(l.Name)) > 60 {
		add("name", "Name is required (at most 60 characters).")
	}
	before := len(f)
	between := func(k string, v, lo, hi float64) {
		if math.IsNaN(v) || v < lo || v > hi {
			add(k, fmt.Sprintf("%s must be between %g and %g mm", k, lo, hi))
		}
	}
	between("pageWidth", l.PageWidth, 20, 500)
	between("pageHeight", l.PageHeight, 15, 1000)
	between("labelWidth", l.LabelWidth, 20, 300)
	between("labelHeight", l.LabelHeight, 12, 300)
	between("marginTop", l.MarginTop, 0, 100)
	between("marginLeft", l.MarginLeft, 0, 100)
	between("gapX", l.GapX, 0, 50)
	between("gapY", l.GapY, 0, 50)
	if l.Columns < 1 || l.Columns > 10 {
		add("columns", "columns must be between 1 and 10")
	}
	if l.Rows < 1 || l.Rows > 40 {
		add("rows", "rows must be between 1 and 40")
	}
	if len(f) == before { // the sizes are valid: do they fit the page?
		if w := l.MarginLeft + float64(l.Columns)*l.LabelWidth + float64(l.Columns-1)*l.GapX; w > l.PageWidth+0.01 {
			add("columns", fmt.Sprintf("the labels need %.1f mm across but the page is %.1f mm wide", w, l.PageWidth))
		}
		if h := l.MarginTop + float64(l.Rows)*l.LabelHeight + float64(l.Rows-1)*l.GapY; h > l.PageHeight+0.01 {
			add("rows", fmt.Sprintf("the labels need %.1f mm down but the page is %.1f mm high", h, l.PageHeight))
		}
	}
	seen := map[string]bool{}
	var fields []string
	for _, x := range l.Fields {
		ok := false
		for _, a := range AllFields {
			ok = ok || a == x
		}
		if !ok {
			add("fields", "unknown field "+x)
			continue
		}
		if !seen[x] {
			seen[x] = true
			fields = append(fields, x)
		}
	}
	if len(fields) == 0 {
		add("fields", "choose at least one field")
	}
	l.Fields = fields
	return f
}

// Has reports whether the layout prints a field.
func (l Layout) Has(field string) bool {
	for _, f := range l.Fields {
		if f == field {
			return true
		}
	}
	return false
}

// PerPage is the number of labels on one page.
func (l Layout) PerPage() int { return l.Columns * l.Rows }
