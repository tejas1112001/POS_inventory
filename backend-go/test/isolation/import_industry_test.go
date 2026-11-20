//go:build integration

package isolation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/xlsx"
)

// uploadFile posts a product file (A13) in the "file" field.
func uploadFile(t *testing.T, l, tok, name string, data []byte, dry bool) resp {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = mw.Close()
	path := baseURL + "/api/products/import"
	if dry {
		path += "?dryRun=true"
	}
	req, _ := http.NewRequest("POST", path, &body)
	req.Host = tenants[l].Host
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Forwarded-For", "10.201.0.2")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	out := resp{Code: res.StatusCode, Raw: string(b), Header: res.Header}
	out.Body = map[string]any{}
	_ = json.Unmarshal(b, &out.Body)
	return out
}

func workbook(t *testing.T, rows ...[]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := xlsx.New(&buf, "Products", "#,##0.00", nil)
	require.NoError(t, err)
	for _, r := range rows {
		cells := make([]xlsx.Cell, len(r))
		for i, v := range r {
			cells[i] = xlsx.Text(v, xlsx.Plain)
		}
		require.NoError(t, w.Row(cells...))
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func sheetRows(t *testing.T, raw string) [][]string {
	t.Helper()
	rows, err := xlsx.Read([]byte(raw))
	require.NoError(t, err)
	return rows
}

// Industry P1 item 6: templates per business type (A12), .xlsx import with
// sizes and colours and the dry-run preview (A13), the error report (A14)
// and its isolation.
func TestIndustryImport(t *testing.T) {
	l := billingTenant(t, "imp-shop")
	setModules(t, l, "clothing")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	store := storeIDs(t, l, tok)[0]
	storeName := ""
	for _, s := range list(t, get(t, host, "/api/stores", tok)) {
		if s["id"] == store {
			storeName = s["name"].(string)
		}
	}

	// A12: the clothing template, as Excel and CSV; retail unchanged.
	x := get(t, host, "/api/products/import/template?type=clothing&format=xlsx", tok)
	require.Equal(t, 200, x.Code, x.Raw)
	require.Contains(t, x.Header.Get("Content-Type"), "spreadsheetml")
	tpl := sheetRows(t, x.Raw)
	require.Equal(t, []string{"article_no", "name", "department", "collection", "season", "size", "colour", "sku", "barcode", "price", "cost",
		"mrp", "category", "brand", "tax_type", "tax_rate", "hsn_code", "opening_qty", "quantity_alert"}, tpl[0])
	require.Equal(t, "TSH-01", tpl[1][0])
	c := get(t, host, "/api/products/import/template?type=clothing", tok)
	require.Equal(t, 200, c.Code)
	require.True(t, strings.HasPrefix(c.Raw, "article_no,name,department,collection,season,size,colour,sku"), c.Raw)
	fw := get(t, host, "/api/products/import/template?type=footwear&format=csv", tok)
	require.NotContains(t, strings.SplitN(fw.Raw, "\n", 2)[0], "size_system", "a clothing shop has no size system attribute")
	require.True(t, strings.HasPrefix(get(t, host, "/api/products/import/template", tok).Raw, "name,sku,price,cost,mrp,barcode"))
	require.Equal(t, 400, get(t, host, "/api/products/import/template?type=bakery", tok).Code)

	// A13: an Excel file; rows with the same article number are one
	// product; the dry run shows the plan and writes nothing.
	file := workbook(t,
		[]string{"Article_No", "Name", "Department", "Season", "Size", "Colour", "SKU", "Price", "Opening_Qty"},
		[]string{"TSH-01", "Crew tee", "Men", "spring/summer", "m", "Blue", "TSH-01-M-BLUE", "499", "5"},
		[]string{"TSH-01", "Crew tee", "", "", "L", "Blue", "TSH-01-L-BLUE", "499", "3"},
		[]string{},
		[]string{"BAG-9", "Tote bag", "Women", "", "", "", "BAG-9", "899", ""},
	)
	dry := uploadFile(t, l, tok, "stock.xlsx", file, true)
	require.Equal(t, 200, dry.Code, dry.Raw)
	require.EqualValues(t, 2, dry.Body["created"])
	require.EqualValues(t, 3, dry.Body["variants"])
	pv := dry.Body["preview"].([]any)
	tee := pv[0].(map[string]any)
	require.Equal(t, "TSH-01", tee["articleNo"])
	require.Equal(t, "Spring/Summer", tee["attributes"].(map[string]any)["season"])
	require.Equal(t, []any{"size", "colour"}, tee["variantDims"])
	v1 := tee["variants"].([]any)[1].(map[string]any)
	require.Equal(t, "L / Blue", v1["label"])
	require.EqualValues(t, 3, v1["row"])
	require.Equal(t, []any{map[string]any{"store": storeName, "qty": 3.0}}, v1["stock"], "the first store when there is no store column")
	require.Empty(t, pv[1].(map[string]any)["variantDims"])
	require.NotContains(t, names(list(t, get(t, host, "/api/products", tok))), "Crew tee", "a dry run writes nothing")

	real := uploadFile(t, l, tok, "stock.xlsx", file, false)
	require.Equal(t, 200, real.Code, real.Raw)
	require.Nil(t, real.Body["preview"])
	found := list(t, get(t, host, "/api/products?search=tsh-01", tok))
	require.Len(t, found, 1)
	p := get(t, host, fmt.Sprintf("/api/products/%v", found[0]["id"]), tok).Body
	require.Equal(t, "variable", p["productType"])
	require.Equal(t, "Men", p["attributes"].(map[string]any)["department"])
	labels := []string{}
	for _, v := range p["ProductVariants"].([]any) {
		labels = append(labels, v.(map[string]any)["label"].(string))
	}
	require.ElementsMatch(t, []string{"M / Blue", "L / Blue"}, labels)
	require.EqualValues(t, 8, found[0]["qty"])

	// Errors: a list and a downloadable report with each original row.
	bad := "article_no,name,department,size,colour,sku,barcode,price\n" +
		"JN-1,Jeans,Men,M,Blue,JN-1-M,,999\n" +
		"JN-1,Jeans,Women,XXXL,Blue,JN-1-X,,999\n" +
		"JN-1,Jeans,,m,blue,JN-1-M2,,999\n" +
		"JN-2,Shorts,Men,S,Red,JN-2-S,8.90123E+12,499\n"
	r := uploadFile(t, l, tok, "bad.csv", []byte(bad), false)
	require.Equal(t, 400, r.Code, r.Raw)
	msgs := []string{}
	for _, e := range r.Body["errors"].([]any) {
		m := e.(map[string]any)
		msgs = append(msgs, fmt.Sprintf("%v %v %v", m["row"], m["field"], m["message"]))
	}
	joined := strings.Join(msgs, "\n")
	require.Contains(t, joined, "3 department Department differs from row 2")
	require.Contains(t, joined, "3 size ")
	require.Contains(t, joined, "5 barcode Excel changed this code into a number")
	token, _ := r.Body["errorReport"].(string)
	require.Len(t, token, 32)
	rep := get(t, host, "/api/products/import/errors/"+token, tok)
	require.Equal(t, 200, rep.Code)
	rows := sheetRows(t, rep.Raw)
	require.Equal(t, []string{"Row", "Column", "Problem", "article_no", "name"}, rows[0][:5])
	require.Contains(t, strings.Join(rows[len(rows)-1], "|"), "8.90123E+12")
	require.NotContains(t, names(list(t, get(t, host, "/api/products", tok))), "Jeans", "all or nothing")

	dup := "article_no,name,size,colour,sku,price\nJN-3,Chinos,M,Blue,JN-3-A,1\nJN-3,Chinos,m,blue,JN-3-B,1\n"
	d := uploadFile(t, l, tok, "dup.csv", []byte(dup), false)
	require.Equal(t, 400, d.Code)
	require.Contains(t, d.Raw, "the same size and colour as row 2")
	require.Equal(t, 404, get(t, host, "/api/products/import/errors/"+token, tok).Code, "a new report replaces the user's last one")
	token = d.Body["errorReport"].(string)

	// A14 isolation: another user of the shop, another shop, a bad token.
	createUser(t, l, tok, "admin2@"+l+".test", "admin", "imp-admin-pass-1")
	atok, _ := login(t, l, "admin2@"+l+".test", "imp-admin-pass-1")
	require.Equal(t, 404, get(t, host, "/api/products/import/errors/"+token, atok).Code)
	require.Equal(t, 404, get(t, host, "/api/products/import/errors/not-a-token", tok).Code)
	l2 := billingTenant(t, "imp-other")
	tok2, _ := ownerLogin(t, l2)
	h2 := tenants[l2].Host
	require.Equal(t, 404, get(t, h2, "/api/products/import/errors/"+token, tok2).Code)
	require.Equal(t, 401, get(t, h2, "/api/products/import/errors/"+token, tok).Code)
	require.Equal(t, 200, get(t, host, "/api/products/import/errors/"+token, tok).Code)

	// A biller cannot import or fetch templates.
	createUser(t, l, tok, "biller@"+l+".test", "biller", "imp-biller-pass-1")
	btok, _ := login(t, l, "biller@"+l+".test", "imp-biller-pass-1")
	require.Equal(t, 403, get(t, host, "/api/products/import/template?type=clothing&format=xlsx", btok).Code)
	require.Equal(t, 403, uploadFile(t, l, btok, "stock.xlsx", file, true).Code)
}
