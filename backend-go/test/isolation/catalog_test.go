//go:build integration

package isolation

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func pngDataURI(t *testing.T, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, c)
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

func list(t *testing.T, r resp) []map[string]any {
	t.Helper()
	var out []map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.Raw), &out), r.Raw)
	return out
}

func names(items []map[string]any) []string {
	var out []string
	for _, i := range items {
		out = append(out, fmt.Sprint(i["name"]))
	}
	return out
}

func get(t *testing.T, host, path, token string) resp {
	return do(t, rq{method: "GET", host: host, path: path, token: token})
}

func TestMasterDataCRUDAndShapes(t *testing.T) {
	host := tenants["a"].Host
	tok, _ := ownerLogin(t, "a")

	// Categories with an image; the old frontend also sends createdBy.
	c := do(t, rq{method: "POST", host: host, path: "/api/categories", token: tok, body: map[string]any{
		"name": "Beverages", "status": "Active", "image": pngDataURI(t, color.RGBA{R: 255, A: 255}), "createdBy": 1}})
	require.Equal(t, 201, c.Code, c.Raw)
	for _, k := range []string{"id", "name", "status", "image", "created_by", "createdAt", "updatedAt"} {
		require.Contains(t, c.Body, k)
	}
	img := c.Body["image"].(string)
	require.True(t, strings.HasPrefix(img, "/api/files/"), "category images are behind login: %s", img)
	f := get(t, host, img, tok)
	require.Equal(t, 200, f.Code)
	require.Equal(t, "image/png", f.Header.Get("Content-Type"))
	require.Equal(t, 401, get(t, host, img, "").Code, "private files need a token")
	catID := int(c.Body["id"].(float64))

	// Edit forms send the URL back: keeps the same file.
	up := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/categories/%d", catID), token: tok, body: map[string]any{"name": "Drinks", "status": "Inactive", "image": img}})
	require.Equal(t, 200, up.Code, up.Raw)
	require.Equal(t, img, up.Body["image"])
	require.Equal(t, "Inactive", up.Body["status"])
	// An outside URL is refused; "" clears.
	bad := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/categories/%d", catID), token: tok, body: map[string]any{"name": "Drinks", "image": "https://evil.example/x.png"}})
	require.Equal(t, 400, bad.Code)
	cl := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/categories/%d", catID), token: tok, body: map[string]any{"name": "Drinks", "image": ""}})
	require.Equal(t, 200, cl.Code)
	require.Nil(t, cl.Body["image"])
	require.Equal(t, 404, get(t, host, img, tok).Code, "replaced file is gone")

	// Validation, duplicates, unknown fields.
	r := do(t, rq{method: "POST", host: host, path: "/api/categories", token: tok, body: map[string]any{"name": "  "}})
	require.Equal(t, 400, r.Code)
	require.Equal(t, "Category name is required.", r.Body["error"])
	require.Equal(t, 409, do(t, rq{method: "POST", host: host, path: "/api/categories", token: tok, body: map[string]any{"name": "drinks"}}).Code, "names are case-insensitive unique")
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/categories", token: tok, body: map[string]any{"name": "X", "is_admin": true}}).Code)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/categories", token: tok, body: map[string]any{"name": "Svg", "image": "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg onload=alert(1)>"))}}).Code)

	// Soft delete: gone from the list, name reusable, second delete 404.
	require.Equal(t, 204, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/categories/%d", catID), token: tok}).Code)
	require.NotContains(t, names(list(t, get(t, host, "/api/categories", tok))), "Drinks")
	require.Equal(t, 404, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/categories/%d", catID), token: tok}).Code)
	re := do(t, rq{method: "POST", host: host, path: "/api/categories", token: tok, body: map[string]any{"name": "Drinks"}})
	require.Equal(t, 201, re.Code)

	// Sub-categories: categoryId arrives as a string from a <select>.
	sc := do(t, rq{method: "POST", host: host, path: "/api/sub-categories", token: tok, body: map[string]any{"name": "Juice", "status": "Active", "categoryId": fmt.Sprint(re.Body["id"])}})
	require.Equal(t, 201, sc.Code, sc.Raw)
	require.Equal(t, "Drinks", sc.Body["Category"].(map[string]any)["name"])
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/sub-categories", token: tok, body: map[string]any{"name": "Orphan", "categoryId": 99999}}).Code)

	// Units, brands, suppliers, warehouses.
	u := do(t, rq{method: "POST", host: host, path: "/api/units", token: tok, body: map[string]any{"name": "Kilogram", "short_name": "kg"}})
	require.Equal(t, 201, u.Code, u.Raw)
	require.Equal(t, "kg", u.Body["short_name"])
	require.Equal(t, 201, do(t, rq{method: "POST", host: host, path: "/api/brands", token: tok, body: map[string]any{"name": "Acme"}}).Code)
	s := do(t, rq{method: "POST", host: host, path: "/api/suppliers", token: tok, body: map[string]any{"name": "Wholesale Co", "email": "sales@wholesale.test", "phone": "+91 99999 00000"}})
	require.Equal(t, 201, s.Code, s.Raw)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/suppliers", token: tok, body: map[string]any{"name": "Bad", "email": "not-an-email"}}).Code)
	wh := do(t, rq{method: "POST", host: host, path: "/api/warehouses", token: tok, body: map[string]any{"name": "Back room", "address": "Lane 4", "status": "Active"}})
	require.Equal(t, 201, wh.Code, wh.Raw)
	require.Contains(t, names(list(t, get(t, host, "/api/warehouses", tok))), "Back room")

	// Customers: email validated; the walk-in customer cannot be deleted.
	cu := do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "Priya", "email": "priya@example.test", "phone": "98765", "city": "Pune", "country": "India", "address": "", "status": "Active"}})
	require.Equal(t, 201, cu.Code, cu.Raw)
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "X", "email": "nope"}}).Code)
	var walkIn int64
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT default_customer_id FROM `+schema("a")+`.settings`).Scan(&walkIn))
	require.Equal(t, 409, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/customers/%d", walkIn), token: tok}).Code)
	search := list(t, get(t, host, "/api/customers?search=pri", tok))
	require.Equal(t, []string{"Priya"}, names(search))

	// Pagination shape (Q-4).
	pg := get(t, host, "/api/customers?page=1&limit=1", tok)
	require.Equal(t, 200, pg.Code)
	require.Len(t, pg.Body["data"], 1)
	require.EqualValues(t, 2, pg.Body["pagination"].(map[string]any)["total"])
}

func TestStoresShowcaseAndPublicFiles(t *testing.T) {
	host := tenants["b"].Host
	tok, _ := ownerLogin(t, "b")

	// Stores: slug generated and unique; partial PUT keeps fields.
	s1 := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "City Center", "address": "MG Road", "status": "Active"}})
	require.Equal(t, 201, s1.Code, s1.Raw)
	require.Equal(t, "city-center", s1.Body["slug"])
	s2 := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "City Center"}})
	require.Equal(t, "city-center-2", s2.Body["slug"])
	id := int(s1.Body["id"].(float64))
	up := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/stores/%d", id), token: tok, body: map[string]any{"name": "City Centre"}})
	require.Equal(t, 200, up.Code)
	require.Equal(t, "MG Road", up.Body["address"], "missing fields keep their value (P2-9)")

	// Plan limit: the trial plan allows 3 stores (seeded + 2).
	lim := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Fourth"}})
	require.Equal(t, 403, lim.Code, lim.Raw)
	require.Equal(t, "PLAN_LIMIT_REACHED", lim.Body["code"])

	// Showcase: GET does not write; PUT validates the slug and stores images.
	sh := get(t, host, fmt.Sprintf("/api/stores/%d/showcase", id), tok)
	require.Equal(t, 200, sh.Code)
	require.Equal(t, "city-center", sh.Body["store"].(map[string]any)["slug"])
	require.Equal(t, 400, do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/stores/%d/showcase", id), token: tok, body: map[string]any{"slug": "Bad Slug!!"}}).Code)
	require.Equal(t, 409, do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/stores/%d/showcase", id), token: tok, body: map[string]any{"slug": "city-center-2"}}).Code)
	put := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/stores/%d/showcase", id), token: tok, body: map[string]any{
		"name": "ignored", "address": "ignored", "slug": "centre", "description": "Open daily", "phone": "123",
		"openingHours": "9-9", "isPublicEnabled": true, "logo": pngDataURI(t, color.RGBA{G: 255, A: 255}), "banner": ""}})
	require.Equal(t, 200, put.Code, put.Raw)
	logo := put.Body["logo"].(string)
	require.True(t, strings.HasPrefix(logo, "/files/t/"), logo)
	require.Equal(t, "centre", put.Body["slug"])

	// Public allow-list: served without login on its own host only.
	pub := do(t, rq{method: "GET", host: host, path: logo})
	require.Equal(t, 200, pub.Code)
	require.Equal(t, "image/png", pub.Header.Get("Content-Type"))
	require.Contains(t, pub.Header.Get("Cache-Control"), "public")
	require.Equal(t, 404, do(t, rq{method: "GET", host: tenants["a"].Host, path: logo}).Code, "another tenant's host never serves it")
	etag := pub.Header.Get("ETag")
	require.Equal(t, 304, do(t, rq{method: "GET", host: host, path: logo, header: map[string]string{"If-None-Match": etag}}).Code)

	// A customer image is never public, even if someone guesses the URL form.
	cu := do(t, rq{method: "POST", host: host, path: "/api/customers", token: tok, body: map[string]any{"name": "Face", "image": pngDataURI(t, color.RGBA{B: 255, A: 255})}})
	require.Equal(t, 201, cu.Code, cu.Raw)
	priv := cu.Body["image"].(string)
	require.Equal(t, 404, do(t, rq{method: "GET", host: host, path: strings.Replace(priv, "/api/files/", "/files/t/", 1)}).Code)
	require.Equal(t, 200, get(t, host, priv, tok).Code)

	// A deleted store's logo stops being public.
	require.Equal(t, 200, do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/stores/%d", id), token: tok}).Code)
	require.Equal(t, 404, do(t, rq{method: "GET", host: host, path: logo}).Code)

	// The last store cannot be deleted.
	stores := list(t, get(t, host, "/api/stores", tok))
	for i, st := range stores {
		code := do(t, rq{method: "DELETE", host: host, path: fmt.Sprintf("/api/stores/%v", st["id"]), token: tok}).Code
		if i == len(stores)-1 {
			require.Equal(t, 409, code, "last store")
		} else {
			require.Equal(t, 200, code)
		}
	}
}

func TestFileUploadThenAttach(t *testing.T) {
	host := tenants["a"].Host
	tok, _ := ownerLogin(t, "a")
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(pngDataURI(t, color.RGBA{R: 9, A: 255}), "data:image/png;base64,"))
	require.NoError(t, err)

	upload := func(purpose string, token string) resp {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		_ = mw.WriteField("purpose", purpose)
		fw, _ := mw.CreateFormFile("file", "x.png")
		_, _ = fw.Write(raw)
		_ = mw.Close()
		req, _ := http.NewRequest("POST", baseURL+"/api/files", &body)
		req.Host = host
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Forwarded-For", "10.200.0.1")
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = res.Body.Close() }()
		b, _ := io.ReadAll(res.Body)
		out := resp{Code: res.StatusCode, Raw: string(b)}
		_ = json.Unmarshal(b, &out.Body)
		return out
	}
	up := upload("brand_image", tok)
	require.Equal(t, 201, up.Code, up.Raw)
	url := up.Body["url"].(string)
	b := do(t, rq{method: "POST", host: host, path: "/api/brands", token: tok, body: map[string]any{"name": "Uploaded", "image": url}})
	require.Equal(t, 201, b.Code, b.Raw)
	require.Equal(t, url, b.Body["image"])
	require.Equal(t, 400, do(t, rq{method: "POST", host: host, path: "/api/brands", token: tok, body: map[string]any{"name": "Again", "image": url}}).Code, "a file attaches once")
	require.Equal(t, 400, upload("receipt", tok).Code, "no arbitrary purposes")

	bt := createUser(t, "a", tok, "uploader@iso-a.test", "biller", "uploader-pass-word-1")
	_ = bt
	btok, _ := login(t, "a", "uploader@iso-a.test", "uploader-pass-word-1")
	require.Equal(t, 403, upload("brand_image", btok).Code, "biller cannot upload brand images")
	require.Equal(t, 404, get(t, host, url, btok).Code, "biller lacks brands:view")
}

// Every master-data entity: what tenant A creates is invisible to tenant B,
// and B cannot change or delete it by ID.
func TestMasterDataIsolationByID(t *testing.T) {
	aTok, _ := ownerLogin(t, "a")
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	for _, e := range []struct{ path, name string }{
		{"/api/categories", "Secret Cat A"}, {"/api/brands", "Secret Brand A"}, {"/api/units", "SecretUnitA"},
		{"/api/suppliers", "Secret Supplier A"}, {"/api/customers", "Secret Customer A"}, {"/api/warehouses", "Secret WH A"},
	} {
		created := do(t, rq{method: "POST", host: tenants["a"].Host, path: e.path, token: aTok, body: map[string]any{"name": e.name}})
		require.Equal(t, 201, created.Code, e.path+created.Raw)
		id := created.Body["id"]
		l := get(t, tenants["c"].Host, e.path, cTok)
		require.Equal(t, 200, l.Code)
		require.NotContains(t, l.Raw, e.name, e.path)
		// IDs collide across tenants; pick one C does not have.
		var cHas bool
		for _, it := range list(t, l) {
			cHas = cHas || it["id"] == id
		}
		if !cHas {
			require.Equal(t, 404, do(t, rq{method: "PUT", host: tenants["c"].Host, path: fmt.Sprintf("%s/%v", e.path, id), token: cTok, body: map[string]any{"name": "pwned"}}).Code, e.path)
			require.Equal(t, 404, do(t, rq{method: "DELETE", host: tenants["c"].Host, path: fmt.Sprintf("%s/%v", e.path, id), token: cTok}).Code, e.path)
		}
		still := get(t, tenants["a"].Host, e.path, aTok)
		require.Contains(t, still.Raw, e.name, "A's row untouched")
	}
	// Files: A's private file ID is unknown on C's host.
	cu := do(t, rq{method: "POST", host: tenants["a"].Host, path: "/api/customers", token: aTok, body: map[string]any{"name": "Img", "image": pngDataURI(t, color.RGBA{R: 1, A: 255})}})
	require.Equal(t, 201, cu.Code)
	require.Equal(t, 404, get(t, tenants["c"].Host, cu.Body["image"].(string), cTok).Code)
}

func TestBillerSeesStoresButCannotManage(t *testing.T) {
	host := tenants["a"].Host
	owner, _ := ownerLogin(t, "a")
	createUser(t, "a", owner, "storeview@iso-a.test", "biller", "storeview-pass-12")
	btok, _ := login(t, "a", "storeview@iso-a.test", "storeview-pass-12")
	require.Equal(t, 200, get(t, host, "/api/stores", btok).Code, "biller has stores:view (C-08)")
	require.Equal(t, 200, get(t, host, "/api/warehouses", btok).Code)
	require.Equal(t, 200, get(t, host, "/api/customers", btok).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/stores", token: btok, body: map[string]any{"name": "Nope"}}).Code)
	require.Equal(t, 403, do(t, rq{method: "POST", host: host, path: "/api/warehouses", token: btok, body: map[string]any{"name": "Nope"}}).Code)
	require.Equal(t, 403, get(t, host, "/api/categories", btok).Code, "biller has no categories:view")
	require.Equal(t, 201, do(t, rq{method: "POST", host: host, path: "/api/customers", token: btok, body: map[string]any{"name": "Walk up"}}).Code, "biller can add customers at POS")
}
