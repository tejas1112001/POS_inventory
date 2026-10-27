//go:build integration

package isolation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// pub is an anonymous request to the public shop on tenant l's host.
func pub(t *testing.T, l, path string, hdr map[string]string) resp {
	t.Helper()
	return do(t, rq{method: "GET", host: tenants[l].Host, path: "/api/public/shop" + path, header: hdr})
}

func publish(t *testing.T, l, tok string, store, product float64, field string) {
	t.Helper()
	r := do(t, rq{method: "POST", host: tenants[l].Host, path: fmt.Sprintf("/api/stores/%v/showcase/toggle-product", jsonID(store)),
		token: tok, body: map[string]any{"productId": product, "field": field, "value": true}})
	require.Equal(t, 200, r.Code, r.Raw)
}

func shopNames(t *testing.T, r resp) []string {
	t.Helper()
	require.Equal(t, 200, r.Code, r.Raw)
	var out []string
	for _, x := range r.Body["data"].([]any) {
		out = append(out, x.(map[string]any)["name"].(string))
	}
	return out
}

// shopWindow creates a showcase store in tenant C (A and B already use
// all three stores the trial plan allows) once per run, with products in
// known states.
type window struct {
	tok, slug                 string
	id                        float64
	cheap, discounted, plain  float64
	hidden, inactive, mainPub float64
	category                  float64
}

var shopFixture *window

func shopWindow(t *testing.T) *window {
	t.Helper()
	if shopFixture != nil {
		return shopFixture
	}
	setStatus(t, "c", "trialing")
	tok, _ := ownerLogin(t, "c")
	host := tenants["c"].Host
	s := do(t, rq{method: "POST", host: host, path: "/api/stores", token: tok, body: map[string]any{"name": "Shop Window", "status": "Active"}})
	require.Equal(t, 201, s.Code, s.Raw)
	w := &window{tok: tok, id: s.Body["id"].(float64), slug: s.Body["slug"].(string)}
	cat := do(t, rq{method: "POST", host: host, path: "/api/categories", token: tok, body: map[string]any{"name": "Shop Teas", "status": "Active"}})
	require.Equal(t, 201, cat.Code, cat.Raw)
	w.category = cat.Body["id"].(float64)
	do(t, rq{method: "POST", host: host, path: "/api/categories", token: tok, body: map[string]any{"name": "Shop Unused", "status": "Active"}})

	mk := func(sku string, qty int, edit func(map[string]any)) float64 {
		b := addFormSingle(sku, w.id, qty)
		if edit != nil {
			edit(b)
		}
		p := createProduct(t, "c", tok, b)
		require.Equal(t, 201, p.Code, p.Raw)
		return p.Body["id"].(float64)
	}
	w.plain = mk("SHOP-PLAIN", 2, func(b map[string]any) { b["categoryId"] = jsonID(w.category); b["slug"] = "shop-plain-tea" })
	w.discounted = mk("SHOP-DISC", 0, func(b map[string]any) { b["discountType"] = "Percentage"; b["discountValue"] = "10" })
	vp := createProduct(t, "c", tok, map[string]any{"name": "Shop Cheap Sampler", "productType": "variable", "variants": []map[string]any{
		{"sku": "SHOP-CHEAP-S", "price": "10", "attributes": map[string]any{"size": "S"}, "inventories": []map[string]any{{"storeId": w.id, "qty": 1}}},
		{"sku": "SHOP-CHEAP-L", "price": "15", "attributes": map[string]any{"size": "L"}}}})
	require.Equal(t, 201, vp.Code, vp.Raw)
	w.cheap = vp.Body["id"].(float64)
	w.hidden = mk("SHOP-HIDDEN", 3, nil)
	w.inactive = mk("SHOP-INACTIVE", 3, func(b map[string]any) { b["status"] = "Inactive" })
	w.mainPub = mk("SHOP-MAIN", 3, nil)
	for _, p := range []float64{w.plain, w.discounted, w.cheap, w.inactive} {
		publish(t, "c", tok, w.id, p, "isPublished")
	}
	publish(t, "c", tok, w.id, w.plain, "isFeatured")
	// Featured but never published: it has a showcase row and must still
	// stay hidden everywhere.
	publish(t, "c", tok, w.id, w.hidden, "isFeatured")
	publish(t, "c", tok, storeIDs(t, "c", tok)[0], w.mainPub, "isPublished")
	shopFixture = w
	return w
}

