// Command seed fills a test shop with sample data through the shop's own
// API, as its owner: categories, brands, units, suppliers, customers,
// products, purchases (stock), sales over the last 30 days and a few
// returns. It never writes to the database directly, so every rule of the
// app (GST, MRP, batches, stock) applies.
//
// It is safe to run again: what already exists (by name, SKU, purchase
// reference or sale note) is skipped.
//
//	SEED_PASSWORD=... go run ./tools/seed -kind retail \
//	    -url https://test-retail.localhost:8443 -email owner@retail.test
//
// Dev and test shops only.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

func main() {
	kind := flag.String("kind", "", "retail, clothing or pharmacy")
	base := flag.String("url", "", "the shop's address, e.g. https://test-retail.localhost:8443")
	email := flag.String("email", "", "the owner's email")
	insecure := flag.Bool("insecure", false, "accept a self-signed certificate (local Caddy); on by default for *.localhost")
	flag.Parse()
	pw := os.Getenv("SEED_PASSWORD")
	if *base == "" || *email == "" || pw == "" {
		fmt.Fprintln(os.Stderr, "usage: SEED_PASSWORD=... seed -kind retail|clothing|pharmacy -url https://<shop> -email <owner>")
		os.Exit(2)
	}
	var data shopData
	switch *kind {
	case "retail":
		data = retailData()
	case "clothing":
		data = clothingData()
	case "pharmacy":
		data = pharmacyData()
	default:
		fmt.Fprintln(os.Stderr, "-kind must be retail, clothing or pharmacy")
		os.Exit(2)
	}
	u, err := url.Parse(strings.TrimRight(*base, "/"))
	if err != nil || u.Host == "" {
		fmt.Fprintln(os.Stderr, "bad -url")
		os.Exit(2)
	}
	local := strings.HasSuffix(u.Hostname(), ".localhost") || u.Hostname() == "localhost"
	c := newClient(u.String(), *email, pw, *insecure || local, local)
	s := &seeder{c: c, d: data, kind: *kind}
	if err := s.run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed failed:", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "done.", s.summary())
}

// ------------------------------------------------------------------ HTTP

type client struct {
	base, email, pw string
	http            *http.Client
	token           string
}

func newClient(base, email, pw string, insecure, local bool) *client {
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure}} //nolint:gosec // dev shops behind local Caddy
	if local {
		// *.localhost is the loopback address; Go's resolver does not know.
		d := &net.Dialer{Timeout: 10 * time.Second}
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, _ := net.SplitHostPort(addr)
			return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
		}
	}
	return &client{base: base, email: email, pw: pw, http: &http.Client{Transport: tr, Timeout: 60 * time.Second}}
}

type resp struct {
	code int
	raw  string
	body any
}

func (r resp) obj() map[string]any { m, _ := r.body.(map[string]any); return m }
func (r resp) list() []any {
	if l, ok := r.body.([]any); ok {
		return l
	}
	if l, ok := r.obj()["data"].([]any); ok {
		return l
	}
	return nil
}

func (c *client) login() error {
	r, err := c.send("POST", "/api/auth/login", map[string]any{"email": c.email, "password": c.pw}, nil, false)
	if err != nil {
		return err
	}
	if r.code != 200 {
		return fmt.Errorf("login: %d %s", r.code, r.raw)
	}
	c.token, _ = r.obj()["token"].(string)
	return nil
}

