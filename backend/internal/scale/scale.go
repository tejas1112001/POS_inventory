// Package scale reads the barcodes weighing scales print (REMAINING "scale
// barcodes", tenant migration 00044): an in-store EAN-13 whose first two
// digits (20-29) name a rule, followed by the item's scale code (PLU),
// the weight or price, and the EAN check digit. Each shop sets its rules,
// as its scales are set up.
package scale

import (
	"fmt"
	"strconv"

	"github.com/shopspring/decimal"
)

// Values a rule's number can be.
const (
	ValueWeight = "weight" // the quantity, in base units after decimals (grams with decimals 3: kg)
	ValuePrice  = "price"  // the line's price, in currency units after decimals (paise with decimals 2)
)

// Rule is one scale barcode layout: Prefix (2 digits, 20-29), ItemDigits
// digits of scale code, then 10-ItemDigits digits of value with Decimals
// decimals, then the check digit.
type Rule struct {
	Prefix     string `json:"prefix"`
	ItemDigits int    `json:"itemDigits"`
	Value      string `json:"value"`
	Decimals   int    `json:"decimals"`
}

// MaxRules bounds a shop's rules.
const MaxRules = 10

// Check returns "" for a valid rule, else what is wrong with it.
func (r Rule) Check() string {
	switch {
	case len(r.Prefix) != 2 || r.Prefix[0] != '2' || r.Prefix[1] < '0' || r.Prefix[1] > '9':
		return "The prefix is two digits from 20 to 29 (in-store codes)."
	case r.ItemDigits < 4 || r.ItemDigits > 6:
		return "The scale code has 4 to 6 digits."
	case r.Value != ValueWeight && r.Value != ValuePrice:
		return "The value is the weight or the price."
	case r.Decimals < 0 || r.Decimals > 3:
		return "Decimals are 0 to 3."
	}
	return ""
}

// CheckRules validates a shop's rules: each valid, prefixes not repeated.
// Problems are keyed scaleBarcodes[i].
func CheckRules(rules []Rule) map[string]string {
	f := map[string]string{}
	if len(rules) > MaxRules {
		f["scaleBarcodes"] = fmt.Sprintf("At most %d scale barcode rules.", MaxRules)
	}
	seen := map[string]int{}
	for i, r := range rules {
		k := fmt.Sprintf("scaleBarcodes[%d]", i)
		if msg := r.Check(); msg != "" {
			f[k] = msg
			continue
		}
		if j, dup := seen[r.Prefix]; dup {
			f[k] = fmt.Sprintf("Prefix %s is already rule %d.", r.Prefix, j+1)
		}
		seen[r.Prefix] = i
	}
	return f
}

// exponents are the decimal exponents of 0-3 decimals.
var exponents = [4]int32{0, -1, -2, -3}

// CheckDigit is the EAN-13 check digit of 12 digits.
func CheckDigit(first12 string) byte {
	sum := 0
	for i := 0; i < 12; i++ {
		d := int(first12[i] - '0')
		if i%2 == 1 {
			d *= 3
		}
		sum += d
	}
	return byte('0' + (10-sum%10)%10)
}

// Read is a scanned scale barcode.
type Read struct {
	Rule  Rule
	Item  string          // the scale code
	Value decimal.Decimal // the weight (base units) or price, decimals applied
}

// Parse reads code with the shop's rules. ok is false for anything that
// is not a valid EAN-13 of one of the rules.
func Parse(code string, rules []Rule) (Read, bool) {
	if len(code) != 13 {
		return Read{}, false
	}
	for i := 0; i < 13; i++ {
		if code[i] < '0' || code[i] > '9' {
			return Read{}, false
		}
	}
	if CheckDigit(code[:12]) != code[12] {
		return Read{}, false
	}
	for _, r := range rules {
		if r.Check() != "" || code[:2] != r.Prefix {
			continue
		}
		end := 2 + r.ItemDigits
		n, err := strconv.ParseInt(code[end:12], 10, 64)
		if err != nil {
			return Read{}, false
		}
		// Decimals is 0-3 (Check): the exponent from a table, no conversion.
		return Read{Rule: r, Item: code[2:end], Value: decimal.New(n, exponents[r.Decimals])}, true
	}
	return Read{}, false
}
