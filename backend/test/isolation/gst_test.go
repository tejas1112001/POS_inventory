//go:build integration

package isolation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Published GSTINs with a correct check character: Maharashtra (27) and
// Karnataka (29).
const (
	gstinMH = "27AAPFU0939F1ZV"
	gstinKA = "29AAGCB7383J1Z4"
)

// M3 step 1 (docs/M3_GST_DESIGN.md DB1-DB4, A1-A3, A6): the state list,
// the shop's state, and the GSTIN and state of stores, customers and
// suppliers. A GSTIN sets its own state, a GSTIN of another state is
// refused (Q6), and fields a request leaves out are kept.
func TestGSTParties(t *testing.T) {
	l := billingTenant(t, "gst-parties")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	call := func(method, path string, body map[string]any) resp {
		return do(t, rq{method: method, host: host, path: path, token: tok, body: body})
	}

	// The state list.
	st := call("GET", "/api/gst/states", nil)
	require.Equal(t, 200, st.Code, st.Raw)
	require.Contains(t, st.Raw, `{"code":"27","name":"Maharashtra","ut":false}`)
	require.Contains(t, st.Raw, `{"code":"04","name":"Chandigarh","ut":true}`)

	// The shop's state; its GSTIN must be of that state.
	r := call("PUT", "/api/settings", map[string]any{"stateCode": "7"})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "stateCode", errField(r))
	r = call("PUT", "/api/settings", map[string]any{"taxLabel": "GST", "taxRegistrationNo": gstinKA, "stateCode": "27"})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "taxRegistrationNo", errField(r))
	r = call("PUT", "/api/settings", map[string]any{"taxLabel": "GST", "taxRegistrationNo": gstinMH, "stateCode": "27"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, "27", r.Body["stateCode"])
	r = call("PUT", "/api/settings", map[string]any{"stateCode": ""})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Nil(t, r.Body["stateCode"], `"" clears it`)

	// A store in Karnataka: the state comes from its GSTIN.
	s := call("POST", "/api/stores", map[string]any{"name": "Bengaluru branch", "gstin": " 29aagcb7383j1z4 ", "invoiceCode": "blr"})
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, gstinKA, s.Body["gstin"])
	require.Equal(t, "29", s.Body["stateCode"])
	require.Equal(t, "BLR", s.Body["invoiceCode"])
	blr := jsonID(num(s.Body["id"]))

	// Refused: another store's invoice code, a GSTIN of another state, a
	// bad check character, a bad code.
	main := jsonID(storeIDs(t, l, tok)[0])
	for _, c := range []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"invoiceCode": "BLR"}, "invoiceCode"},
		{map[string]any{"gstin": gstinKA, "stateCode": "27"}, "gstin"},
		{map[string]any{"gstin": "27AAPFU0939F1ZW"}, "gstin"},
		{map[string]any{"invoiceCode": "TOO-LONG"}, "invoiceCode"},
		{map[string]any{"stateCode": "00"}, "stateCode"},
	} {
		r = call("PUT", "/api/stores/"+main, c.body)
		require.Equal(t, 400, r.Code, "%v: %s", c.body, r.Raw)
		require.Equal(t, c.field, errField(r), "%v", c.body)
	}

	// Fields left out are kept; "" clears the GSTIN and keeps the state.
	r = call("PUT", "/api/stores/"+blr, map[string]any{"name": "Bengaluru MG Road"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, gstinKA, r.Body["gstin"])
	require.Equal(t, "BLR", r.Body["invoiceCode"])
	r = call("PUT", "/api/stores/"+blr, map[string]any{"gstin": ""})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Nil(t, r.Body["gstin"])
	require.Equal(t, "29", r.Body["stateCode"])

	// A registered buyer from Karnataka, then without the GSTIN; an
	// unregistered buyer with only a state.
	cu := call("POST", "/api/customers", map[string]any{"name": "Kaveri Traders", "gstin": gstinKA,
		"legalName": "Kaveri Traders Pvt Ltd", "billingAddress": "4 Residency Road, Bengaluru"})
	require.Equal(t, 201, cu.Code, cu.Raw)
	require.Equal(t, gstinKA, cu.Body["gstin"])
	require.Equal(t, "29", cu.Body["stateCode"])
	require.Equal(t, "Kaveri Traders Pvt Ltd", cu.Body["legalName"])
	cid := jsonID(num(cu.Body["id"]))
	r = call("PUT", "/api/customers/"+cid, map[string]any{"name": "Kaveri Traders", "phone": "080 1234"})
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, gstinKA, r.Body["gstin"], "an old form without the fields keeps them")
	require.Equal(t, "4 Residency Road, Bengaluru", r.Body["billingAddress"])
	r = call("PUT", "/api/customers/"+cid, map[string]any{"name": "Kaveri Traders", "gstin": gstinMH, "stateCode": "29"})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "gstin", errField(r))
	walk := call("POST", "/api/customers", map[string]any{"name": "Asha from Pune", "stateCode": "27"})
	require.Equal(t, 201, walk.Code, walk.Raw)
	require.Nil(t, walk.Body["gstin"])
	require.Equal(t, "27", walk.Body["stateCode"])

	// A supplier's GSTIN (purchase tax is a later module).
	su := call("POST", "/api/suppliers", map[string]any{"name": "Deccan Wholesale", "gstin": gstinMH})
	require.Equal(t, 201, su.Code, su.Raw)
	require.Equal(t, "27", su.Body["stateCode"])
	r = call("POST", "/api/suppliers", map[string]any{"name": "Typo Supplies", "gstin": "27AAPFU0939F1Z"})
	require.Equal(t, 400, r.Code, r.Raw)
	require.Equal(t, "gstin", errField(r))
}