func TestPublicShopShowsOnlyPublishedProducts(t *testing.T) {
	w := shopWindow(t)
	base := "/" + w.slug

	// Shop header, cacheable, with a working ETag.
	info := pub(t, "c", base, nil)
	require.Equal(t, 200, info.Code, info.Raw)
	require.Equal(t, "Shop Window", info.Body["name"])
	require.Equal(t, true, info.Body["isPublicEnabled"])
	require.NotContains(t, info.Body, "id", "no internal store ID")
	require.Equal(t, "public, max-age=60", info.Header.Get("Cache-Control"))
	etag := info.Header.Get("ETag")
	require.NotEmpty(t, etag)
	require.Equal(t, 304, pub(t, "c", base, map[string]string{"If-None-Match": etag}).Code)

	// Published, active products of this store only; newest first.
	list := pub(t, "c", base+"/products", nil)
	require.ElementsMatch(t, []string{"Product SHOP-PLAIN", "Product SHOP-DISC", "Shop Cheap Sampler"}, shopNames(t, list))
	require.Equal(t, 3.0, num(list.Body["pagination"].(map[string]any)["total"]))
	for _, banned := range []string{`"qty"`, `"totalQty"`, `"cost"`, `"itemBarcode"`, `"weight"`, `"supplier`, `"created_by"`, "SHOP-HIDDEN", "SHOP-MAIN", "SHOP-INACTIVE"} {
		require.NotContains(t, list.Raw, banned)
	}
	byName := map[string]map[string]any{}
	for _, x := range list.Body["data"].([]any) {
		byName[x.(map[string]any)["name"].(string)] = x.(map[string]any)
	}
	d := byName["Product SHOP-DISC"]
	require.Equal(t, true, d["hasDiscount"])
	require.Equal(t, 120.5, num(d["price"]))
	require.Equal(t, 108.45, num(d["finalPrice"]))
	require.Equal(t, false, d["inStock"])
	require.Equal(t, "Out of Stock", d["stockStatus"])
	p := byName["Product SHOP-PLAIN"]
	require.Equal(t, true, p["inStock"])
	require.Equal(t, "Shop Teas", p["category"])
	require.Equal(t, true, p["isFeatured"])
	c := byName["Shop Cheap Sampler"]
	require.Len(t, c["variants"].([]any), 2)
	require.Equal(t, 10.0, num(c["price"]), "the first variant's price")
	require.Equal(t, false, c["variants"].([]any)[1].(map[string]any)["inStock"])

	// Sorting in SQL, across the whole result; filters; the page size cap.
	require.Equal(t, []string{"Shop Cheap Sampler", "Product SHOP-DISC", "Product SHOP-PLAIN"}, shopNames(t, pub(t, "c", base+"/products?sort=price_asc", nil)))
	require.Equal(t, []string{"Product SHOP-PLAIN", "Product SHOP-DISC", "Shop Cheap Sampler"}, shopNames(t, pub(t, "c", base+"/products?sort=price_desc", nil)))
	require.Equal(t, []string{"Product SHOP-DISC", "Product SHOP-PLAIN", "Shop Cheap Sampler"}, shopNames(t, pub(t, "c", base+"/products?sort=name_asc", nil)))
	require.Equal(t, []string{"Shop Cheap Sampler"}, shopNames(t, pub(t, "c", base+"/products?search=sampler", nil)))
	require.Empty(t, shopNames(t, pub(t, "c", base+"/products?search=%25", nil)), "LIKE wildcards are literal")
	require.Equal(t, []string{"Product SHOP-PLAIN"}, shopNames(t, pub(t, "c", base+"/products?categoryId="+jsonID(w.category), nil)))
	require.Equal(t, []string{"Product SHOP-PLAIN"}, shopNames(t, pub(t, "c", base+"/products?featured=true", nil)))
	paged := pub(t, "c", base+"/products?limit=1000&page=2", nil)
	require.Equal(t, 48.0, num(paged.Body["pagination"].(map[string]any)["limit"]), "limit is capped at 48")
	require.Empty(t, paged.Body["data"].([]any))
	two := pub(t, "c", base+"/products?limit=2&page=2&sort=name_asc", nil)
	require.Equal(t, []string{"Shop Cheap Sampler"}, shopNames(t, two))
	require.Equal(t, 2.0, num(two.Body["pagination"].(map[string]any)["totalPages"]))

	feat := pub(t, "c", base+"/featured", nil)
	require.Equal(t, 200, feat.Code)
	require.Contains(t, feat.Raw, "Product SHOP-PLAIN")
	require.NotContains(t, feat.Raw, "SHOP-DISC")
	require.NotContains(t, feat.Raw, "SHOP-HIDDEN", "featured needs published")
	cats := pub(t, "c", base+"/categories", nil)
	require.Equal(t, 200, cats.Code)
	require.Contains(t, cats.Raw, "Shop Teas")
	require.NotContains(t, cats.Raw, "Shop Unused", "only categories with published products")

	// Product pages: by ID or slug, published here only.
	one := pub(t, "c", base+"/products/"+jsonID(w.plain), nil)
	require.Equal(t, 200, one.Code, one.Raw)
	require.Equal(t, "Product SHOP-PLAIN", one.Body["product"].(map[string]any)["name"])
	require.Equal(t, "Shop Window", one.Body["shop"].(map[string]any)["name"])
	require.Equal(t, 200, pub(t, "c", base+"/products/shop-plain-tea", nil).Code)
	for _, id := range []float64{w.hidden, w.inactive, w.mainPub} {
		require.Equal(t, 404, pub(t, "c", base+"/products/"+jsonID(id), nil).Code)
	}
	require.Equal(t, 404, pub(t, "c", base+"/products/no-such-slug", nil).Code)

	// No numeric store lookup; unknown and malformed slugs look the same.
	unknown := pub(t, "c", "/no-such-shop", nil)
	require.Equal(t, 404, unknown.Code)
	require.Equal(t, unknown.Raw, pub(t, "c", "/"+jsonID(w.id), nil).Raw)
	require.Equal(t, 404, pub(t, "c", "/bad_slug!", nil).Code)

	// A disabled showcase says so on the header and shows nothing else.
	host := tenants["c"].Host
	off := do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/stores/%s/showcase", jsonID(w.id)), token: w.tok,
		body: map[string]any{"isPublicEnabled": false}})
	require.Equal(t, 200, off.Code, off.Raw)
	t.Cleanup(func() {
		do(t, rq{method: "PUT", host: host, path: fmt.Sprintf("/api/stores/%s/showcase", jsonID(w.id)), token: w.tok,
			body: map[string]any{"isPublicEnabled": true}})
	})
	closed := pub(t, "c", base, nil)
	require.Equal(t, 200, closed.Code)
	require.Equal(t, false, closed.Body["isPublicEnabled"])
	require.NotEmpty(t, closed.Body["message"])
	require.NotContains(t, closed.Body, "address")
	for _, path := range []string{"/products", "/featured", "/categories", "/products/" + jsonID(w.plain)} {
		require.Equal(t, 404, pub(t, "c", base+path, nil).Code, path)
	}
}

