package sales

import (
	"context"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/tejas1112001/POS_inventory/backend/internal/apperr"
	"github.com/tejas1112001/POS_inventory/backend/internal/gst"
	"github.com/tejas1112001/POS_inventory/backend/internal/pricing"
	"github.com/tejas1112001/POS_inventory/backend/internal/tenancy"
)

// GST per sale (M3 step 2, docs/M3_GST_DESIGN.md §4). Each sale works out
// who sells (the store's GSTIN and state, else the shop's), who buys (the
// customer's GSTIN, name, address and state), where it is supplied (the
// store's state unless the cashier names another) and so how its tax is
// split. The sale keeps a copy of all of it.

// taxCtx is a sale's tax identity.
type taxCtx struct {
	sellerGSTIN, sellerState                        *string
	buyerGSTIN, buyerName, buyerAddress, buyerState *string
	place                                           *string // place of supply; nil while no state is known
	split                                           string  // pricing.Split*
	kind                                            string  // b2b (the buyer has a GSTIN) or b2c
}

// GSTJSON is a sale's or credit note's copy (null for documents made
// before M3).
type GSTJSON struct {
	SellerGSTIN   *string `json:"sellerGstin"`
	SellerState   *string `json:"sellerState"`
	BuyerGSTIN    *string `json:"buyerGstin"`
	BuyerName     *string `json:"buyerName"`
	BuyerAddress  *string `json:"buyerAddress"`
	BuyerState    *string `json:"buyerState"`
	PlaceOfSupply *string `json:"placeOfSupply"`
	TaxSplit      string  `json:"taxSplit"`
	InvoiceKind   string  `json:"invoiceKind"`
}

// gstCols are the copy columns, in the order of taxCtx.values (sales and
// sale_returns alike).
const gstCols = `seller_gstin, seller_state, buyer_gstin, buyer_name, buyer_address, buyer_state, place_of_supply, tax_split, invoice_kind`

// gstColsS and gstColsR are gstCols of the sales (s) and sale_returns (r)
// aliases, for the loaders.
const (
	gstColsS = `s.seller_gstin, s.seller_state, s.buyer_gstin, s.buyer_name, s.buyer_address, s.buyer_state, s.place_of_supply, s.tax_split, s.invoice_kind`
	gstColsR = `r.seller_gstin, r.seller_state, r.buyer_gstin, r.buyer_name, r.buyer_address, r.buyer_state, r.place_of_supply, r.tax_split, r.invoice_kind`
)

func (t taxCtx) values() []any {
	return []any{t.sellerGSTIN, t.sellerState, t.buyerGSTIN, t.buyerName, t.buyerAddress, t.buyerState, t.place, ptrOrNil(t.split), ptrOrNil(t.kind)}
}

// ptrOrNil keeps "" out of the copy columns: a document made before M3
// has no copy, and its credit notes have none either.
func ptrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// scanGST reads the copy columns into a JSON value (nil when the document
// has none).
type gstScan struct {
	sellerGSTIN, sellerState, buyerGSTIN, buyerName, buyerAddress, buyerState, place, split, kind *string
}

func (g *gstScan) dest() []any {
	return []any{&g.sellerGSTIN, &g.sellerState, &g.buyerGSTIN, &g.buyerName, &g.buyerAddress, &g.buyerState, &g.place, &g.split, &g.kind}
}

func (g gstScan) json() *GSTJSON {
	if g.split == nil {
		return nil
	}
	return &GSTJSON{SellerGSTIN: g.sellerGSTIN, SellerState: g.sellerState, BuyerGSTIN: g.buyerGSTIN, BuyerName: g.buyerName,
		BuyerAddress: g.buyerAddress, BuyerState: g.buyerState, PlaceOfSupply: g.place, TaxSplit: *g.split, InvoiceKind: deref(g.kind)}
}

func (g gstScan) ctx() taxCtx {
	return taxCtx{sellerGSTIN: g.sellerGSTIN, sellerState: g.sellerState, buyerGSTIN: g.buyerGSTIN, buyerName: g.buyerName,
		buyerAddress: g.buyerAddress, buyerState: g.buyerState, place: g.place, split: deref(g.split), kind: deref(g.kind)}
}

