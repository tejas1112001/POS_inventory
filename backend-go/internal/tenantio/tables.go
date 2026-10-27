package tenantio

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Tables are the tenant tables an export contains. Excluded are never
// written to an export (OPERATIONS §6.2). A table in neither list fails the
// export, so a migration that adds a table must decide which list it joins.
var (
	Tables = []string{
		"audit_log", "brands", "categories", "customers", "doc_counters", "email_outbox", "files", "inventories",
		"order_items", "permissions", "product_variants", "products", "purchase_items", "purchases", "receipts",
		"return_items", "role_permissions", "roles", "sale_returns", "sales", "settings", "stock_adjustments",
		"stock_movements", "store_products", "stores", "sub_categories", "suppliers", "tenant_email_settings", "units",
		"users", "warehouses",
	}
	Excluded = map[string]string{
		"refresh_tokens":   "live sessions",
		"auth_tokens":      "invite and password-reset links",
		"pos_locks":        "transient",
		"idempotency_keys": "transient",
		"goose_db_version": "migration bookkeeping; a restore runs the migrations itself",
	}
)

// tablePlan is how one table is exported and restored.
type tablePlan struct {
	Name     string
	Columns  []string
	OrderBy  []string // the primary key
	Identity []string // identity columns (sequences reset after a restore)
}

// Catalog queries, run in the snapshot: current_schema() is the one
// schema on the session's search_path.
const (
	qTablesInSchema = `SELECT c.relname FROM pg_class c
		WHERE c.relnamespace = current_schema()::regnamespace AND c.relkind IN ('r','p') ORDER BY c.relname`
	qColumns = `SELECT a.attname, a.attidentity <> '' FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = $1
		  AND a.attnum > 0 AND NOT a.attisdropped AND a.attgenerated = '' ORDER BY a.attnum`
	qPrimaryKey = `SELECT a.attname FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid
		CROSS JOIN LATERAL unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
		WHERE c.relnamespace = current_schema()::regnamespace AND c.relname = $1 AND i.indisprimary ORDER BY k.ord`
	qForeignKeys = `SELECT DISTINCT c.relname, p.relname FROM pg_constraint co
		JOIN pg_class c ON c.oid = co.conrelid JOIN pg_class p ON p.oid = co.confrelid
		WHERE co.contype = 'f' AND c.relnamespace = current_schema()::regnamespace AND c.oid <> p.oid`
)

// texts collects one text column (call it with tx.Query of a constant).
func texts(rows pgx.Rows, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// plan reads the schema in the snapshot and returns the exported tables in
// dependency order (parents first; ties alphabetical).
func plan(ctx context.Context, tx pgx.Tx) ([]tablePlan, error) {
	names, err := texts(tx.Query(ctx, qTablesInSchema))
	if err != nil {
		return nil, err
	}
	var out []tablePlan
	present := map[string]bool{}
	for _, n := range names {
		if _, ok := Excluded[n]; ok {
			continue
		}
		if !slices.Contains(Tables, n) {
			return nil, fmt.Errorf("tenantio: table %q is neither exported nor excluded (tenantio.Tables)", n)
		}
		p := tablePlan{Name: n}
		rows, err := tx.Query(ctx, qColumns, n)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var col string
			var ident bool
			if err := rows.Scan(&col, &ident); err != nil {
				rows.Close()
				return nil, err
			}
			p.Columns = append(p.Columns, col)
			if ident {
				p.Identity = append(p.Identity, col)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if p.OrderBy, err = texts(tx.Query(ctx, qPrimaryKey, n)); err != nil {
			return nil, err
		}
		if len(p.Columns) == 0 || len(p.OrderBy) == 0 {
			return nil, fmt.Errorf("tenantio: table %q has no columns or no primary key", n)
		}
		present[n] = true
		out = append(out, p)
	}
	rows, err := tx.Query(ctx, qForeignKeys)
	if err != nil {
		return nil, err
	}
	deps := map[string][]string{}
	for rows.Next() {
		var child, parent string
		if err := rows.Scan(&child, &parent); err != nil {
			rows.Close()
			return nil, err
		}
		if present[child] && present[parent] {
			deps[child] = append(deps[child], parent)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return order(out, deps)
}

// order sorts tables so that every table comes after the tables it
// references (Kahn's algorithm; alphabetical among the ready ones).
func order(tables []tablePlan, deps map[string][]string) ([]tablePlan, error) {
	byName := map[string]tablePlan{}
	waiting := map[string]int{}
	users := map[string][]string{}
	for _, t := range tables {
		byName[t.Name] = t
		waiting[t.Name] = 0
	}
	for child, parents := range deps {
		for _, p := range parents {
			waiting[child]++
			users[p] = append(users[p], child)
		}
	}
	var ready []string
	for n, w := range waiting {
		if w == 0 {
			ready = append(ready, n)
		}
	}
	var out []tablePlan
	for len(ready) > 0 {
		sort.Strings(ready)
		n := ready[0]
		ready = ready[1:]
		out = append(out, byName[n])
		for _, u := range users[n] {
			if waiting[u]--; waiting[u] == 0 {
				ready = append(ready, u)
			}
		}
	}
	if len(out) != len(tables) {
		return nil, fmt.Errorf("tenantio: foreign keys form a cycle")
	}
	return out, nil
}
