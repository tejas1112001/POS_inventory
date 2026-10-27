//go:build integration

// Phase 3 item 2: cross-tenant isolation on every endpoint and in the
// database. The per-module "...IsolationAcrossTenants" tests check chosen
// objects; these walk every read route and every privilege.
package isolation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/rbac"
)

// canaryTenant is a tenant of its own whose data all carries one marker.
type canaryTenant struct {
	l, marker, token string
	ids              []string // numeric object IDs (products, sales, ...); per-tenant sequences, so the same numbers name other tenants' own rows
	fileIDs          []string // private and public file UUIDs
	storeSlug        string
	productID        string
	barcode          string
}

func seedCanary(t *testing.T) canaryTenant {
	t.Helper()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	m := "cnry" + hex.EncodeToString(b) // lowercase: usable in slugs and emails
	l := billingTenant(t, "iso-canary")
	tok, _ := ownerLogin(t, l)
	host := tenants[l].Host
	c := canaryTenant{l: l, marker: m, token: tok}
	id := func(v any) string { return jsonID(v.(float64)) }
	post := func(path string, body map[string]any) resp {
		t.Helper()
		r := do(t, rq{method: "POST", host: host, path: path, token: tok, body: body})
		require.Contains(t, []int{200, 201}, r.Code, "%s: %s", path, r.Raw)
		return r
	}

	store := storeIDs(t, l, tok)[0]
	c.ids = append(c.ids, jsonID(store))
	c.storeSlug = m + "-store"
	sh := do(t, rq{method: "PUT", host: host, path: "/api/stores/" + jsonID(store) + "/showcase", token: tok, body: map[string]any{
		"slug": c.storeSlug, "description": m + " shop", "isPublicEnabled": true, "logo": pngDataURI(t, color.RGBA{R: 9, A: 255})}})
	require.Equal(t, 200, sh.Code, sh.Raw)
	c.fileIDs = append(c.fileIDs, strings.TrimPrefix(sh.Body["logo"].(string), "/files/t/"))

	for _, path := range []string{"/api/categories", "/api/brands", "/api/units", "/api/warehouses"} {
		name := m + strings.ReplaceAll(path, "/api/", "-")
		body := map[string]any{"name": name, "status": "Active"}
		if path == "/api/units" {
			body = map[string]any{"name": name, "short_name": name}
		}
		r := post(path, body)
		c.ids = append(c.ids, id(r.Body["id"]))
	}
	cu := post("/api/customers", map[string]any{"name": m + "-customer", "image": pngDataURI(t, color.RGBA{B: 9, A: 255})})
	c.ids = append(c.ids, id(cu.Body["id"]))
	c.fileIDs = append(c.fileIDs, strings.TrimPrefix(cu.Body["image"].(string), "/api/files/"))
	supplier := createSupplier(t, l, tok, m+"-supplier")
	c.ids = append(c.ids, jsonID(supplier))

	p := stockedProduct(t, l, tok, m+"sku", store, 50)
	c.productID, c.barcode = jsonID(p.pid), "BC"+p.sku
	c.ids = append(c.ids, jsonID(p.pid), jsonID(p.vid))
	publish(t, l, tok, store, p.pid, "isPublished")
	publish(t, l, tok, store, p.pid, "isFeatured")

	sale := posSale(store, line(p, 2))
	sale["saleNote"] = m + " sale"
	s := sell(t, l, tok, sale, nil)
	require.Equal(t, 201, s.Code, s.Raw)
	c.ids = append(c.ids, id(s.Body["id"]))
	ret := postReturn(t, l, tok, retBody(s.Body["id"], store, retItem(p, 1)), nil)
	require.Equal(t, 201, ret.Code, ret.Raw)

	pu := postPurchase(t, l, tok, purchaseBody(supplier, m+"-po", "Ordered", purchaseLine(p.pid, p.vid, store, 3, 10)), nil)
	require.Equal(t, 201, pu.Code, pu.Raw)
	c.ids = append(c.ids, id(pu.Body["id"]))
	ab := adjust(p.vid, store, "Addition", 1)
	ab["note"] = m + " recount"
	adj := postAdjustment(t, l, tok, ab, nil)
	require.Equal(t, 201, adj.Code, adj.Raw)

	role := post("/api/roles", map[string]any{"name": m + "-role"})
	c.ids = append(c.ids, id(role.Body["id"]))
	c.ids = append(c.ids, jsonID(float64(createUser(t, l, tok, m+"-user@"+l+".test", "biller", "canary-user-pass-123"))))

	slices.Sort(c.ids)
	c.ids = slices.Compact(c.ids)
	return c
}

