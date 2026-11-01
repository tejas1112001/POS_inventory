//go:build integration && load

// Package load measures the hot paths of PHASE1_DESIGN §9 (p95 targets at
// 50 tenants, 1 node, 4 vCPU) against the real app and Postgres 18 in a
// container, and checks stock under concurrent checkouts. It is not part
// of CI: run it with
//
//	go test -tags integration,load -count=1 -timeout 30m -v ./test/load
//
// LOAD_TENANTS (default 50), LOAD_SKUS (default 1000) and LOAD_SECONDS
// (default 60) change the size. LOAD_THINK_MS (default 0) is the pause of
// each client between requests: 0 saturates the node (throughput), 1000
// is about one request a second per shop (the §9 latency targets). Results are printed and written to
// LOAD_OUT (default test/load/results.md).
package load

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/app"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/provisioning"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/security"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/testdb"
)

const ownerPW = "load-test-owner-pass-2026"

func env(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

// rk is a random 32-byte key, base64 (the format the config expects).
func rk() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

// suffix is a short random hex string for unique names.
func suffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type shop struct {
	id, host, token string
	ip              string // each shop calls from its own address, as real shops do
	store           float64
	storeSlug       string
	variants        []variant // the first five products
}

type variant struct {
	pid, vid float64
	price    float64
}

type client struct {
	base string
	hc   *http.Client
}

func (c client) do(method, host, path, token string, body any, hdr map[string]string) (int, []byte, time.Duration, error) {
	var r io.Reader
	if body != nil {
		switch b := body.(type) {
		case *bytes.Buffer:
			r = b
		default:
			j, _ := json.Marshal(b)
			r = bytes.NewReader(j)
		}
	}
	req, err := http.NewRequest(method, c.base+path, r)
	if err != nil {
		return 0, nil, 0, err
	}
	req.Host = host
	req.Header.Set("X-Requested-With", "fetch")
	req.Header.Set("Origin", "http://"+host)
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	start := time.Now()
	res, err := c.hc.Do(req)
	if err != nil {
		return 0, nil, time.Since(start), err
	}
	data, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	return res.StatusCode, data, time.Since(start), nil
}

// call is client.do from the shop's own address (X-Forwarded-For; the app
// trusts it from 127.0.0.1 here, as behind Caddy). The per-address login
// and request limits then apply per shop, as in production.
func (s *shop) call(c client, method, path string, body any, hdr map[string]string) (int, []byte, time.Duration, error) {
	h := map[string]string{"X-Forwarded-For": s.ip}
	for k, v := range hdr {
		h[k] = v
	}
	return c.do(method, s.host, path, s.token, body, h)
}

// stats collects latencies per scenario.
type stats struct {
	mu   sync.Mutex
	lat  map[string][]time.Duration
	errs map[string]int
}

func (s *stats) add(name string, d time.Duration, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ok {
		s.lat[name] = append(s.lat[name], d)
	} else {
		s.errs[name]++
	}
}

func pct(ds []time.Duration, p float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	i := int(float64(len(ds)-1) * p)
	return ds[i]
}

func TestLoad(t *testing.T) {
	ctx := context.Background()
	nTenants, nSKUs, seconds := env("LOAD_TENANTS", 50), env("LOAD_SKUS", 1000), env("LOAD_SECONDS", 60)
	think := time.Duration(0)
	if v, err := strconv.Atoi(os.Getenv("LOAD_THINK_MS")); err == nil && v > 0 {
		think = time.Duration(v) * time.Millisecond
	}
	runtime.GOMAXPROCS(4) // the target node has 4 vCPU (the database runs in its own container)

	tdb, err := testdb.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = tdb.Stop(ctx) }()
	files, err := os.MkdirTemp("", "posd-load-")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(files) }()
	cfg, err := config.LoadFrom(map[string]string{
		"BASE_DOMAIN": "pos.test", "PLATFORM_NAME": "Load", "APP_ENV": "test", "PUBLIC_SCHEME": "http",
		"DATABASE_URL": tdb.AppDSN.Reveal(), "DATABASE_JOBS_URL": tdb.JobsDSN.Reveal(), "DATABASE_MIGRATOR_URL": tdb.MigratorDSN.Reveal(),
		"JWT_KEYS": "k1:" + rk(), "PLATFORM_JWT_KEYS": "p1:" + rk(), "MASTER_KEYS": "m1:" + rk(),
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32", "SHUTDOWN_DRAIN_DELAY": "0s", "STORAGE_FS_ROOT": files,
	})
	require.NoError(t, err)
	a, err := app.New(ctx, cfg, slog.New(slog.DiscardHandler), app.WithTenantLoginLimit(1_000_000))
	require.NoError(t, err)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	iln, _ := net.Listen("tcp", "127.0.0.1:0")
	sctx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- a.Serve(sctx, ln, iln) }()
	defer func() { stop(); <-done }()
	for deadline := time.Now().Add(10 * time.Second); !a.ListenerHealthy() && time.Now().Before(deadline); {
		time.Sleep(50 * time.Millisecond)
	}
	c := client{base: "http://" + ln.Addr().String(), hc: &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{MaxIdleConnsPerHost: 256}}}

	adm, err := pgx.Connect(ctx, tdb.AdminDSN.Reveal())
	require.NoError(t, err)
	defer func() { _ = adm.Close(ctx) }()
	_, err = adm.Exec(ctx, `UPDATE platform.plans SET limits = jsonb_set(jsonb_set(limits, '{max_products}', '100000'), '{max_users}', '1000') WHERE code = 'trial'`)
	require.NoError(t, err)
	hash, err := security.NewHasher(security.TenantArgon2, 2).Hash(ctx, ownerPW)
	require.NoError(t, err)

	// Setup: tenants (6 at a time), owner password, 1k SKUs by CSV import.
	setupStart := time.Now()
	o := provisioning.New(tdb.MigratorDSN, "http", "pos.test", slog.New(slog.DiscardHandler))
	shops := make([]*shop, nTenants)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 6)
	var setupErr atomic.Value
	for i := range nTenants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			s, err := setupShop(ctx, c, o, adm, hash, i, nSKUs)
			if err != nil {
				setupErr.Store(err)
				return
			}
			shops[i] = s
		}()
	}
	wg.Wait()
	if e, ok := setupErr.Load().(error); ok {
		require.NoError(t, e)
	}
	setup := time.Since(setupStart)

	// Concurrency check first: 40 parallel one-item sales on 25 units.
	sold, refused, left := oversell(t, c, shops[0])

	// Warm a few receipts.
	var receipts []string
	var rmu sync.Mutex

	st := &stats{lat: map[string][]time.Duration{}, errs: map[string]int{}}
	end := time.Now().Add(time.Duration(seconds) * time.Second)
	var seq atomic.Int64
	for _, s := range shops {
		wg.Add(1)
		go func(s *shop) {
			defer wg.Done()
			for i := 0; time.Now().Before(end); i++ {
				if think > 0 && i > 0 {
					time.Sleep(think)
				}
				switch i % 5 {
				case 0: // checkout, 5 lines
					var items []map[string]any
					for _, v := range s.variants {
						items = append(items, map[string]any{"productId": v.pid, "variantId": v.vid, "quantity": 1, "unitPrice": v.price})
					}
					key := fmt.Sprintf("load-%d", seq.Add(1))
					code, body, d, err := s.call(c, "POST", "/api/sales", map[string]any{"orderItems": items, "storeId": s.store, "method": "cash", "amountTendered": 100000}, map[string]string{"Idempotency-Key": key})
					st.add("POST /api/sales (5 lines)", d, err == nil && code == 201)
					if code == 201 {
						var sale struct{ ID float64 }
						_ = json.Unmarshal(body, &sale)
						rmu.Lock()
						receipts = append(receipts, s.host+"|"+s.token+"|"+strconv.FormatFloat(sale.ID, 'f', 0, 64)+"|"+s.ip)
						rmu.Unlock()
					}
				case 1:
					code, _, d, err := s.call(c, "GET", fmt.Sprintf("/api/products/pos?storeId=%v", s.store), nil, nil)
					st.add(fmt.Sprintf("GET /api/products/pos (%d SKUs)", nSKUs), d, err == nil && code == 200)
				case 2:
					code, _, d, err := s.call(c, "GET", fmt.Sprintf("/api/products?page=%d&limit=25&search=item", 1+i%10), nil, nil)
					st.add("GET /api/products (page of 25, search)", d, err == nil && code == 200)
				case 3:
					today := time.Now().Format("2006-01-02")
					code, _, d, err := s.call(c, "GET", "/api/dashboard?startDate="+today+"&endDate="+today, nil, nil)
					st.add("GET /api/dashboard (today)", d, err == nil && code == 200)
				case 4:
					rmu.Lock()
					var pick string
					if len(receipts) > 20 {
						pick = receipts[len(receipts)-20]
					}
					rmu.Unlock()
					if pick == "" {
						continue
					}
					p := strings.SplitN(pick, "|", 4)
					code, _, d, err := c.do("GET", p[0], "/api/sales/"+p[2]+"/receipt", p[1], nil, map[string]string{"X-Forwarded-For": p[3]})
					if code == http.StatusServiceUnavailable {
						continue // still rendering: not a download
					}
					st.add("GET /api/sales/{id}/receipt (PDF download)", d, err == nil && code == 200)
				}
			}
		}(s)
	}
	wg.Wait()

	targets := map[string]time.Duration{
		"POST /api/sales (5 lines)":                           60 * time.Millisecond,
		fmt.Sprintf("GET /api/products/pos (%d SKUs)", nSKUs): 120 * time.Millisecond,
		"GET /api/products (page of 25, search)":              80 * time.Millisecond,
		"GET /api/dashboard (today)":                          150 * time.Millisecond,
		"GET /api/sales/{id}/receipt (PDF download)":          30 * time.Millisecond,
	}
	var names []string
	for n := range st.lat {
		names = append(names, n)
	}
	sort.Strings(names)
	var out strings.Builder
	var total int
	for _, ds := range st.lat {
		total += len(ds)
	}
	fmt.Fprintf(&out, "# Load test results\n\n%s · %d tenants · %d SKUs each · %d s · %d concurrent clients · pause %s between a client's requests · %.0f requests/s · app GOMAXPROCS=4 · host %d CPUs · Postgres 18 in a container on the same host · setup %s\n\n",
		time.Now().UTC().Format("2006-01-02 15:04 UTC"), nTenants, nSKUs, seconds, nTenants, think, float64(total)/float64(seconds), runtime.NumCPU(), setup.Round(time.Second))
	out.WriteString("| Scenario | Requests | Errors | p50 | p95 | p99 | Target p95 | Met |\n|---|---|---|---|---|---|---|---|\n")
	for _, n := range names {
		ds := st.lat[n]
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		p95 := pct(ds, 0.95)
		met := "yes"
		if tg := targets[n]; tg > 0 && p95 > tg {
			met = "no"
		}
		fmt.Fprintf(&out, "| %s | %d | %d | %s | %s | %s | %s | %s |\n", n, len(ds), st.errs[n],
			pct(ds, 0.5).Round(100*time.Microsecond), p95.Round(100*time.Microsecond), pct(ds, 0.99).Round(100*time.Microsecond), targets[n], met)
	}
	fmt.Fprintf(&out, "\nConcurrent checkouts on one variant with 25 in stock: 40 requests at once → %d sold, %d refused, %d left (expected 25 / 15 / 0).\n", sold, refused, left)
	fmt.Print(out.String())
	path := os.Getenv("LOAD_OUT")
	if path == "" {
		path = "results.md"
	}
	require.NoError(t, os.WriteFile(path, []byte(out.String()), 0o644))
	require.Equal(t, 25, sold)
	require.Equal(t, 0, left)
}

