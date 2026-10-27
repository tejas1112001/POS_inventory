// Package xlsx writes a single-sheet Excel workbook (Office Open XML
// SpreadsheetML) with the standard library: inline strings, numbers, a
// bold shaded header style and a currency number format. It replaces
// excelize for exports (writing only; excelize v2.11.0 had reachable
// advisories GO-2026-6684 and GO-2026-6688 with no tagged fix).
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

// Writer streams rows into the workbook's only sheet.
type Writer struct {
	zw   *zip.Writer
	bw   *bufio.Writer
	rows int
}

const (
	nsMain = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	nsRel  = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsPkg  = "http://schemas.openxmlformats.org/package/2006/relationships"
)

var fixedParts = []struct{ name, body string }{
	{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
		`<Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` +
		`</Types>`},
	{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="` + nsPkg + `">` +
		`<Relationship Id="rId1" Type="` + nsRel + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
	{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="` + nsPkg + `">` +
		`<Relationship Id="rId1" Type="` + nsRel + `/worksheet" Target="worksheets/sheet1.xml"/>` +
		`<Relationship Id="rId2" Type="` + nsRel + `/styles" Target="styles.xml"/></Relationships>`},
}

// New starts a workbook with one sheet. moneyFormat is an Excel number
// format such as "#,##0.00"; widths are column widths in characters.
func New(w io.Writer, sheet, moneyFormat string, widths []float64) (*Writer, error) {
	zw := zip.NewWriter(w)
	put := func(name, body string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	for _, p := range fixedParts {
		if err := put(p.name, p.body); err != nil {
			return nil, err
		}
	}
	if err := put("xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<workbook xmlns="`+nsMain+`" xmlns:r="`+nsRel+`"><sheets><sheet name="`+escape(sheet)+`" sheetId="1" r:id="rId1"/></sheets></workbook>`); err != nil {
		return nil, err
	}
	if err := put("xl/styles.xml", styles(moneyFormat)); err != nil {
		return nil, err
	}
	f, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriter(f)
	_, _ = bw.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><worksheet xmlns="` + nsMain + `">`)
	if len(widths) > 0 {
		_, _ = bw.WriteString("<cols>")
		for i, wd := range widths {
			fmt.Fprintf(bw, `<col min="%d" max="%d" width="%s" customWidth="1"/>`, i+1, i+1, strconv.FormatFloat(wd, 'f', -1, 64))
		}
		_, _ = bw.WriteString("</cols>")
	}
	_, _ = bw.WriteString("<sheetData>")
	return &Writer{zw: zw, bw: bw}, nil
}

func styles(moneyFormat string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="` + nsMain + `">` +
		`<numFmts count="1"><numFmt numFmtId="164" formatCode="` + escape(moneyFormat) + `"/></numFmts>` +
		`<fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts>` +
		`<fills count="3"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill>` +
		`<fill><patternFill patternType="solid"><fgColor rgb="FFF0F0F0"/><bgColor indexed="64"/></patternFill></fill></fills>` +
		`<borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
		`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
		`<cellXfs count="5">` +
		`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>` +
		`<xf numFmtId="0" fontId="1" fillId="2" borderId="0" xfId="0" applyFont="1" applyFill="1"/>` +
		`<xf numFmtId="164" fontId="0" fillId="0" borderId="0" xfId="0" applyNumberFormat="1"/>` +
		`<xf numFmtId="164" fontId="1" fillId="0" borderId="0" xfId="0" applyNumberFormat="1" applyFont="1"/>` +
		`<xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` +
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

// Row appends one row.
func (w *Writer) Row(cells ...Cell) error {
	if w.zw == nil {
		return errors.New("xlsx: writer closed")
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

// Close finishes the sheet and the zip archive.
func (w *Writer) Close() error {
	if w.zw == nil {
		return nil
	}
	if _, err := w.bw.WriteString("</sheetData></worksheet>"); err != nil {
		return err
	}
	if err := w.bw.Flush(); err != nil {
		return err
	}
	err := w.zw.Close()
	w.zw = nil
	return err
}

// Read returns the text of every cell of the first sheet of a workbook
// written by this package, row by row (numbers as written). It is meant
// for tests and checks, not for untrusted files.
func Read(data []byte) ([][]string, error) {
	zr, err := zip.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return nil, err
	}
	f, err := zr.Open("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var ws struct {
		Rows []struct {
			Cells []struct {
				Ref    string `xml:"r,attr"`
				Value  string `xml:"v"`
				Inline string `xml:"is>t"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.NewDecoder(io.LimitReader(f, 64<<20)).Decode(&ws); err != nil {
		return nil, err
	}
	out := make([][]string, 0, len(ws.Rows))
	for _, r := range ws.Rows {
		var row []string
		for _, c := range r.Cells {
			col := 0
			for _, ch := range c.Ref {
				if ch < 'A' || ch > 'Z' {
					break
				}
				col = col*26 + int(ch-'A'+1)
			}
			for len(row) < col-1 {
				row = append(row, "")
			}
			if c.Inline != "" {
				row = append(row, c.Inline)
			} else {
				row = append(row, c.Value)
			}
		}
		out = append(out, row)
	}
	return out, nil
}