func (c *client) send(method, path string, body any, hdr map[string]string, auth bool) (resp, error) {
	var b io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		b = bytes.NewReader(j)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.base+path, b)
	if err != nil {
		return resp{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return resp{}, err
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	out := resp{code: res.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out, nil
}

// do calls the API as the owner, signing in again when the token expires
// and waiting when rate limited.
func (c *client) do(method, path string, body any, hdr map[string]string) resp {
	for try := 0; ; try++ {
		if c.token == "" {
			if err := c.login(); err != nil {
				fail("%v", err)
			}
		}
		r, err := c.send(method, path, body, hdr, true)
		if err != nil {
			fail("%s %s: %v", method, path, err)
		}
		if r.code == 401 && try < 2 {
			c.token = ""
			continue
		}
		if r.code == 429 && try < 6 {
			fmt.Fprintln(os.Stdout, "  rate limited, waiting 20s ...")
			time.Sleep(20 * time.Second)
			continue
		}
		return r
	}
}

func (c *client) all(path string) []any {
	var out []any
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	for page := 1; page < 200; page++ {
		r := c.do("GET", fmt.Sprintf("%s%spage=%d&limit=100", path, sep, page), nil, nil)
		if r.code != 200 {
			fail("GET %s: %d %s", path, r.code, r.raw)
		}
		l := r.list()
		out = append(out, l...)
		if _, paged := r.obj()["pagination"]; !paged || len(l) < 100 {
			break
		}
	}
	return out
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "seed failed: "+f+"\n", a...)
	os.Exit(1)
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		var f float64
		_, _ = fmt.Sscanf(x, "%g", &f)
		return f
	}
	return 0
}

func str(v any) string { s, _ := v.(string); return s }

// ------------------------------------------------------------------ data

type product struct {
	name, cat, brand, unit, hsn string
	tax                         float64
	price, mrp                  float64 // mrp 0: none (loose goods)
	loose                       bool    // sold by weight: fractions allowed
	untracked                   bool    // pharmacy: no batches (sundries)
	// clothing: a variable product over size × colour
	article string
	sizes   []string
	colours []string
	attrs   map[string]string
	// purchase quantity per variant (0: the default for the shop)
	buy float64
}

type shopData struct {
	categories, brands []string
	units              [][2]string // name, short name
	suppliers          []party
	customers          []party
	products           []product
	sales              int // how many sales over the last 30 days
	alert              float64
}

type party struct{ name, phone, address, city string }

// ------------------------------------------------------------------ seeder

type variant struct {
	pid, vid  float64
	sku       string
	price     float64
	loose     bool
	tracked   bool
	p         *product
	attrs     map[string]string
	available float64
}

type seeder struct {
	c     *client
	d     shopData
	kind  string
	store float64
	ids   map[string]map[string]float64 // kind → name → id
	vars  []*variant
	made  map[string]int
}

func (s *seeder) summary() string {
	keys := make([]string, 0, len(s.made))
	for k := range s.made {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s +%d", k, s.made[k]))
	}
	if len(parts) == 0 {
		return "Nothing new: everything was already there."
	}
	return "Added: " + strings.Join(parts, ", ")
}

func (s *seeder) run() error {
	s.ids = map[string]map[string]float64{}
	s.made = map[string]int{}
	stores := s.c.do("GET", "/api/stores", nil, nil).list()
	if len(stores) == 0 {
		return fmt.Errorf("the shop has no store")
	}
	s.store = num(stores[0].(map[string]any)["id"])
	fmt.Fprintf(os.Stdout, "store %v: %s\n", s.store, str(stores[0].(map[string]any)["name"]))

	s.named("categories", "/api/categories", s.d.categories, func(n string) any { return map[string]any{"name": n, "status": "Active"} })
	s.named("brands", "/api/brands", s.d.brands, func(n string) any { return map[string]any{"name": n, "status": "Active"} })
	unitNames := []string{}
	short := map[string]string{}
	for _, u := range s.d.units {
		unitNames = append(unitNames, u[0])
		short[u[0]] = u[1]
	}
	s.named("units", "/api/units", unitNames, func(n string) any { return map[string]any{"name": n, "short_name": short[n], "status": "Active"} })
	names := func(ps []party) []string {
		out := []string{}
		for _, p := range ps {
			out = append(out, p.name)
		}
		return out
	}
	byName := func(ps []party, n string) party {
		for _, p := range ps {
			if p.name == n {
				return p
			}
		}
		return party{}
	}
	s.named("suppliers", "/api/suppliers", names(s.d.suppliers), func(n string) any {
		p := byName(s.d.suppliers, n)
		return map[string]any{"name": n, "phone": p.phone, "address": p.address + ", " + p.city, "status": "Active"}
	})
	s.named("customers", "/api/customers", names(s.d.customers), func(n string) any {
		p := byName(s.d.customers, n)
		return map[string]any{"name": n, "phone": p.phone, "address": p.address, "city": p.city, "country": "India", "status": "Active"}
	})
	s.products()
	s.purchases()
	if s.kind == "pharmacy" {
		s.expireOne()
	}
	s.sales()
	return nil
}

