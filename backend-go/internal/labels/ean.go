// Package labels implements in-store barcodes and price labels (industry
// Phase 1 item 5, docs/INDUSTRY_PHASE1_DESIGN.md D1, DB9-DB10, A9-A11,
// P3): internal EAN-13 codes for variants without a barcode, label layouts
// and the label PDF.
package labels

import (
	"fmt"
	"strings"
)

// InStorePrefix is GS1's restricted-circulation prefix: codes starting
// with 2 are never assigned to manufacturers, so an internal code cannot
// clash with a product's printed EAN.
const InStorePrefix = "2"

// CheckDigit returns the EAN-13 check digit of 12 digits.
func CheckDigit(d12 string) (byte, error) {
	if len(d12) != 12 {
		return 0, fmt.Errorf("labels: need 12 digits, got %d", len(d12))
	}
	sum := 0
	for i := 0; i < 12; i++ {
		c := d12[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("labels: %q is not a digit", c)
		}
		n := int(c - '0')
		if i%2 == 1 {
			n *= 3
		}
		sum += n
	}
	return byte('0' + (10-sum%10)%10), nil
}

// IsEAN13 reports whether s is 13 digits with a correct check digit.
func IsEAN13(s string) bool {
	if len(s) != 13 {
		return false
	}
	c, err := CheckDigit(s[:12])
	return err == nil && c == s[12]
}

// InternalCode is the EAN-13 for counter value n: "2" + n as 11 digits +
// the check digit (n from 1 to 99,999,999,999).
func InternalCode(n int64) (string, error) {
	if n < 1 || n > 99_999_999_999 {
		return "", fmt.Errorf("labels: counter %d out of range", n)
	}
	d12 := InStorePrefix + fmt.Sprintf("%011d", n)
	c, err := CheckDigit(d12)
	if err != nil {
		return "", err
	}
	return d12 + string(c), nil
}

// Symbology is the barcode type a code prints as: EAN-13 when it is a
// valid EAN-13, Code 128 otherwise (SKUs, other codes).
func Symbology(code string) string {
	if IsEAN13(strings.TrimSpace(code)) {
		return "ean13"
	}
	return "code128"
}