func TestPublicShopTenantStatusPlanAndRateLimit(t *testing.T) {
	ctx := context.Background()
	adm := admin(t)
	cTok, _ := ownerLogin(t, "c")
	setStatus(t, "c", "trialing")
	cSlug := list(t, get(t, tenants["c"].Host, "/api/stores", cTok))[0]["slug"].(string)
	open := func() int { return pub(t, "c", "/"+cSlug, nil).Code }
	require.Equal(t, 200, open())

	// Suspended tenants have no shop; read-only ones keep it.
	setStatus(t, "c", "suspended")
	t.Cleanup(func() { setStatus(t, "c", "trialing") })
	eventually(t, func() bool { return open() == 404 }, "suspended hides the shop")
	setStatus(t, "c", "read_only")
	eventually(t, func() bool { return open() == 200 }, "read_only keeps the shop")
	setStatus(t, "c", "trialing")

	// A plan without public_shop hides it.
	var trial, noShop int64
	require.NoError(t, adm.QueryRow(ctx, `SELECT plan_id FROM platform.tenants WHERE id = $1`, tenants["c"].ID).Scan(&trial))
	require.NoError(t, adm.QueryRow(ctx, `INSERT INTO platform.plans (code, name, price_minor, billing_interval, trial_days, limits, features, is_public)
		SELECT 'no_shop_test', 'No shop', price_minor, billing_interval, trial_days, limits,
			features || '{"public_shop": false}', false FROM platform.plans WHERE id = $1 RETURNING id`, trial).Scan(&noShop))
	_, err := adm.Exec(ctx, `UPDATE platform.tenants SET plan_id = $1 WHERE id = $2`, noShop, tenants["c"].ID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = adm.Exec(ctx, `UPDATE platform.tenants SET plan_id = $1 WHERE id = $2`, trial, tenants["c"].ID)
	})
	eventually(t, func() bool { return open() == 404 }, "plan without public_shop")
	_, err = adm.Exec(ctx, `UPDATE platform.tenants SET plan_id = $1 WHERE id = $2`, trial, tenants["c"].ID)
	require.NoError(t, err)
	eventually(t, func() bool { return open() == 200 }, "plan restored")

	// One visitor is limited to 120 requests a minute.
	ip := "198.51.100.77"
	codes := map[int]int{}
	for range 125 {
		codes[do(t, rq{method: "GET", host: tenants["c"].Host, path: "/api/public/shop/" + cSlug, ip: ip}).Code]++
	}
	require.Equal(t, 120, codes[200])
	require.Equal(t, 5, codes[429])
	limited := do(t, rq{method: "GET", host: tenants["c"].Host, path: "/api/public/shop/" + cSlug, ip: ip})
	require.Equal(t, 429, limited.Code)
	require.NotEmpty(t, limited.Header.Get("Retry-After"))
	require.Equal(t, 200, open(), "other visitors are not affected")
}