// expireOne moves one seed batch that expires soon to yesterday, so the
// shop has expired stock to see (receiving refuses expired batches). The
// batch edit is the owner's correction, with a reason, as in the app.
func (s *seeder) expireOne() {
	r := s.c.do("GET", "/api/batches?limit=100", nil, nil)
	items, _ := r.obj()["items"].([]any)
	var pick map[string]any
	for _, it := range items {
		b := it.(map[string]any)
		if !strings.HasPrefix(str(b["sku"]), "SD-PH-") {
			continue
		}
		if b["expired"] == true {
			return // done by an earlier run
		}
		if d := num(b["daysToExpiry"]); d > 0 && d <= 90 && num(b["totalQty"]) > 0 && (pick == nil || d < num(pick["daysToExpiry"])) {
			pick = b
		}
	}
	if pick == nil {
		return
	}
	u := s.c.do("PUT", fmt.Sprintf("/api/batches/%v", pick["id"]), map[string]any{"expiry": day(-1), "reason": "Sample expired stock for testing"}, nil)
	if u.code != 200 {
		fmt.Fprintf(os.Stdout, "  expired sample skipped: %d %s\n", u.code, u.raw)
		return
	}
	s.made["expired batches"]++
	fmt.Fprintf(os.Stdout, "  expired yesterday: %s batch %s\n", str(pick["productName"]), str(pick["batchNo"]))
}

// named makes sure each name exists in a simple list (categories, ...).
func (s *seeder) named(kind, path string, want []string, body func(string) any) {
	have := map[string]float64{}
	for _, it := range s.c.do("GET", path, nil, nil).list() {
		m := it.(map[string]any)
		have[strings.ToLower(str(m["name"]))] = num(m["id"])
	}
	s.ids[kind] = map[string]float64{}
	for _, n := range want {
		if id, ok := have[strings.ToLower(n)]; ok {
			s.ids[kind][n] = id
			continue
		}
		r := s.c.do("POST", path, body(n), nil)
		if r.code != 201 && r.code != 200 {
			fail("create %s %q: %d %s", kind, n, r.code, r.raw)
		}
		s.ids[kind][n] = num(r.obj()["id"])
		s.made[kind]++
	}
	fmt.Fprintf(os.Stdout, "%s: %d\n", kind, len(want))
}

func slugPart(v string) string {
	v = strings.ToUpper(strings.NewReplacer(" ", "", "/", "", "'", "", ".", "").Replace(v))
	return v
}

// sku builds a short stable SKU for a single product.
func sku(prefix string, i int) string { return fmt.Sprintf("%s-%03d", prefix, i+1) }

// ean13 is a valid EAN-13 in the 890 (India) range.
func ean13(shop, i int) string {
	d := fmt.Sprintf("890%d%08d", shop, 10000+i)
	sum := 0
	for k, ch := range d {
		n := int(ch - '0')
		if k%2 == 1 {
			n *= 3
		}
		sum += n
	}
	return fmt.Sprintf("%s%d", d, (10-sum%10)%10)
}