func setupShop(ctx context.Context, c client, o *provisioning.Orchestrator, adm *pgx.Conn, hash string, i, nSKUs int) (*shop, error) {
	slug := fmt.Sprintf("load-%03d", i)
	res, err := o.Provision(ctx, provisioning.Request{Slug: slug, Name: "Load " + slug, OwnerEmail: "owner@" + slug + ".test", OwnerName: "Owner"})
	if err != nil {
		return nil, err
	}
	schema := "t_" + res.TenantID
	if _, err := adm.Exec(ctx, `UPDATE `+schema+`.users SET password_hash = $1 WHERE email = $2`, hash, "owner@"+slug+".test"); err != nil {
		return nil, err
	}
	s := &shop{id: res.TenantID, host: slug + ".pos.test", ip: fmt.Sprintf("10.20.%d.%d", i/250, 1+i%250)}
	code, body, _, err := s.call(c, "POST", "/api/auth/login", map[string]any{"email": "owner@" + slug + ".test", "password": ownerPW}, nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("login %s: %d %s %v", slug, code, body, err)
	}
	var tok struct{ Token string }
	_ = json.Unmarshal(body, &tok)
	s.token = tok.Token
	code, body, _, _ = s.call(c, "GET", "/api/stores", nil, nil)
	var stores []struct {
		ID   float64
		Slug string
	}
	if err := json.Unmarshal(body, &stores); err != nil || len(stores) == 0 || code != 200 {
		return nil, fmt.Errorf("stores %s: %d %s", slug, code, body)
	}
	s.store, s.storeSlug = stores[0].ID, stores[0].Slug
	var csv strings.Builder
	csv.WriteString("name,sku,price,store,opening_qty\n")
	for k := range nSKUs {
		fmt.Fprintf(&csv, "Load item %04d,SKU-%04d,%d.50,%s,100000\n", k, k, 10+k%90, stores[0].Slug)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("csvFile", "products.csv")
	_, _ = fw.Write([]byte(csv.String()))
	_ = mw.Close()
	code, body, _, err = s.call(c, "POST", "/api/products/import", &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
	if err != nil || code != 200 {
		return nil, fmt.Errorf("import %s: %d %.300s %v", slug, code, body, err)
	}
	code, body, _, _ = s.call(c, "GET", fmt.Sprintf("/api/products/pos?storeId=%v", s.store), nil, nil)
	var list []struct {
		ID              float64
		ProductVariants []struct {
			ID    float64
			Price json.Number
		}
	}
	if err := json.Unmarshal(body, &list); err != nil || code != 200 || len(list) < 5 {
		return nil, fmt.Errorf("pos list %s: %d", slug, code)
	}
	for _, p := range list[:5] {
		price, _ := p.ProductVariants[0].Price.Float64()
		s.variants = append(s.variants, variant{pid: p.ID, vid: p.ProductVariants[0].ID, price: price})
	}
	return s, nil
}

// oversell sends 40 one-unit sales at once for a variant with 25 in stock.
func oversell(t *testing.T, c client, s *shop) (sold, refused, left int) {
	t.Helper()
	sku := "RACE-" + suffix()
	csv := "name,sku,price,store,opening_qty\nRace item," + sku + ",10," + s.storeSlug + ",25\n"
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("csvFile", "race.csv")
	_, _ = fw.Write([]byte(csv))
	_ = mw.Close()
	code, body, _, err := s.call(c, "POST", "/api/products/import", &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
	require.NoError(t, err)
	require.Equal(t, 200, code, string(body))
	code, body, _, _ = s.call(c, "GET", "/api/products?search="+sku, nil, nil)
	require.Equal(t, 200, code)
	var found []struct {
		ID              float64
		ProductVariants []struct{ ID float64 }
	}
	require.NoError(t, json.Unmarshal(body, &found))
	require.Len(t, found, 1)
	pid, vid := found[0].ID, found[0].ProductVariants[0].ID
	var wg sync.WaitGroup
	var ok, no atomic.Int64
	startGate := make(chan struct{})
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startGate
			code, _, _, err := s.call(c, "POST", "/api/sales", map[string]any{
				"orderItems": []map[string]any{{"productId": pid, "variantId": vid, "quantity": 1, "unitPrice": 10}},
				"storeId":    s.store, "method": "cash", "amountTendered": 10,
			}, map[string]string{"Idempotency-Key": fmt.Sprintf("race-%s-%d", sku, i)})
			if err == nil && code == 201 {
				ok.Add(1)
			} else {
				no.Add(1)
			}
		}()
	}
	close(startGate)
	wg.Wait()
	code, body, _, _ = s.call(c, "GET", fmt.Sprintf("/api/products/%v", pid), nil, nil)
	require.Equal(t, 200, code)
	var p struct {
		ProductVariants []struct {
			Inventories []struct{ Qty int }
		}
	}
	require.NoError(t, json.Unmarshal(body, &p))
	return int(ok.Load()), int(no.Load()), p.ProductVariants[0].Inventories[0].Qty
}