func TestPublicShopIsolationAcrossTenants(t *testing.T) {
	w := shopWindow(t)
	aTok, _ := ownerLogin(t, "a")
	aSlug := list(t, get(t, tenants["a"].Host, "/api/stores", aTok))[0]["slug"].(string)
	for _, s := range list(t, get(t, tenants["a"].Host, "/api/stores", aTok)) {
		require.NotEqual(t, w.slug, s["slug"], "A has no store with C's showcase slug")
	}

	// C's store slug and product IDs mean nothing on A's host.
	require.Equal(t, 404, pub(t, "a", "/"+w.slug, nil).Code)
	require.Equal(t, 404, pub(t, "a", "/"+w.slug+"/products", nil).Code)
	require.Equal(t, 404, pub(t, "a", "/"+aSlug+"/products/"+jsonID(w.plain), nil).Code)
	require.Equal(t, 404, pub(t, "a", "/"+aSlug+"/products/shop-plain-tea", nil).Code)
	for _, path := range []string{"/products", "/featured", "/categories"} {
		r := pub(t, "a", "/"+aSlug+path, nil)
		require.Equal(t, 200, r.Code, r.Raw)
		require.False(t, strings.Contains(r.Raw, "SHOP-") || strings.Contains(r.Raw, "Shop Teas") || strings.Contains(r.Raw, "Shop Cheap"), path)
	}
	// Different content on each host never shares an ETag.
	require.NotEqual(t, pub(t, "c", "/"+w.slug, nil).Header.Get("ETag"), pub(t, "a", "/"+aSlug, nil).Header.Get("ETag"))
}

// showcaseOn turns store id's public showcase on or off in tenant C.
func showcaseOn(t *testing.T, tok string, id float64, on bool) {
	t.Helper()
	r := do(t, rq{method: "PUT", host: tenants["c"].Host, path: fmt.Sprintf("/api/stores/%s/showcase", jsonID(id)), token: tok,
		body: map[string]any{"isPublicEnabled": on}})
	require.Equal(t, 200, r.Code, r.Raw)
}

