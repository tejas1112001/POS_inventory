package reports

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"github.com/johnfercher/maroto/v2"
	"github.com/johnfercher/maroto/v2/pkg/components/text"
	"github.com/johnfercher/maroto/v2/pkg/config"
	"github.com/johnfercher/maroto/v2/pkg/consts/align"
	"github.com/johnfercher/maroto/v2/pkg/consts/fontstyle"
	"github.com/johnfercher/maroto/v2/pkg/consts/orientation"
	"github.com/johnfercher/maroto/v2/pkg/consts/pagesize"
	"github.com/johnfercher/maroto/v2/pkg/core"
	"github.com/johnfercher/maroto/v2/pkg/props"
	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/money"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/pdffonts"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/xlsx"
)

// Column kinds.
const (
	kindText = iota
	kindInt
	kindMoney
	kindDate
)

// column describes one exported column. width is in PDF grid units (of
// pdfGrid); total marks columns summed in the totals row.
type column struct {
	header string
	kind   int
	width  int
	xlsx   float64
	total  bool
}

// table is an export: rows hold string, int, decimal.Decimal or time.Time
// values matching the columns.
type table struct {
	title    string
	business string
	subtitle string
	columns  []column
	rows     [][]any
}

const pdfGrid = 24

// SafeText keeps a text cell from being read as a formula (CSV/formula
// injection, API_CONTRACT §5.1.1): text starting with =, +, -, @, a tab
// or a carriage return gets a leading apostrophe.
func SafeText(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func (t table) totals() []any {
	out := make([]any, len(t.columns))
	for i, c := range t.columns {
		if !c.total {
			continue
		}
		switch c.kind {
		case kindInt:
			n := 0
			for _, r := range t.rows {
				n += r[i].(int)
			}
			out[i] = n
		case kindMoney:
			sum := decimal.Zero
			for _, r := range t.rows {
				sum = sum.Add(r[i].(decimal.Decimal))
			}
			out[i] = sum
		}
	}
	out[0] = "Total"
	return out
}

// xlsxMoneyFormat is a number format with the currency's minor digits.
func xlsxMoneyFormat(currency string) string {
	if exp := money.Exponent(currency); exp > 0 {
		return "#,##0." + strings.Repeat("0", int(exp))
	}
	return "#,##0"
}

// writeXLSX streams the table into one sheet: a bold header, numbers as
// numbers, dates as sortable text in the tenant's time zone, text made
// formula-safe, and a totals row.
func writeXLSX(t table, currency string, loc *time.Location) ([]byte, error) {
	widths := make([]float64, len(t.columns))
	for i, c := range t.columns {
		widths[i] = c.xlsx
	}
	var buf bytes.Buffer
	w, err := xlsx.New(&buf, "Report", xlsxMoneyFormat(currency), widths)
	if err != nil {
		return nil, err
	}
	header := make([]xlsx.Cell, len(t.columns))
	for i, c := range t.columns {
		header[i] = xlsx.Text(c.header, xlsx.Header)
	}
	if err := w.Row(header...); err != nil {
		return nil, err
	}
	cell := func(v any, bold bool) xlsx.Cell {
		text, money := xlsx.Plain, xlsx.Money
		if bold {
			text, money = xlsx.Bold, xlsx.BoldMoney
		}
		switch x := v.(type) {
		case string:
			return xlsx.Text(SafeText(x), text)
		case int:
			return xlsx.Num(float64(x), text)
		case decimal.Decimal:
			return xlsx.Num(x.InexactFloat64(), money)
		case time.Time:
			return xlsx.Text(x.In(loc).Format("2006-01-02 15:04"), text)
		}
		return xlsx.Cell{}
	}
	for _, row := range t.rows {
		cells := make([]xlsx.Cell, len(row))
		for i, v := range row {
			cells[i] = cell(v, false)
		}
		if err := w.Row(cells...); err != nil {
			return nil, err
		}
	}
	totals := t.totals()
	cells := make([]xlsx.Cell, len(totals))
	for i, v := range totals {
		cells[i] = cell(v, true)
	}
	if err := w.Row(cells...); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var (
	grey  = &props.Color{Red: 110, Green: 110, Blue: 110}
	shade = &props.Cell{BackgroundColor: &props.Color{Red: 240, Green: 240, Blue: 240}}
)

// writePDF renders the table on landscape A4 pages with a repeated
// header row and page numbers. Text is plain data (no markup).
func writePDF(t table, f receipts.Formatter) ([]byte, error) {
	fonts, err := pdffonts.Load()
	if err != nil {
		return nil, err
	}
	cfg := config.NewBuilder().
		WithPageSize(pagesize.A4).WithOrientation(orientation.Horizontal).WithMaxGridSize(pdfGrid).
		WithLeftMargin(10).WithRightMargin(10).WithTopMargin(10).
		WithCustomFonts(fonts).
		WithDefaultFont(&props.Font{Family: pdffonts.Family, Size: 7.5}).
		WithPageNumber(props.PageNumber{Pattern: "Page {current} of {total}", Place: props.RightBottom,
			Family: pdffonts.Family, Size: 7, Color: grey}).
		WithTitle(t.title, true).
		WithCreator(t.business, true).
		Build()
	m := maroto.New(cfg)
	m.AddAutoRow(text.NewCol(pdfGrid, t.business, props.Text{Size: 13, Style: fontstyle.Bold}))
	m.AddAutoRow(text.NewCol(pdfGrid, t.title, props.Text{Size: 11, Style: fontstyle.Bold, Top: 1}))
	m.AddAutoRow(text.NewCol(pdfGrid, t.subtitle, props.Text{Size: 8, Color: grey, Top: 1, Bottom: 2}))

	cellText := func(c column, v any) string {
		switch x := v.(type) {
		case nil:
			return ""
		case string:
			return x
		case int:
			return strconv.Itoa(x)
		case decimal.Decimal:
			return f.Money(x)
		case time.Time:
			return f.DateTime(x)
		}
		return ""
	}
	row := func(values []any, bold bool) []core.Col {
		cols := make([]core.Col, len(t.columns))
		for i, c := range t.columns {
			p := props.Text{Size: 7.5, Top: 0.8, Bottom: 0.8}
			if c.kind == kindInt || c.kind == kindMoney {
				p.Align = align.Right
			}
			if bold {
				p.Style = fontstyle.Bold
			}
			cols[i] = text.NewCol(c.width, cellText(c, values[i]), p)
		}
		return cols
	}
	headers := make([]any, len(t.columns))
	for i, c := range t.columns {
		headers[i] = c.header
	}
	m.AddAutoRow(row(headers, true)...).WithStyle(shade)
	for _, r := range t.rows {
		m.AddAutoRow(row(r, false)...)
	}
	m.AddAutoRow(row(t.totals(), true)...).WithStyle(shade)
	if len(t.rows) == 0 {
		m.AddAutoRow(text.NewCol(pdfGrid, "No rows match these filters.", props.Text{Size: 8, Color: grey, Top: 2}))
	}
	doc, err := m.Generate()
	if err != nil {
		return nil, err
	}
	return doc.GetBytes(), nil
}
