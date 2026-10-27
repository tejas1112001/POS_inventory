package receipts

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestFormatter(t *testing.T) {
	kol, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	d := decimal.RequireFromString
	in := NewFormatter("en-IN", "INR", kol)
	require.Equal(t, "₹12,34,567.50", in.Money(d("1234567.5")), "Indian grouping")
	require.Equal(t, "₹0.00", in.Money(decimal.Zero))
	require.Equal(t, "-₹5.00", in.Money(d("-5")))
	require.Equal(t, "18%", in.Percent(d("18.000")))
	require.Equal(t, "2.5%", in.Percent(d("2.5")))
	ts := time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)
	require.Equal(t, "20 Oct 2026, 23:30", in.DateTime(ts), "tenant time zone")

	us := NewFormatter("en-US", "USD", time.UTC)
	require.Equal(t, "$1,234,567.50", us.Money(d("1234567.5")))
	require.Equal(t, "Oct 20, 2026, 6:00 PM", us.DateTime(ts))

	require.Equal(t, "¥1,235", NewFormatter("en-GB", "JPY", nil).Money(d("1234.5")), "no minor unit")
	require.Equal(t, "KWD 1,234.500", NewFormatter("en-US", "KWD", nil).Money(d("1234.5")))
	require.Equal(t, "€1.234,50", NewFormatter("en-DE", "EUR", nil).Money(d("1234.5")), "separators come from the locale")
	require.Equal(t, "₹1.00", NewFormatter("xx", "???", nil).Money(d("1")), "fallback en-IN/INR")
}

func sampleView() View {
	return View{
		ReceiptID: 7, SaleID: 3, Version: 2, Title: "Tax Invoice",
		BusinessName: "Asha General Stores", LegalName: "Asha Retail Private Limited",
		Address: []string{"12 MG Road", "Pune 411001"}, Phone: "+91 98765 43210", Email: "hello@asha.test",
		TaxID: "GSTIN: 27ABCDE1234F1Z5", StoreName: "Main Store", StoreAddress: "12 MG Road", StorePhone: "020 1234",
		Reference: "POS-INV-000042", Date: "20 Oct 2026, 23:30", Cashier: "Ravi", Customer: "Kiran Patil",
		CustomerEmail: "kiran@example.test",
		Lines: []Line{
			{Name: "Masala Chai 250g", Attributes: "Pack: 250g", SKU: "TEA-250", Qty: 2, UnitPrice: "₹120.50", TaxRate: "5%", TaxAmount: "₹12.05", Total: "₹253.05"},
			{Name: "Cotton Shirt", Attributes: "Color: Blue, Size: M", SKU: "SHIRT-M", Qty: 1, UnitPrice: "₹1,499.00", Discount: "₹100.00", HasDiscount: true, TaxRate: "12%", TaxAmount: "₹167.88", Total: "₹1,566.88"},
		},
		TaxRows:  []TaxRow{{Label: "CGST 2.5%", Taxable: "₹241.00", Amount: "₹6.03"}, {Label: "SGST 2.5%", Taxable: "₹241.00", Amount: "₹6.02"}},
		Subtotal: "₹1,740.00", Discount: "₹100.00", HasDiscount: true, Tax: "₹179.93", Shipping: "₹40.00", HasShipping: true,
		Total: "₹1,859.93", Paid: "₹1,859.93", Tendered: "₹2,000.00", Change: "₹140.07", HasTendered: true,
		PaymentMethod: "cash", PaymentStatus: "Paid", Header: []string{"Welcome!"}, Footer: []string{"No refunds after 7 days"},
	}
}

func logoPNG(t *testing.T) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for x := 0; x < 64; x++ {
		for y := 0; y < 64; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: 120, B: 200, A: 255})
		}
	}
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

func TestRenderPDF(t *testing.T) {
	v := sampleView()
	pdf, err := RenderPDF(v, logoPNG(t))
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(pdf, []byte("%PDF-")))
	require.Less(t, len(pdf), 2<<20, "fits the 2 MiB attachment cap")
	if dir := os.Getenv("RECEIPT_SAMPLE_DIR"); dir != "" { // visual check: RECEIPT_SAMPLE_DIR=/tmp/x go test -run TestRenderPDF
		require.NoError(t, os.WriteFile(dir+"/receipt.pdf", pdf, 0o600))
		v.Void, v.VoidedAt, v.VoidReason = true, "21 Oct 2026, 10:00", "wrong item"
		vp, err := RenderPDF(v, nil)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(dir+"/void.pdf", vp, 0o600))
	}

	// Hostile and unusual data renders without failing.
	h := sampleView()
	h.BusinessName = "<script>alert(1)</script> {{.Secret}} \r\nBcc: x"
	h.Customer = strings.Repeat("Very long customer name ", 20)
	h.Lines = append(h.Lines, Line{Name: "क्षितिज ज्ञानेश्वर", Qty: 1, UnitPrice: "₹1.00", TaxRate: "0%", Total: "₹1.00"})
	for i := 0; i < 80; i++ {
		h.Lines = append(h.Lines, Line{Name: "Item", Qty: 1, UnitPrice: "₹1.00", TaxRate: "0%", Total: "₹1.00"})
	}
	var jb bytes.Buffer
	require.NoError(t, jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil))
	pdf, err = RenderPDF(h, jb.Bytes())
	require.NoError(t, err, "multi-page, JPEG logo")
	require.True(t, bytes.HasPrefix(pdf, []byte("%PDF-")))
	_, err = RenderPDF(h, []byte("not an image"))
	require.NoError(t, err, "an unreadable logo is skipped")
}