// concretePaths expands a route pattern with the canary tenant's values.
func (c canaryTenant) concretePaths(pattern string) []string {
	p := strings.TrimPrefix(pattern, "/api")
	p = strings.ReplaceAll(p, "{slug}", c.storeSlug)
	p = strings.ReplaceAll(p, "{product}", c.productID)
	p = strings.ReplaceAll(p, "{barcode}", c.barcode)
	ids := c.ids
	if strings.HasPrefix(p, "/files/") {
		ids = c.fileIDs
	}
	var out []string
	if strings.Contains(p, "{id}") {
		for _, id := range ids {
			out = append(out, "/api"+strings.ReplaceAll(p, "{id}", id))
		}
	} else {
		out = append(out, "/api"+p)
	}
	q := "?q=" + c.marker + "&search=" + c.marker + "&page=1&limit=100&from=2000-01-01&to=2100-01-01"
	for _, base := range slices.Clone(out) {
		out = append(out, base+q)
	}
	return out
}

// readable returns the text a leak check can search: raw bytes, or the
// cells of a spreadsheet export.
func readable(t *testing.T, r resp) string {
	if strings.Contains(r.Header.Get("Content-Type"), "spreadsheetml") {
		var b strings.Builder
		for _, row := range xlsxRows(t, r.Raw) {
			b.WriteString(strings.Join(row, " "))
			b.WriteByte('\n')
		}
		return b.String()
	}
	return r.Raw
}

func TestNoReadRouteShowsAnotherTenantsData(t *testing.T) {
	c := seedCanary(t)
	canaryID := tenants[c.l].ID
	var reads []route
	for _, rt := range sortedRoutes(t) {
		if rt.method == "GET" {
			reads = append(reads, rt)
		}
	}
	require.Greater(t, len(reads), 30)
	t.Logf("%d read routes; canary has %d object IDs and %d files", len(reads), len(c.ids), len(c.fileIDs))

	// Positive control: on its own host the canary data is visible, so the
	// marker check below would see a leak.
	seen := map[string]bool{}
	for _, rt := range reads {
		for _, path := range c.concretePaths(rt.path) {
			r := do(t, rq{method: "GET", host: tenants[c.l].Host, path: path, token: c.token})
			if strings.Contains(readable(t, r), c.marker) {
				seen[rt.key] = true
			}
		}
	}
	for _, must := range []string{"GET /api/products", "GET /api/customers", "GET /api/suppliers", "GET /api/sales",
		"GET /api/purchases", "GET /api/users", "GET /api/roles", "GET /api/categories", "GET /api/brands",
		"GET /api/stock-adjustments", "GET /api/public/shop/{slug}/products", "GET /api/public/shop/{slug}"} {
		require.True(t, seen[must], "control: %s should show the canary on its own host (seen on %d routes)", must, len(seen))
	}
	t.Logf("control: the canary is visible on %d read routes of its own host", len(seen))

	// Every read route, on every other tenant's host, signed in as that
	// tenant's owner (all permissions) and signed out, with the canary's
	// IDs, slugs, barcode and marker in the path and query.
	requests := 0
	for _, l := range []string{"a", "b", "c"} {
		owner, _ := ownerLogin(t, l)
		for _, rt := range reads {
			for _, path := range c.concretePaths(rt.path) {
				for _, tok := range []string{owner, ""} {
					if tok == "" && rt.req.Kind != rbac.KindPublic {
						continue
					}
					r := do(t, rq{method: "GET", host: tenants[l].Host, path: path, token: tok})
					require.Less(t, r.Code, 500, "%s on %s: %s", path, l, r.Raw)
					body := readable(t, r)
					require.NotContains(t, body, c.marker, "%s on tenant %s shows the canary tenant's data", path, l)
					require.NotContains(t, body, canaryID, "%s on tenant %s names the canary tenant", path, l)
					requests++
				}
			}
		}
		// Files are served by ID outside /api too.
		for _, f := range c.fileIDs {
			for _, path := range []string{"/files/t/" + f, "/api/files/" + f} {
				r := do(t, rq{method: "GET", host: tenants[l].Host, path: path, token: owner})
				require.Equal(t, 404, r.Code, "%s on tenant %s", path, l)
			}
		}
	}
	t.Logf("%d read requests on 3 other tenants: no canary data", requests)
}

// ------------------------------------------------------- DB privileges

