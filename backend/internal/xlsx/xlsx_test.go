package xlsx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestColumn(t *testing.T) {
	for i, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA"} {
		require.Equal(t, want, column(i))
	}
}

func TestWriteAndRead(t *testing.T) {
	var buf bytes.Buffer
	w, err := New(&buf, "Report", "#,##0.00", []float64{12, 30})
	require.NoError(t, err)
	require.NoError(t, w.Row(Text("Name", Header), Text("Amount", Header)))
	require.NoError(t, w.Row(Text("Tea <&> \"quoted\"", Plain), Num(84.5, Money)))
	require.NoError(t, w.Row(Text("bad\x00\x07chars\tok", Plain), Cell{}, Num(-1.25, Money)))
	require.NoError(t, w.Row(Text("Total", Bold), Num(83.25, BoldMoney)))
	require.NoError(t, w.Close())
	require.NoError(t, w.Close(), "closing twice is harmless")
	require.Error(t, w.Row(Text("late", Plain)))

	rows, err := Read(buf.Bytes())
	require.NoError(t, err)
	require.Equal(t, [][]string{
		{"Name", "Amount"},
		{"Tea <&> \"quoted\"", "84.5"},
		{"badchars\tok", "", "-1.25"},
		{"Total", "83.25"},
	}, rows)

	// Every part is well-formed XML and the package lists them.
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		rc, err := f.Open()
		require.NoError(t, err)
		d := xml.NewDecoder(rc)
		for {
			_, err := d.Token()
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err, f.Name)
		}
		_ = rc.Close()
	}
	for _, n := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/styles.xml", "xl/worksheets/sheet1.xml"} {
		require.True(t, names[n], n)
	}
	styles, err := zr.Open("xl/styles.xml")
	require.NoError(t, err)
	b, _ := io.ReadAll(styles)
	require.True(t, strings.Contains(string(b), `formatCode="#,##0.00"`))
}
