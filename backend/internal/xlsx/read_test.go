package xlsx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// excelLike builds a workbook the way Excel saves one: shared strings,
// the first sheet in a part that is not sheet1.xml, rich text, a boolean,
// a number and a gap row.
func excelLike(t *testing.T, sheetXML string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"xl/workbook.xml": `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
			`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>` +
			`<sheet name="Products" sheetId="3" r:id="rId7"/><sheet name="Help" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="x/worksheet" Target="worksheets/sheet1.xml"/>` +
			`<Relationship Id="rId7" Type="x/worksheet" Target="/xl/worksheets/sheet3.xml"/></Relationships>`,
		"xl/sharedStrings.xml": `<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="4" uniqueCount="4">` +
			`<si><t>name</t></si><si><t>sku</t></si>` +
			`<si><r><rPr><b/></rPr><t>Crew </t></r><r><t xml:space="preserve">tee</t></r><rPh><t>IGNORED</t></rPh></si>` +
			`<si><t>TSH-01</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
			`<sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>help sheet</t></is></c></row></sheetData></worksheet>`,
		"xl/worksheets/sheet3.xml": sheetXML,
	}
	for name, body := range parts {
		f, err := zw.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

const sheetHead = `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`

func TestReadExcelWorkbook(t *testing.T) {
	data := excelLike(t, sheetHead+
		`<row r="1" spans="1:4"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="inlineStr"><is><t>price</t></is></c><c r="D1" t="str"><f>x</f><v>flag</v></c></row>`+
		`<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" t="s"><v>3</v></c><c r="C2"><v>499.5</v></c><c r="D2" t="b"><v>1</v></c></row>`+
		`<row r="3"><c r="A3" s="2"/></row>`+ // styled but empty
		`<row r="5"><c r="C5"><v>8901234567890</v></c><c r="E5" t="e"><v>#N/A</v></c></row>`+
		`</sheetData></worksheet>`)
	rows, err := Read(data)
	require.NoError(t, err)
	require.Equal(t, [][]string{
		{"name", "sku", "price", "flag"},
		{"Crew tee", "TSH-01", "499.5", "TRUE"},
		nil,
		nil,
		{"", "", "8901234567890", "", "#N/A"},
	}, rows)

	_, err = ReadLimit(data, 4)
	require.ErrorIs(t, err, ErrTooManyRows, "a value in row 5")
	rows, err = ReadLimit(data, 5)
	require.NoError(t, err)
	require.Len(t, rows, 5)
}

func TestReadRefusesBadFiles(t *testing.T) {
	_, err := Read([]byte("name,sku\nx,y\n"))
	require.ErrorIs(t, err, ErrNotWorkbook)
	_, err = Read(excelLike(t, sheetHead+`<row><c r="A1"><v>1</v></c>`)) // truncated XML
	require.Error(t, err)
	_, err = Read(excelLike(t, sheetHead+`<row r="1"><c r="ZZZZ1"><v>1</v></c></row></sheetData></worksheet>`))
	require.Error(t, err, "a column past the limit")

	// A part that unzips past the cap is refused before it is read.
	defer func(n int64) { maxPartBytes = n }(maxPartBytes)
	maxPartBytes = 4 << 10
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("xl/worksheets/sheet1.xml")
	require.NoError(t, err)
	_, err = f.Write([]byte(sheetHead + strings.Repeat(" ", int(maxPartBytes)+1) + `</sheetData></worksheet>`))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	_, err = Read(buf.Bytes())
	require.Error(t, err)
}

func TestWriteSeveralSheets(t *testing.T) {
	var buf bytes.Buffer
	b, err := NewBook(&buf, "#,##0.00")
	require.NoError(t, err)
	require.Error(t, b.Row(Text("no sheet yet", Plain)))
	require.NoError(t, b.Sheet("Products", []Col{{Width: 14, Style: Code}, {}}))
	require.NoError(t, b.Row(Text("sku", Header), Text("price", Header)))
	require.NoError(t, b.Row(Text("0902", Code), Num(180, Plain)))
	require.NoError(t, b.Sheet("Help: [columns]", nil))
	require.NoError(t, b.Row(Text("help", Plain)))
	require.NoError(t, b.Close())

	rows, err := Read(buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, [][]string{{"sku", "price"}, {"0902", "180"}}, rows, "the first sheet")
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	var wb, sheet string
	for _, f := range zr.File {
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		_ = rc.Close()
		switch f.Name {
		case "xl/workbook.xml":
			wb = b.String()
		case "xl/worksheets/sheet1.xml":
			sheet = b.String()
		}
	}
	require.Contains(t, wb, `name="Help- -columns-"`, "sheet names drop characters Excel refuses")
	require.Contains(t, sheet, `style="5"`, "the code column is formatted as text")
}
