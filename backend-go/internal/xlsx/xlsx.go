// Package xlsx writes Excel workbooks (Office Open XML SpreadsheetML) with
// the standard library: inline strings, numbers, a bold shaded header
// style, a currency number format and a text format for code columns; one
// or more sheets. It replaces excelize for exports (excelize v2.11.0 had
// reachable advisories GO-2026-6684 and GO-2026-6688 with no tagged fix).
// Read reads the first sheet of a workbook, including files saved by
// Excel, LibreOffice or Google Sheets (product import, industry P1 item 6).
package xlsx

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Style is a cell style.
type Style int

// Cell styles (indexes into cellXfs below).
const (
	Plain Style = iota
	Header
	Money
	BoldMoney
	Bold
	Code // text format ("@"): Excel keeps typed codes as text
)

// Cell is one value: text, or a number when IsNumber is set. An empty
// cell has neither.
type Cell struct {
	Text     string
	Number   float64
	IsNumber bool
	Style    Style
}

// Text is a text cell.
func Text(s string, st Style) Cell { return Cell{Text: s, Style: st} }

// Num is a number cell.
func Num(f float64, st Style) Cell { return Cell{Number: f, IsNumber: true, Style: st} }

// Col is a column's width (in characters; 0: default) and the style new
// cells typed into it take.
type Col struct {
	Width float64
	Style Style
}

// Writer streams rows into the workbook's sheets, one sheet at a time.
type Writer struct {
	zw     *zip.Writer
	bw     *bufio.Writer
	rows   int
	sheets []string
}

const (
	nsMain = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsRel  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkg  = "http://schemas.openxmlformats.org/package/2006/relationships"
)

func put(zw *zip.Writer, name, body string) error {
	f, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, body)
	return err
}

// NewBook starts a workbook; add sheets with Sheet. moneyFormat is an
// Excel number format such as "#,##0.00".
func NewBook(w io.Writer, moneyFormat string) (*Writer, error) {
	zw := zip.NewWriter(w)
	if err := put(zw, "xl/styles.xml", styles(moneyFormat)); err != nil {
		return nil, err
	}
	return &Writer{zw: zw}, nil
}

// New starts a workbook with one sheet; widths are column widths in
// characters.
func New(w io.Writer, sheet, moneyFormat string, widths []float64) (*Writer, error) {
	b, err := NewBook(w, moneyFormat)
	if err != nil {
		return nil, err
	}
	cols := make([]Col, len(widths))
	for i, wd := range widths {
		cols[i] = Col{Width: wd}
	}
	if err := b.Sheet(sheet, cols); err != nil {
		return nil, err
	}
	return b, nil
}

// Sheet finishes the current sheet (if any) and starts the next one.
func (w *Writer) Sheet(name string, cols []Col) error {
	if w.zw == nil {
		return errors.New("xlsx: writer closed")
	}
	if err := w.endSheet(); err != nil {
		return err
	}
	w.sheets = append(w.sheets, name)
	f, err := w.zw.Create(fmt.Sprintf("xl/worksheets/sheet%d.xml", len(w.sheets)))
	if err != nil {
		return err
	}
	w.bw, w.rows = bufio.NewWriter(f), 0
	_, _ = w.bw.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="` + nsMain + `">`)
	if len(cols) > 0 {
		_, _ = w.bw.WriteString("<cols>")
		for i, c := range cols {
			fmt.Fprintf(w.bw, `<col min="%d" max="%d"`, i+1, i+1)
			if c.Width > 0 {
				fmt.Fprintf(w.bw, ` width="%s" customWidth="1"`, strconv.FormatFloat(c.Width, 'f', -1, 64))
			} else {
				_, _ = w.bw.WriteString(` width="12"`)
			}
			if c.Style != Plain {
				fmt.Fprintf(w.bw, ` style="%d"`, c.Style)
			}
			_, _ = w.bw.WriteString("/>")
		}
		_, _ = w.bw.WriteString("</cols>")
	}
	_, err = w.bw.WriteString("<sheetData>")
	return err
}

func (w *Writer) endSheet() error {
	if w.bw == nil {
		return nil
	}
	if _, err := w.bw.WriteString("</sheetData></worksheet>"); err != nil {
		return err
	}
	err := w.bw.Flush()
	w.bw = nil
	return err
}

func styles(moneyFormat string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="` + nsMain + `">` +
		`<numFmts count="1"><numFmt numFmtId="164" formatCode="` + escape(moneyFormat) + `"/></numFmts>` +
		`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
		`<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>` +
		`<fill><patternFill patternType="solid"><fgColor rgb="FFF0F0F0"/><bgColor indexed="64"/></patternFill></fill></fills>` +
		`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
		`<cellXfs count="6">` +
		`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
		`<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/>` +
		`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
		`<xf numFmtId="164" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyFont="1"/>` +
		`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
		`<xf numFmtId="49" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
		`</cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`
}

