package products

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/attributes"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/xlsx"
)

func clothingDefs() attributes.Defs {
	return attributes.Defs{
		"department": {Key: "department", Label: "Department", Level: "product", Kind: "choice", Choices: []string{"Men", "Women"}, Sort: 10},
		"season":     {Key: "season", Label: "Season", Level: "product", Kind: "choice", Choices: []string{"Spring/Summer"}, Sort: 30},
		"size":       {Key: "size", Label: "Size", Level: "variant", Kind: "choice", Choices: []string{"S", "M", "L"}, Sort: 50},
		"colour":     {Key: "colour", Label: "Colour", Level: "variant", Kind: "text", Choices: []string{}, Sort: 60},
	}
}

// The same rows read the same from CSV and from an .xlsx workbook.
func TestReadTableCSVAndXLSX(t *testing.T) {
	rows := [][]string{
		{"article_no", "name", "size", "colour", "sku", "price"},
		{"TSH-01", "Crew tee", "M", "Blue", "0902-M", "499"},
		{},
		{"TSH-01", "Crew tee", "L", "Blue", "0902-L", "499.5"},
	}
	var csvBuf strings.Builder
	for _, r := range rows {
		csvBuf.WriteString(strings.Join(r, ",") + "\n")
	}
	var x bytes.Buffer
	w, err := xlsx.New(&x, "Products", "#,##0.00", nil)
	require.NoError(t, err)
	for _, r := range rows {
		cells := []xlsx.Cell{}
		for _, v := range r {
			cells = append(cells, xlsx.Text(v, xlsx.Code))
		}
		require.NoError(t, w.Row(cells...))
	}
	require.NoError(t, w.Close())

	known := map[string]bool{"article_no": true, "name": true, "size": true, "colour": true, "sku": true, "price": true}
	for name, in := range map[string]struct {
		raw     []byte
		xlsx    bool
		lastRow string
	}{
		// CSV rows count records (blank lines are skipped, as before); a
		// workbook keeps the sheet's row numbers.
		"csv":  {[]byte("\xef\xbb\xbf" + csvBuf.String()), false, "3"},
		"xlsx": {x.Bytes(), true, "4"},
	} {
		recs, errs := readTable(in.raw, in.xlsx)
		require.Empty(t, errs, name)
		header, parsed, perrs := parseRows(recs, known)
		require.Empty(t, perrs, name)
		require.Equal(t, rows[0], header, name)
		require.Len(t, parsed, 2, name+": the empty row is skipped")
		require.Equal(t, in.lastRow, parsed[1]["_line"], name)
		require.Equal(t, "0902-L", parsed[1]["sku"], name)
	}

	_, errs := readTable([]byte("PK\x03\x04 not really a zip"), true)
	require.Contains(t, errs[0].Message, "not a readable Excel workbook")
	_, _, perrs := parseRows([][]string{{"name", "sku", "price", "colour"}}, map[string]bool{"name": true, "sku": true, "price": true})
	require.Equal(t, "colour", perrs[0].Field, "an attribute the shop does not define is an unknown column")
}

func TestScientificNotation(t *testing.T) {
	for v, sci := range map[string]bool{"8.90123E+12": true, "8.9e12": true, "1E+5": true, "8901234567890": false, "TSH-1E5": false, "1.5": false} {
		require.Equal(t, sci, sciNotation.MatchString(v), v)
	}
}

func groupOf(lines ...map[string]string) *group {
	g := &group{}
	for i, v := range lines {
		g.rows = append(g.rows, importRow{line: i + 2, vals: v})
	}
	g.article = g.rows[0].vals["article_no"]
	return g
}

func TestCheckGroup(t *testing.T) {
	defs := clothingDefs()
	cols := []string{"department", "season", "size", "colour"}
	var got []string
	add := func(row int, field, msg string) { got = append(got, fmt.Sprintf("%d %s: %s", row, field, msg)) }

	// Sizes and colours become dimensions; values are spelled as defined.
	g := groupOf(
		map[string]string{"article_no": "T1", "name": "Tee", "department": "men", "size": "m", "colour": "Blue"},
		map[string]string{"article_no": "T1", "name": "tee", "department": "", "size": "L", "colour": "Blue"},
	)
	checkGroup(g, defs, cols, add)
	require.Empty(t, got)
	require.Equal(t, []string{"size", "colour"}, g.checked.Dims)
	require.Equal(t, map[string]string{"department": "Men"}, g.checked.Attributes)
	require.Equal(t, "M", g.checked.Variants[0]["size"])

	// Product-level values must agree; options must differ; values must be
	// choices; a missing size on one row is reported on that row.
	got = nil
	g = groupOf(
		map[string]string{"article_no": "T2", "name": "Tee", "department": "Men", "size": "M", "colour": "Blue"},
		map[string]string{"article_no": "T2", "name": "Polo", "department": "Women", "size": "m", "colour": "blue"},
		map[string]string{"article_no": "T2", "name": "Tee", "size": "XXL", "colour": "Red"},
		map[string]string{"article_no": "T2", "name": "Tee", "colour": "Green"},
	)
	checkGroup(g, defs, cols, add)
	joined := strings.Join(got, "\n")
	require.Contains(t, joined, "3 name: name differs from row 2")
	require.Contains(t, joined, "3 department: Department differs from row 2 (Men)")
	require.Contains(t, joined, "3 size/colour: the same size and colour as row 2")
	require.Contains(t, joined, "4 size: ")
	require.Contains(t, joined, "5 size: Size is required")

	// Without attribute columns nothing is typed (the general template).
	got = nil
	g = groupOf(map[string]string{"name": "Rice", "sku": "R1"}, map[string]string{"name": "Rice", "sku": "R2"})
	checkGroup(g, defs, nil, add)
	require.Empty(t, got)
	require.False(t, g.checked.Uses())
}

func TestTemplateXLSX(t *testing.T) {
	var buf bytes.Buffer
	cols := templateColumns["clothing"]
	require.NoError(t, writeTemplateXLSX(&buf, cols, templateExample["clothing"], clothingDefs()))
	rows, err := xlsx.Read(buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, cols, rows[0])
	require.Equal(t, "TSH-01", rows[1][0])
	require.Len(t, rows, 2, "header and one example row on the first sheet")
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	f, err := zr.Open("xl/workbook.xml")
	require.NoError(t, err)
	wb, _ := io.ReadAll(f)
	require.Contains(t, string(wb), `name="Help"`, "a Help sheet")
}
