package reports

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend/internal/receipts"
	"github.com/tejas1112001/POS_inventory/backend/internal/xlsx"
)

func TestSafeText(t *testing.T) {
	for in, want := range map[string]string{
		"=HYPERLINK(\"http://x\")": "'=HYPERLINK(\"http://x\")",
		"+1":                       "'+1",
		"-2":                       "'-2",
		"@SUM(A1)":                 "'@SUM(A1)",
		"\tcmd":                    "'\tcmd",
		"\rcmd":                    "'\rcmd",
		"Tea":                      "Tea",
		"":                         "",
		"a=b":                      "a=b",
	} {
		require.Equal(t, want, SafeText(in), in)
	}
}

func TestXLSXMoneyFormat(t *testing.T) {
	require.Equal(t, "#,##0.00", xlsxMoneyFormat("INR"))
	require.Equal(t, "#,##0", xlsxMoneyFormat("JPY"))
	require.Equal(t, "#,##0.000", xlsxMoneyFormat("KWD"))
}

func sample(n int) table {
	t := table{title: "Sales report", business: "Shop", subtitle: "All dates", columns: salesColumns}
	day := time.Date(2026, 10, 23, 18, 36, 0, 0, time.UTC)
	for i := range n {
		name := "Tea"
		if i == 0 {
			name = "=cmd|' /C calc'!A0"
		}
		soldAs := ""
		if i == 1 {
			soldAs = "2 Box"
		}
		t.rows = append(t.rows, []any{day, "POS-INV-000001", name, "SKU-1", "Walk-in Customer", "Main", 2, soldAs,
			decimal.RequireFromString("84.00"), decimal.RequireFromString("42.00"), decimal.RequireFromString("42.00")})
	}
	return t
}

func TestWriteXLSX(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	b, err := writeXLSX(sample(3), "INR", kolkata)
	require.NoError(t, err)
	rows, err := xlsx.Read(b)
	require.NoError(t, err)
	require.Len(t, rows, 5, "header, 3 rows, totals")
	require.Equal(t, []string{"Date", "Reference", "Product", "SKU", "Customer", "Store", "Qty", "Sold as", "Amount", "Returned", "Net"}, rows[0])
	require.Equal(t, "2026-10-24 00:06", rows[1][0], "the tenant's time zone")
	require.Equal(t, "'=cmd|' /C calc'!A0", rows[1][2], "formula-safe text")
	require.Equal(t, "84", rows[1][8], "amounts are numbers")
	require.Equal(t, "2 Box", rows[2][7], "the unit sold in (H3)")
	require.Equal(t, "Total", rows[4][0])
	require.Equal(t, "6", rows[4][6], "quantities total in base units")
	require.Equal(t, "", rows[4][7], "no total for the unit text")
	require.Equal(t, "252", rows[4][8])
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	require.NoError(t, err)
	sheet, err := zr.Open("xl/worksheets/sheet1.xml")
	require.NoError(t, err)
	xmlBody, err := io.ReadAll(sheet)
	require.NoError(t, err)
	require.NotContains(t, string(xmlBody), "<f>", "no formulas anywhere")
}

func TestWritePDF(t *testing.T) {
	f := receipts.NewFormatter("en-IN", "INR", time.UTC)
	one, err := writePDF(sample(1), f)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(one, []byte("%PDF-")))
	many, err := writePDF(sample(400), f)
	require.NoError(t, err)
	require.Greater(t, bytes.Count(many, []byte("/Type /Page\n")), 3, "spans several pages")
	empty, err := writePDF(sample(0), f)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(empty, []byte("%PDF-")))
}

// Every export's PDF columns fill the grid exactly.
func TestColumnWidths(t *testing.T) {
	for name, cols := range map[string][]column{"sales": salesColumns, "purchases": purchaseColumns} {
		w := 0
		for _, c := range cols {
			w += c.width
		}
		require.Equal(t, pdfGrid, w, name)
	}
}
