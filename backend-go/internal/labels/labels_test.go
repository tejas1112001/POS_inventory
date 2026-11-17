package labels

import (
	"bytes"
	"image"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckDigit(t *testing.T) {
	for code, ok := range map[string]bool{
		"4006381333931": true,  // a real EAN-13
		"8901030865275": true,  // an Indian EAN-13 (890 prefix)
		"4006381333932": false, // wrong check digit
		"400638133393":  false, // 12 digits
		"40063813339A1": false,
	} {
		require.Equal(t, ok, IsEAN13(code), code)
	}
	c, err := InternalCode(1)
	require.NoError(t, err)
	require.Equal(t, "2000000000015", c)
	require.True(t, IsEAN13(c))
	c, err = InternalCode(99_999_999_999)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(c, "299999999999"))
	require.True(t, IsEAN13(c))
	_, err = InternalCode(0)
	require.Error(t, err)
	require.Equal(t, "ean13", Symbology("2000000000015"))
	require.Equal(t, "code128", Symbology("TSH-01-M-BLUE"))
	require.Equal(t, "code128", Symbology("2000000000016"), "a bad check digit prints as Code 128")
}

// EAN-13 digit patterns (L codes; G is R reversed; R is L inverted).
var lCodes = []string{"0001101", "0011001", "0010011", "0111101", "0100011", "0110001", "0101111", "0111011", "0110111", "0001011"}
var parity = []string{"LLLLLL", "LLGLGG", "LLGGLG", "LLGGGL", "LGLLGG", "LGGLLG", "LGGGLL", "LGLGLG", "LGLGGL", "LGGLGL"}

func invert(s string) string {
	b := []byte(s)
	for i := range b {
		b[i] = '0' + '1' - b[i]
	}
	return string(b)
}

func reverse(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// decodeEAN13 reads the middle pixel row of a rendered barcode: 95 modules
// of equal width, dark = 1. It is a minimal reader, enough to prove the
// printed bars carry the code.
func decodeEAN13(t *testing.T, img image.Image) string {
	t.Helper()
	b := img.Bounds()
	y := b.Min.Y + b.Dy()/2
	w := float64(b.Dx()) / 95
	bits := make([]byte, 95)
	for m := 0; m < 95; m++ {
		x := b.Min.X + int((float64(m)+0.5)*w)
		r, _, _, _ := img.At(x, y).RGBA()
		bits[m] = '0'
		if r < 0x8000 {
			bits[m] = '1'
		}
	}
	s := string(bits)
	require.Equal(t, "101", s[:3], "start guard")
	require.Equal(t, "01010", s[45:50], "middle guard")
	require.Equal(t, "101", s[92:], "end guard")
	var digits, pat strings.Builder
	for i := 0; i < 6; i++ {
		chunk := s[3+i*7 : 10+i*7]
		found := false
		for d, l := range lCodes {
			switch chunk {
			case l:
				digits.WriteByte(byte('0' + d))
				pat.WriteByte('L')
				found = true
			case reverse(invert(l)):
				digits.WriteByte(byte('0' + d))
				pat.WriteByte('G')
				found = true
			}
		}
		require.True(t, found, "left digit %d", i)
	}
	first := -1
	for d, p := range parity {
		if p == pat.String() {
			first = d
		}
	}
	require.GreaterOrEqual(t, first, 0, "parity pattern")
	for i := 0; i < 6; i++ {
		chunk := s[50+i*7 : 57+i*7]
		found := false
		for d, l := range lCodes {
			if chunk == invert(l) {
				digits.WriteByte(byte('0' + d))
				found = true
			}
		}
		require.True(t, found, "right digit %d", i)
	}
	return string(rune('0'+first)) + digits.String()
}

// The bars the label PDF embeds decode back to the code.
func TestEAN13Decodes(t *testing.T) {
	for _, n := range []int64{1, 42, 123456789, 99_999_999_999} {
		c, err := InternalCode(n)
		require.NoError(t, err)
		img, modules, err := BarcodeImage(c, 4, 40)
		require.NoError(t, err)
		require.Equal(t, 95, modules)
		require.Equal(t, c, decodeEAN13(t, img))
	}
	_, modules, err := BarcodeImage("TSH-01-M-BLUE", 4, 40)
	require.NoError(t, err)
	require.Greater(t, modules, 95, "Code 128")
}

func TestLayoutValidate(t *testing.T) {
	for _, p := range Presets {
		cp := p
		cp.ID = "copy-" + p.ID
		require.Empty(t, cp.Validate("x"), p.ID)
	}
	bad := Layout{ID: "a4-3x8", Name: "", PageWidth: 100, PageHeight: 100, LabelWidth: 60, LabelHeight: 30, Columns: 2, Rows: 1, Fields: []string{"name", "colourful"}}
	f := bad.Validate("layouts[0]")
	require.Contains(t, f, "layouts[0].id")
	require.Contains(t, f, "layouts[0].name")
	require.Contains(t, f, "layouts[0].fields")
	wide := Layout{ID: "wide", Name: "Wide", PageWidth: 100, PageHeight: 100, LabelWidth: 60, LabelHeight: 30, Columns: 2, Rows: 1, Fields: []string{"name"}}
	require.Contains(t, wide.Validate("l"), "l.columns", "two 60 mm labels do not fit 100 mm")
}

func pages(pdf []byte) int {
	return bytes.Count(pdf, []byte("/Type /Page")) - bytes.Count(pdf, []byte("/Type /Pages"))
}

func TestRender(t *testing.T) {
	items := make([]Label, 30)
	for i := range items {
		items[i] = Label{Name: "Crew tee", Variant: "M / Blue", Price: "₹499.00", SKU: "TSH-01-M-BLUE", Code: "2000000000015"}
	}
	items[3].Code = "TSH-01-M-BLUE" // Code 128
	items[4].Name = "क्षितिज " + strings.Repeat("very long product name ", 5)
	a4, err := Render(Presets[0], items)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(a4, []byte("%PDF-")))
	require.Equal(t, 2, pages(a4), "30 labels on 24-label sheets")

	roll, err := Render(Presets[1], items[:5])
	require.NoError(t, err)
	require.Equal(t, 5, pages(roll), "one label per roll page")
	_, err = Render(Presets[2], items[:1])
	require.NoError(t, err)

	_, err = Render(Presets[0], nil)
	require.Error(t, err)
	_, err = Render(Presets[0], make([]Label, MaxLabels+1))
	require.Error(t, err)
}