func (s *seeder) prefix() (string, int) {
	switch s.kind {
	case "retail":
		return "SD-RT", 1
	case "clothing":
		return "SD-CL", 2
	}
	return "SD-PH", 3
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

func (s *seeder) products() {
	existing := map[string]map[string]any{} // sku → variant (with product id)
	for _, it := range s.c.all("/api/products") {
		p := it.(map[string]any)
		for _, v := range p["ProductVariants"].([]any) {
			vm := v.(map[string]any)
			vm["_pid"] = p["id"]
			vm["_tracks"] = p["tracksBatches"]
			existing[strings.ToLower(str(vm["sku"]))] = vm
		}
	}
	pre, shopNo := s.prefix()
	for i := range s.d.products {
		p := &s.d.products[i]
		type vdef struct {
			sku   string
			attrs map[string]string
		}
		var defs []vdef
		if p.article != "" {
			colours := p.colours
			if len(colours) == 0 {
				colours = []string{""}
			}
			sizes := p.sizes
			if len(sizes) == 0 {
				sizes = []string{""}
			}
			for _, sz := range sizes {
				for _, co := range colours {
					a := map[string]string{}
					parts := []string{pre, p.article}
					if sz != "" {
						a["size"] = sz
						parts = append(parts, slugPart(sz))
					}
					if co != "" {
						a["colour"] = co
						parts = append(parts, slugPart(co))
					}
					defs = append(defs, vdef{strings.Join(parts, "-"), a})
				}
			}
		} else {
			defs = []vdef{{sku(pre, i), nil}}
		}
		if v, ok := existing[strings.ToLower(defs[0].sku)]; ok {
			// Already made: pick up its variants.
			for _, d := range defs {
				ev, ok := existing[strings.ToLower(d.sku)]
				if !ok {
					continue
				}
				s.vars = append(s.vars, &variant{pid: num(v["_pid"]), vid: num(ev["id"]), sku: d.sku, price: p.price,
					loose: p.loose, tracked: ev["_tracks"] == true, p: p, attrs: d.attrs})
			}
			continue
		}
		variants := []map[string]any{}
		for k, d := range defs {
			v := map[string]any{"sku": d.sku, "price": p.price, "cost": round2(p.price * 0.78), "weight": 0,
				"itemBarcode": ean13(shopNo, i*20+k),
				"inventories": []map[string]any{{"storeId": s.store, "qty": 0, "quantityAlert": s.d.alert}}}
			if p.mrp > 0 {
				v["mrp"] = p.mrp
			}
			attrs := map[string]any{}
			for ak, av := range d.attrs {
				attrs[ak] = av
			}
			v["attributes"] = attrs
			variants = append(variants, v)
		}
		ptype := "single"
		if p.article != "" {
			ptype = "variable"
		}
		body := map[string]any{"name": p.name, "productType": ptype, "taxType": "Inclusive", "tax": p.tax,
			"sellingType": "POS", "status": "Active", "variants": variants}
		if p.hsn != "" {
			body["hsnCode"] = p.hsn
		}
		if id, ok := s.ids["categories"][p.cat]; ok {
			body["categoryId"] = id
		}
		if id, ok := s.ids["brands"][p.brand]; ok {
			body["brandId"] = id
		}
		if id, ok := s.ids["units"][p.unit]; ok && p.unit != "" {
			body["unitId"] = id
		}
		if p.loose {
			body["allowFraction"] = true
		}
		if s.kind == "pharmacy" {
			body["tracksBatches"] = !p.untracked
		}
		if p.article != "" {
			body["articleNo"] = p.article
			dims := []string{}
			if len(p.sizes) > 0 {
				dims = append(dims, "size")
			}
			if len(p.colours) > 0 {
				dims = append(dims, "colour")
			}
			body["variantDims"] = dims
			a := map[string]any{}
			for k, v := range p.attrs {
				a[k] = v
			}
			body["attributes"] = a
		}
		r := s.c.do("POST", "/api/products", body, nil)
		if r.code != 201 {
			fail("create product %q: %d %s", p.name, r.code, r.raw)
		}
		s.made["products"]++
		pid := num(r.obj()["id"])
		bySKU := map[string]float64{}
		for _, v := range r.obj()["ProductVariants"].([]any) {
			vm := v.(map[string]any)
			bySKU[strings.ToLower(str(vm["sku"]))] = num(vm["id"])
		}
		tracked := r.obj()["tracksBatches"] == true
		for _, d := range defs {
			s.vars = append(s.vars, &variant{pid: pid, vid: bySKU[strings.ToLower(d.sku)], sku: d.sku, price: p.price,
				loose: p.loose, tracked: tracked, p: p, attrs: d.attrs})
		}
	}
	fmt.Fprintf(os.Stdout, "products: %d (%d items to sell)\n", len(s.d.products), len(s.vars))
}

// ------------------------------------------------------------- purchases

func day(offset int) string { return time.Now().AddDate(0, 0, offset).Format("2006-01-02") }

func (s *seeder) purchases() {
	have := map[string]map[string]any{}
	for _, it := range s.c.all("/api/purchases") {
		m := it.(map[string]any)
		have[str(m["reference"])] = m
	}
	rng := rand.New(rand.NewPCG(7, uint64(len(s.kind)))) //nolint:gosec // repeatable sample data, not security
	// Each supplier gets a share of the products; three deliveries.
	nSup := len(s.d.suppliers)
	groups := make([][]*variant, nSup)
	for i, v := range s.vars {
		groups[i%nSup] = append(groups[i%nSup], v)
	}
	for g, vs := range groups {
		if len(vs) == 0 {
			continue
		}
		sup := s.d.suppliers[g]
		ref := fmt.Sprintf("SEED-%s-P%02d", strings.ToUpper(s.kind[:2]), g+1)
		qtys := map[*variant]float64{}
		lines := []map[string]any{}
		for _, v := range vs {
			q := v.p.buy
			if q == 0 {
				switch {
				case s.kind == "clothing":
					q = float64(4 + rng.IntN(7))
				case v.loose:
					q = float64(20 + 5*rng.IntN(6))
				default:
					q = float64(18 + rng.IntN(30))
				}
			}
			qtys[v] = q
			lines = append(lines, map[string]any{"productId": v.pid, "variantId": v.vid, "storeId": s.store,
				"quantity": q, "unitPrice": round2(v.price * 0.78)})
		}
		if old, ok := have[ref]; ok {
			s.finishPurchase(old, vs, qtys)
			continue
		}
		status := "Received"
		if s.kind == "pharmacy" {
			status = "Ordered" // batches come with the receipt
		}
		pay, paid := []string{"Paid", "Partial", "Unpaid"}[g%3], 0.0
		total := 0.0
		for _, l := range lines {
			total += num(l["quantity"]) * num(l["unitPrice"])
		}
		switch pay {
		case "Paid":
			paid = round2(total)
		case "Partial":
			paid = math.Round(total / 2)
		}
		body := map[string]any{"reference": ref, "date": day(-40 + 12*g), "status": status, "payment_status": pay,
			"paid": paid, "supplierId": s.ids["suppliers"][sup.name], "note": "Seed data", "purchaseItems": lines}
		r := s.c.do("POST", "/api/purchases", body, map[string]string{"Idempotency-Key": "seed-" + strings.ToLower(ref)})
		if r.code != 201 {
			fail("purchase %s: %d %s", ref, r.code, r.raw)
		}
		s.made["purchases"]++
		if status == "Received" {
			for _, v := range vs {
				v.available = qtys[v]
			}
			continue
		}
		s.receive(r.obj(), vs, qtys, rng)
	}
	fmt.Fprintf(os.Stdout, "purchases: %d\n", len(groups))
}

// finishPurchase picks up a purchase made by an earlier run: its stock,
// and for pharmacy the batches if they were never received.
func (s *seeder) finishPurchase(old map[string]any, vs []*variant, qtys map[*variant]float64) {
	if str(old["status"]) != "Received" && s.kind == "pharmacy" {
		r := s.c.do("GET", fmt.Sprintf("/api/purchases/%v", old["id"]), nil, nil)
		s.receive(r.obj(), vs, qtys, rand.New(rand.NewPCG(9, 9))) //nolint:gosec // repeatable sample data
	}
	for _, v := range vs {
		v.available = s.stock(v)
	}
}

func (s *seeder) stock(v *variant) float64 {
	r := s.c.do("GET", fmt.Sprintf("/api/products/%v", v.pid), nil, nil)
	for _, it := range r.obj()["ProductVariants"].([]any) {
		vm := it.(map[string]any)
		if num(vm["id"]) != v.vid {
			continue
		}
		for _, inv := range vm["Inventories"].([]any) {
			im := inv.(map[string]any)
			if num(im["store_id"]) == s.store {
				return num(im["qty"])
			}
		}
	}
	return 0
}

// receive takes in a pharmacy delivery with batch, expiry and MRP. A few
// batches expire soon, so the expiry screens have something to show.
func (s *seeder) receive(pur map[string]any, vs []*variant, qtys map[*variant]float64, rng *rand.Rand) {
	byVar := map[float64]*variant{}
	for _, v := range vs {
		byVar[v.vid] = v
	}
	lines := []map[string]any{}
	near := []string{}
	for _, it := range pur["items"].([]any) {
		im := it.(map[string]any)
		v := byVar[num(im["variantId"])]
		if v == nil {
			v = byVar[num(im["variant_id"])]
		}
		if v == nil {
			continue
		}
		open := num(im["quantity"]) - num(im["receivedQty"])
		if open <= 0 {
			continue
		}
		l := map[string]any{"purchaseItemId": im["id"], "quantity": open}
		if v.tracked {
			code := strings.ReplaceAll(v.sku, "SD-PH-", "")
			b1 := math.Ceil(open * 0.4)
			exp1 := day(240 + rng.IntN(300))
			if rng.IntN(5) == 0 { // about one in five: expires within 90 days
				exp1 = day(25 + rng.IntN(50))
				near = append(near, v.p.name)
			}
			l["batches"] = []map[string]any{
				{"batchNo": "B" + code + "A", "expiry": exp1, "mfgDate": day(-200), "mrp": v.p.mrp, "quantity": b1},
				{"batchNo": "B" + code + "B", "expiry": day(420 + rng.IntN(300)), "mfgDate": day(-60), "mrp": v.p.mrp, "quantity": open - b1},
			}
		}
		lines = append(lines, l)
		v.available += open
	}
	if len(lines) == 0 {
		return
	}
	r := s.c.do("POST", fmt.Sprintf("/api/purchases/%v/receipts", pur["id"]),
		map[string]any{"lines": lines, "acceptNearExpiry": true, "note": "Seed data"},
		map[string]string{"Idempotency-Key": fmt.Sprintf("seed-receipt-%v", pur["id"])})
	if r.code != 201 {
		fail("receive purchase %v: %d %s", pur["id"], r.code, r.raw)
	}
	if len(near) > 0 {
		fmt.Fprintf(os.Stdout, "  near expiry: %s\n", strings.Join(near, ", "))
	}
}

// ------------------------------------------------------------------ sales

type plannedSale struct {
	n        int
	at       time.Time
	lines    []map[string]any
	customer float64
	method   string
}

func (s *seeder) sales() {
	doneNotes := map[string]bool{}
	for _, it := range s.c.all("/api/sales") {
		m := it.(map[string]any)
		if n := str(m["note"]); strings.HasPrefix(n, "Seed sale") {
			doneNotes[n] = true
		}
	}
	custIDs := []float64{}
	for _, c := range s.d.customers {
		custIDs = append(custIDs, s.ids["customers"][c.name])
	}
	rng := rand.New(rand.NewPCG(42, uint64(len(s.kind)))) //nolint:gosec // repeatable sample data, not security
	loc, _ := time.LoadLocation("Asia/Kolkata")
	if loc == nil {
		loc = time.FixedZone("IST", 5*3600+1800)
	}
	now := time.Now().In(loc)
	var plan []plannedSale
	for n := 1; n <= s.d.sales; n++ {
		// Spread from 30 days ago to today, opening hours 9:00-20:59.
		d := 30 - (n-1)*31/s.d.sales
		at := time.Date(now.Year(), now.Month(), now.Day(), 9+rng.IntN(12), rng.IntN(60), 0, 0, loc).AddDate(0, 0, -d)
		if at.After(now) {
			at = now.Add(-time.Duration(s.d.sales-n+1) * time.Minute)
		}
		ps := plannedSale{n: n, at: at, method: []string{"cash", "cash", "upi", "upi", "card"}[rng.IntN(5)]}
		if rng.IntN(10) < 4 && len(custIDs) > 0 {
			ps.customer = custIDs[rng.IntN(len(custIDs))]
		}
		seen := map[*variant]bool{}
		for k, want := 0, 1+rng.IntN(4); k < want; k++ {
			v := s.vars[rng.IntN(len(s.vars))]
			q := float64(1 + rng.IntN(3))
			if v.loose {
				q = []float64{0.5, 1, 1.5, 2, 0.25}[rng.IntN(5)]
			}
			if seen[v] || v.available < q+2 { // keep a little stock
				continue
			}
			seen[v] = true
			v.available -= q
			ps.lines = append(ps.lines, map[string]any{"productId": v.pid, "variantId": v.vid, "quantity": q, "unitPrice": v.price})
		}
		if len(ps.lines) > 0 {
			plan = append(plan, ps)
		}
	}
	if len(doneNotes) > 0 {
		fmt.Fprintf(os.Stdout, "sales: already seeded (%d)\n", len(doneNotes))
	} else {
		for _, ps := range plan {
			s.sell(ps)
		}
		fmt.Fprintf(os.Stdout, "sales: %d\n", len(plan))
	}
	s.returns()
}

func (s *seeder) sell(ps plannedSale) {
	note := fmt.Sprintf("Seed sale %02d", ps.n)
	body := map[string]any{"storeId": s.store, "orderItems": ps.lines}
	if ps.customer > 0 {
		body["customerId"] = ps.customer
	}
	q := s.c.do("POST", "/api/sales/quote", body, nil)
	if q.code != 200 {
		fail("quote for %s: %d %s", note, q.code, q.raw)
	}
	total := num(q.obj()["total"])
	pay := map[string]any{"method": ps.method, "amount": total}
	if ps.method == "cash" {
		pay["tendered"] = math.Ceil(total/50) * 50
	} else {
		pay["reference"] = fmt.Sprintf("SEED%06d", 100000+ps.n*37)
	}
	body["payments"] = []any{pay}
	body["date"] = ps.at.Format(time.RFC3339)
	body["note"] = note
	r := s.c.do("POST", "/api/sales", body, map[string]string{"Idempotency-Key": fmt.Sprintf("seed-%s-sale-%02d", s.kind, ps.n)})
	if r.code != 201 {
		fmt.Fprintf(os.Stdout, "  %s skipped: %d %s\n", note, r.code, r.raw)
		return
	}
	s.made["sales"]++
}

// returns makes a refund or two from recent seed sales and, for clothing,
// a size exchange. It reads the sales back from the shop, so a second run
// finds the same ones.
func (s *seeder) returns() {
	done := map[string]bool{}
	for _, it := range s.c.all("/api/sales/returns") {
		if n := str(it.(map[string]any)["note"]); strings.HasPrefix(n, "Seed ") {
			done[n] = true
		}
	}
	type sold struct {
		id   float64
		note string
	}
	var recent []sold // newest first
	for _, it := range s.c.all("/api/sales") {
		m := it.(map[string]any)
		at, err := time.Parse(time.RFC3339, str(m["date"]))
		if err != nil || !strings.HasPrefix(str(m["note"]), "Seed sale") {
			continue
		}
		if age := time.Since(at); age > time.Hour && age < 6*24*time.Hour {
			recent = append(recent, sold{num(m["id"]), str(m["note"])})
		}
	}
	sort.Slice(recent, func(i, j int) bool { return recent[i].note > recent[j].note })
	items := func(id float64) []map[string]any {
		r := s.c.do("GET", fmt.Sprintf("/api/sales/%v", id), nil, nil)
		var out []map[string]any
		for _, x := range r.obj()["items"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	nReturns := 2
	if s.kind != "retail" {
		nReturns = 1
	}
	reason := map[string][2]string{"retail": {"Pack was damaged", "damaged"}, "clothing": {"Stitching came loose", "defective"},
		"pharmacy": {"Doctor changed the medicine", "resellable"}}[s.kind]
	for n := 1; n <= nReturns && n <= len(recent); n++ {
		note := fmt.Sprintf("Seed return %02d", n)
		if done[note] {
			continue
		}
		sale := recent[n-1]
		it := items(sale.id)[0]
		q := math.Min(1, num(it["quantity"]))
		body := map[string]any{"saleId": sale.id, "note": note,
			"returnItems": []map[string]any{{"orderItemId": it["id"], "quantity": q, "reason": reason[0], "condition": reason[1]}}}
		r := s.c.do("POST", "/api/sales/returns", body, map[string]string{"Idempotency-Key": fmt.Sprintf("seed-%s-return-%d", s.kind, n)})
		if r.code != 201 {
			fmt.Fprintf(os.Stdout, "  %s skipped: %d %s\n", note, r.code, r.raw)
			continue
		}
		s.made["returns"]++
	}
	if s.kind != "clothing" || done["Seed exchange 01"] || len(recent) <= nReturns {
		return
	}
	// Size exchange: a customer swaps a garment for the next size up.
	byVid := map[float64]*variant{}
	for _, v := range s.vars {
		byVid[v.vid] = v
	}
	for _, sale := range recent[nReturns:] {
		for _, it := range items(sale.id) {
			v := byVid[num(it["variant_id"])]
			if v == nil {
				continue
			}
			next := s.otherSize(v)
			if next == nil {
				continue
			}
			body := map[string]any{"saleId": sale.id, "note": "Seed exchange 01", "settlement": "refund",
				"policyOverrideReason": "Sample exchange for testing",
				"returnLines":          []map[string]any{{"orderItemId": it["id"], "quantity": 1, "reason": "Size too small", "condition": "resellable"}},
				"newLines":             []map[string]any{{"productId": next.pid, "variantId": next.vid, "quantity": 1, "unitPrice": next.price}}}
			r := s.c.do("POST", "/api/sales/exchanges", body, map[string]string{"Idempotency-Key": "seed-clothing-exchange-1"})
			if r.code != 201 {
				fmt.Fprintf(os.Stdout, "  Seed exchange 01 skipped: %d %s\n", r.code, r.raw)
			} else {
				s.made["exchanges"]++
			}
			return
		}
	}
}

func (s *seeder) otherSize(v *variant) *variant {
	if v.attrs["size"] == "" {
		return nil
	}
	sizes := v.p.sizes
	for i, sz := range sizes {
		if sz != v.attrs["size"] || i+1 >= len(sizes) {
			continue
		}
		for _, o := range s.vars {
			if o.p == v.p && o.attrs["size"] == sizes[i+1] && o.attrs["colour"] == v.attrs["colour"] {
				return o
			}
		}
	}
	return nil
}