func TestPublicHomePage(t *testing.T) {
	w := shopWindow(t)
	host := tenants["c"].Host
	home := func(l string) resp {
		t.Helper()
		r := do(t, rq{method: "GET", host: tenants[l].Host, path: "/api/public/home"})
		require.Equal(t, 200, r.Code, r.Raw)
		require.Equal(t, "no-cache", r.Header.Get("Cache-Control"), "a settings change shows at once")
		return r
	}
	put := func(body map[string]any) resp {
		return do(t, rq{method: "PUT", host: host, path: "/api/settings", token: w.tok, body: body})
	}
	t.Cleanup(func() { put(map[string]any{"homePage": "login", "shopStoreId": nil}) })

	// Default: the staff sign-in.
	require.Equal(t, "login", get(t, host, "/api/settings", w.tok).Body["homePage"])
	r := home("c")
	require.JSONEq(t, `{"homePage":"login"}`, r.Raw)

	// The shop with no store chosen: the store with slug "main".
	s := put(map[string]any{"homePage": "shop"})
	require.Equal(t, 200, s.Code, s.Raw)
	require.Equal(t, "shop", s.Body["homePage"])
	require.Nil(t, s.Body["shopStoreId"])
	require.JSONEq(t, `{"homePage":"shop","slug":"main"}`, home("c").Raw)

	// A chosen store; its ETag works.
	s = put(map[string]any{"shopStoreId": jsonID(w.id)})
	require.Equal(t, 200, s.Code, s.Raw)
	r = home("c")
	require.Equal(t, w.slug, r.Body["slug"])
	require.Equal(t, 304, do(t, rq{method: "GET", host: host, path: "/api/public/home", header: map[string]string{"If-None-Match": r.Header.Get("ETag")}}).Code)

	// Bad values are refused.
	require.Equal(t, 400, put(map[string]any{"homePage": "landing"}).Code)
	require.Equal(t, 400, put(map[string]any{"shopStoreId": 999999}).Code)

	// The chosen store turned off: the next active public store ("main").
	showcaseOn(t, w.tok, w.id, false)
	t.Cleanup(func() { showcaseOn(t, w.tok, w.id, true) })
	require.JSONEq(t, `{"homePage":"shop","slug":"main"}`, home("c").Raw)
	showcaseOn(t, w.tok, w.id, true)
	require.Equal(t, w.slug, home("c").Body["slug"], "back on: the chosen store again")

	// No active public store left: the sign-in.
	ids := storeIDs(t, "c", w.tok)
	for _, id := range ids {
		showcaseOn(t, w.tok, id, false)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			showcaseOn(t, w.tok, id, true)
		}
	})
	require.JSONEq(t, `{"homePage":"login"}`, home("c").Raw)
	for _, id := range ids {
		showcaseOn(t, w.tok, id, true)
	}
	require.Equal(t, w.slug, home("c").Body["slug"])

	// Tenant suspended: the sign-in.
	setStatus(t, "c", "suspended")
	t.Cleanup(func() { setStatus(t, "c", "trialing") })
	eventually(t, func() bool { return home("c").Body["homePage"] == "login" }, "suspended shows the sign-in")
	setStatus(t, "c", "trialing")
	eventually(t, func() bool { return home("c").Body["homePage"] == "shop" }, "active again")

	// Other tenants are not affected.
	require.JSONEq(t, `{"homePage":"login"}`, home("a").Raw)
}

