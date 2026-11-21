package catalog

import (
	"regexp"
	"strings"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/gst"
)

// Tax identity of stores, customers and suppliers (M3 step 1,
// docs/M3_GST_DESIGN.md DB2-DB4): a GSTIN and a GST state code. Each field
// is optional in a request: missing keeps the value, "" clears it.

// taxIn is the GSTIN and state of a request.
type taxIn struct {
	GSTIN     *string `json:"gstin"`
	StateCode *string `json:"stateCode"`
}

// taxOut is the GSTIN and state of a record (nil: not set).
type taxOut struct {
	GSTIN     *string `json:"gstin"`
	StateCode *string `json:"stateCode"`
}

func strOr(p *string, cur *string) string {
	if p != nil {
		return *p
	}
	if cur != nil {
		return *cur
	}
	return ""
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// resolveTax applies the request to the current values. A new GSTIN sets
// the state to its own when the request names no state; a GSTIN of
// another state is refused (M3 Q6). Clearing the state of a record that
// keeps its GSTIN is refused too.
func resolveTax(in taxIn, cur taxOut) (taxOut, error) {
	g := strOr(in.GSTIN, cur.GSTIN)
	s := strOr(in.StateCode, cur.StateCode)
	if in.GSTIN != nil && in.StateCode == nil && strings.TrimSpace(*in.GSTIN) != "" {
		s = "" // the new GSTIN's own state
	}
	g, s, f := gst.Party(g, s)
	if len(f) > 0 {
		return cur, apperr.Validation(f)
	}
	return taxOut{GSTIN: ptr(g), StateCode: ptr(s)}, nil
}

var invoiceCodePattern = regexp.MustCompile(`^[A-Z0-9]{2,5}$`)

// cleanInvoiceCode reads a store's invoice code (M3 Q8): 2-5 capital
// letters or digits, "" for none.
func cleanInvoiceCode(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s != "" && !invoiceCodePattern.MatchString(s) {
		return "", apperr.Invalid("invoiceCode", "Use 2 to 5 capital letters or digits, such as MUM.")
	}
	return s, nil
}