// column returns the column letters for a 0-based index (A, B, ..., AA).
func column(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// clean drops characters XML 1.0 cannot carry.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return r
		case r < 0x20, r >= 0xD800 && r <= 0xDFFF, r == 0xFFFE, r == 0xFFFF:
			return -1
		}
		return r
	}, s)
}

func escape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(clean(s)))
	return b.String()
}

// Row appends one row to the current sheet.
func (w *Writer) Row(cells ...Cell) error {
	if w.zw == nil {
		return errors.New("xlsx: writer closed")
	}
	if w.bw == nil {
		return errors.New("xlsx: no sheet started")
	}
	w.rows++
	fmt.Fprintf(w.bw, `<row r="%d">`, w.rows)
	for i, c := range cells {
		ref := column(i) + strconv.Itoa(w.rows)
		switch {
		case c.IsNumber:
			fmt.Fprintf(w.bw, `<c r="%s" s="%d"><v>%s</v></c>`, ref, c.Style, strconv.FormatFloat(c.Number, 'f', -1, 64))
		case c.Text != "":
			fmt.Fprintf(w.bw, `<c r="%s" s="%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, c.Style, escape(c.Text))
		case c.Style != Plain:
			fmt.Fprintf(w.bw, `<c r="%s" s="%d"/>`, ref, c.Style)
		}
	}
	_, err := w.bw.WriteString("</row>")
	return err
}

// Close finishes the last sheet, writes the package parts that list the
// sheets and closes the zip archive.
func (w *Writer) Close() error {
	if w.zw == nil {
		return nil
	}
	if err := w.endSheet(); err != nil {
		return err
	}
	if len(w.sheets) == 0 {
		return errors.New("xlsx: a workbook needs a sheet")
	}
	var types, rels, sheets strings.Builder
	for i, name := range w.sheets {
		n := i + 1
		fmt.Fprintf(&types, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="%s/worksheet" Target="worksheets/sheet%d.xml"/>`, n, nsRel, n)
		fmt.Fprintf(&sheets, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, escape(sheetName(name)), n, n)
	}
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			types.String() +
			`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
			`</Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="` + nsPkg + `">` +
			`<Relationship Id="rId1" Type="` + nsRel + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Relationships xmlns="` + nsPkg + `">` + rels.String() +
			fmt.Sprintf(`<Relationship Id="rId%d" Type="%s/styles" Target="styles.xml"/>`, len(w.sheets)+1, nsRel) +
			`</Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<workbook xmlns="` + nsMain + `" xmlns:r="` + nsRel + `"><sheets>` + sheets.String() + `</sheets></workbook>`},
	}
	for _, p := range parts {
		if err := put(w.zw, p.name, p.body); err != nil {
			return err
		}
	}
	err := w.zw.Close()
	w.zw = nil
	return err
}

// sheetName fits Excel's rules: at most 31 characters, none of []:*?/\.
func sheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`[]:*?/\`, r) {
			return '-'
		}
		return r
	}, s)
	if r := []rune(s); len(r) > 31 {
		s = string(r[:31])
	}
	if s == "" {
		s = "Sheet"
	}
	return s
}