// The buyer's address for the invoice: the billing address, else the
// contact address with its city and country.
const qTaxParties = `SELECT st.gstin, st.state_code, c.gstin, c.state_code, coalesce(c.legal_name, c.name),
		coalesce(c.billing_address, nullif(concat_ws(', ', nullif(c.address, ''), nullif(c.city, ''), nullif(c.country, '')), ''))
	FROM stores st, customers c WHERE st.id = $1 AND c.id = $2`

func firstOf(vals ...*string) *string {
	for _, v := range vals {
		if v != nil && *v != "" {
			return v
		}
	}
	return nil
}

// taxContext works out a sale's tax identity (§4). place is the place of
// supply the cashier chose ("" for the store's state). Rules, in order:
//  1. No state is known (the store and the shop have none): the shop's
//     tax_split setting, as before M3.
//  2. The seller has no GSTIN: no split (not a tax invoice).
//  3. Place of supply in the seller's state: CGST + SGST (CGST + UTGST in a
//     union territory without a legislature); else IGST.
func taxContext(ctx context.Context, tx tenancy.TenantTx, storeID, customerID int64, place string, s settings) (taxCtx, error) {
	var t taxCtx
	var storeGSTIN, storeState *string
	if err := tx.QueryRow(ctx, qTaxParties, storeID, customerID).Scan(&storeGSTIN, &storeState, &t.buyerGSTIN, &t.buyerState,
		&t.buyerName, &t.buyerAddress); err != nil {
		return t, err
	}
	t.kind = "b2c"
	if t.buyerGSTIN != nil {
		t.kind = "b2b"
	}
	t.sellerGSTIN = firstOf(storeGSTIN, s.taxReg)
	t.sellerState = firstOf(storeState, s.stateCode)
	place = strings.TrimSpace(place)
	if place != "" {
		if _, ok := gst.StateOf(place); !ok {
			return t, apperr.Invalid("placeOfSupply", "Choose a GST state code (two digits, such as 29 for Karnataka).")
		}
		if t.sellerState == nil {
			return t, apperr.Invalid("placeOfSupply", "Set the shop's GST state (Settings) before choosing a place of supply.")
		}
	}
	if t.sellerState == nil {
		t.split = s.split
		return t, nil
	}
	p := *t.sellerState
	if place != "" {
		p = place
	}
	t.place = &p
	switch st, _ := gst.StateOf(p); {
	case t.sellerGSTIN == nil:
		t.split = pricing.SplitNone
	case p != *t.sellerState:
		t.split = pricing.SplitIGST
	case st.UT:
		t.split = pricing.SplitCGSTUTGST
	default:
		t.split = pricing.SplitCGSTSGST
	}
	return t, nil
}

// interStateLimit: a B2C invoice to another state above this needs the
// buyer's name, address and state (M3 Q7, in rupees).
var interStateLimit = decimal.NewFromInt(50_000)

// interStateProblem is the M3 Q7 refusal for a sale of total, or "".
func (t taxCtx) interStateProblem(total decimal.Decimal, currency string) string {
	if t.kind != "b2c" || t.place == nil || t.sellerState == nil || *t.place == *t.sellerState || currency != "INR" ||
		!total.GreaterThan(interStateLimit) || t.buyerAddress != nil {
		return ""
	}
	return fmt.Sprintf("A sale of more than ₹%s to another state needs the buyer's name and address. Choose the customer and add their address.",
		interStateLimit.StringFixed(0))
}

// hsnWarnings lists the lines of a B2B sale whose product has no HSN code
// (a warning: the sale is still allowed).
func (t taxCtx) hsnWarnings(ctx context.Context, tx tenancy.TenantTx, pr priced) ([]string, error) {
	if t.kind != "b2b" || len(pr.lines) == 0 {
		return nil, nil
	}
	ids := make([]int64, 0, len(pr.lines))
	for _, l := range pr.lines {
		ids = append(ids, l.v.productID)
	}
	rows, err := tx.Query(ctx, `SELECT name FROM products WHERE id = ANY($1) AND coalesce(hsn_code, '') = '' ORDER BY name`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n+" has no HSN code. A B2B invoice should show it.")
	}
	return out, rows.Err()
}