func TestPublicShopsAndDefaultStore(t *testing.T) {
	w := shopWindow(t)
	host := tenants["c"].Host
	shops := func(l string) (int, []string) {
		t.Helper()
		r := do(t, rq{method: "GET", host: tenants[l].Host, path: "/api/public/shops"})
		if r.Code != 200 {
			return r.Code, nil
		}
		require.Equal(t, "no-cache", r.Header.Get("Cache-Control"))
		var slugs []string
		for _, x := range r.Body["stores"].([]any) {
			m := x.(map[string]any)
			require.ElementsMatch(t, []string{"name", "slug"}, keys(m), "only the name and slug are public")
			slugs = append(slugs, m["slug"].(string))
		}
		return 200, slugs
	}
	showcase := func(id float64) resp {
		t.Helper()
		r := get(t, host, fmt.Sprintf("/api/stores/%s/showcase", jsonID(id)), w.tok)
		require.Equal(t, 200, r.Code, r.Raw)
		return r
	}
	setDefault := func(tok string, id any) resp {
		return do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/stores/%v/showcase/default", id), token: tok})
	}
	t.Cleanup(func() {
		do(t, rq{method: "PUT", host: host, path: "/api/settings", token: w.tok, body: map[string]any{"homePage": "login", "shopStoreId": nil}})
	})
	mainID := storeIDs(t, "c", w.tok)[0]

	// The switcher lists C's public stores only.
	code, slugs := shops("c")
	require.Equal(t, 200, code)
	require.Contains(t, slugs, "main")
	require.Contains(t, slugs, w.slug)
	_, aSlugs := shops("a")
	require.NotContains(t, aSlugs, w.slug)
	showcaseOn(t, w.tok, w.id, false)
	t.Cleanup(func() { showcaseOn(t, w.tok, w.id, true) })
	_, slugs = shops("c")
	require.NotContains(t, slugs, w.slug, "turned off: not listed")
	require.Equal(t, 400, setDefault(w.tok, jsonID(w.id)).Code, "a store with its showcase off can't be the default")
	showcaseOn(t, w.tok, w.id, true)

	// Nothing chosen: "main" is the default.
	require.Equal(t, true, showcase(mainID).Body["isDefault"])
	require.Equal(t, false, showcase(w.id).Body["isDefault"])
	require.Equal(t, "login", showcase(w.id).Body["homePage"])

	// Set as default: stored in the settings and audited.
	r := setDefault(w.tok, jsonID(w.id))
	require.Equal(t, 200, r.Code, r.Raw)
	require.Equal(t, w.id, get(t, host, "/api/settings", w.tok).Body["shopStoreId"])
	require.Equal(t, true, showcase(w.id).Body["isDefault"])
	require.Equal(t, false, showcase(mainID).Body["isDefault"])
	require.Equal(t, 200, setDefault(w.tok, jsonID(w.id)).Code, "setting it again is fine")
	var n int
	require.NoError(t, admin(t).QueryRow(context.Background(), `SELECT count(*) FROM `+schema("c")+`.audit_log
		WHERE action = 'settings.update' AND after->>'shopStoreId' = $1`, jsonID(w.id)).Scan(&n))
	require.Equal(t, 1, n, "one audit entry; the repeat changed nothing")

	// Its showcase turned off: the default falls back to "main".
	showcaseOn(t, w.tok, w.id, false)
	require.Equal(t, true, showcase(mainID).Body["isDefault"])
	showcaseOn(t, w.tok, w.id, true)

	// Unknown store, no permission, other tenant.
	require.Equal(t, 404, setDefault(w.tok, 999999).Code)
	createUser(t, "c", w.tok, "biller-showcase@iso-c.test", "biller", "biller-showcase-pass-1")
	btok, _ := login(t, "c", "biller-showcase@iso-c.test", "biller-showcase-pass-1")
	require.Equal(t, 403, setDefault(btok, jsonID(w.id)).Code)
	aTok, _ := ownerLogin(t, "a")
	require.Equal(t, 401, do(t, rq{method: "POST", host: host, path: fmt.Sprintf("/api/stores/%s/showcase/default", jsonID(w.id)), token: aTok}).Code)

	// Suspended tenants have no switcher either.
	setStatus(t, "c", "suspended")
	t.Cleanup(func() { setStatus(t, "c", "trialing") })
	eventually(t, func() bool { c, _ := shops("c"); return c == 404 }, "suspended hides the switcher")
	setStatus(t, "c", "trialing")
	eventually(t, func() bool { c, _ := shops("c"); return c == 200 }, "active again")

	// Unknown API paths stay a JSON 404, never the app or a redirect.
	for _, p := range []string{"/api/public/nope", "/api/shop/main", "/api"} {
		r := do(t, rq{method: "GET", host: host, path: p})
		require.Equal(t, 404, r.Code, p)
		require.Equal(t, "NOT_FOUND", r.Body["code"], p)
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