func TestTenantRolesHoldNothingOutsideTheirSchema(t *testing.T) {
	ctx := context.Background()
	c := admin(t)
	strs := func(q string, args ...any) []string {
		t.Helper()
		rows, err := c.Query(ctx, q, args...)
		require.NoError(t, err)
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		require.NoError(t, err)
		return out
	}
	letters := []string{"a", "b", "c"}
	for _, owner := range letters {
		role := schema(owner) + "_rw"

		// No special attributes, and member of no other role.
		var super, createRole, createDB, bypass, login, repl bool
		require.NoError(t, c.QueryRow(ctx, `SELECT rolsuper, rolcreaterole, rolcreatedb, rolbypassrls, rolcanlogin, rolreplication
			FROM pg_roles WHERE rolname = $1`, role).Scan(&super, &createRole, &createDB, &bypass, &login, &repl))
		require.False(t, super || createRole || createDB || bypass || login || repl, "%s has a role attribute", role)
		require.Empty(t, strs(`SELECT r.rolname::text FROM pg_auth_members m JOIN pg_roles r ON r.oid = m.roleid
			JOIN pg_roles u ON u.oid = m.member WHERE u.rolname = $1`, role), "%s is a member of another role", role)
		// Its members can SET ROLE to it but never inherit its privileges,
		// and no member is another tenant's role.
		for _, row := range strs(`SELECT u.rolname::text || ':' || m.inherit_option::text FROM pg_auth_members m
			JOIN pg_roles r ON r.oid = m.roleid JOIN pg_roles u ON u.oid = m.member WHERE r.rolname = $1`, role) {
			name, inherit, _ := strings.Cut(row, ":")
			require.False(t, strings.HasPrefix(name, "t_"), "%s is a member of %s", name, role)
			require.Equal(t, "false", inherit, "%s inherits %s", name, role)
		}
		// It can create nothing anywhere, its own schema included.
		for _, ns := range strs(`SELECT nspname::text FROM pg_namespace`) {
			var create bool
			require.NoError(t, c.QueryRow(ctx, `SELECT has_schema_privilege($1, $2, 'CREATE')`, role, ns).Scan(&create))
			require.False(t, create, "%s can CREATE in %s", role, ns)
		}

		for _, other := range letters {
			if other == owner {
				continue
			}
			s := schema(other)
			for _, tbl := range strs(`SELECT tablename::text FROM pg_tables WHERE schemaname = $1`, s) {
				for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
					var got bool
					require.NoError(t, c.QueryRow(ctx, `SELECT has_table_privilege($1, $2, $3)`, role, s+"."+tbl, priv).Scan(&got))
					require.False(t, got, "%s %s on %s.%s", role, priv, s, tbl)
				}
			}
			for _, seq := range strs(`SELECT sequencename::text FROM pg_sequences WHERE schemaname = $1`, s) {
				for _, priv := range []string{"USAGE", "SELECT", "UPDATE"} {
					var got bool
					require.NoError(t, c.QueryRow(ctx, `SELECT has_sequence_privilege($1, $2, $3)`, role, s+"."+seq, priv).Scan(&got))
					require.False(t, got, "%s %s on sequence %s.%s", role, priv, s, seq)
				}
			}
			for _, fn := range strs(`SELECT p.oid::regprocedure::text FROM pg_proc p WHERE p.pronamespace = $1::regnamespace AND p.prosecdef`, s) {
				var got bool
				require.NoError(t, c.QueryRow(ctx, `SELECT has_function_privilege($1, $2, 'EXECUTE')`, role, fn).Scan(&got))
				require.False(t, got, "%s can run %s's SECURITY DEFINER function %s", role, s, fn)
			}
		}
	}

	// Every SECURITY DEFINER function in a tenant schema pins its
	// search_path, so a caller cannot redirect its unqualified names.
	n := 0
	for _, row := range strs(`SELECT p.oid::regprocedure::text || '|' || coalesce(array_to_string(p.proconfig, ','), '')
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname LIKE 't\_%' AND p.prosecdef`) {
		fn, cfg, _ := strings.Cut(row, "|")
		require.Contains(t, cfg, "search_path=", "%s is SECURITY DEFINER without a pinned search_path", fn)
		n++
	}
	require.Positive(t, n, "mark_outbox_pending exists in every tenant schema")

	// The application login roles hold no privilege on any tenant schema
	// of their own; they reach tenant data only through SET ROLE.
	for _, app := range strs(`SELECT rolname::text FROM pg_roles WHERE rolname IN ('pos_app', 'pos_jobs', 'pos_platform')`) {
		for _, l := range letters {
			var usage bool
			require.NoError(t, c.QueryRow(ctx, `SELECT has_schema_privilege($1, $2, 'USAGE')`, app, schema(l)).Scan(&usage))
			require.False(t, usage, "%s has USAGE on %s", app, schema(l))
		}
	}
}
