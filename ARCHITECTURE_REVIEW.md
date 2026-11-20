# Architecture Review: Tenancy, Data Model and Fit by Business Type

| | |
|---|---|
| Date | 2026-11-20 |
| Scope | Tenant creation, schema per tenant, database tables and relationships, business-specific product attributes, data isolation, scalability, fit for Retail, Clothing, Footwear, Pharmacy and other business types |
| Method | Read-only review of the code, the migrations and the design docs. No code or data was changed. The only database access was read-only catalog queries on the dev database, inside a transaction that was rolled back. That database has no tenant schemas yet, so the per-tenant object counts below are worked out from the 32 tenant migrations. |
| Status | Findings only. The changes will be decided after this review. |

**Verdict:** the tenancy and isolation foundation is strong, well above typical SaaS POS builds. The weak points are:
- the migration and background-job layer, which will hit limits at a few hundred to about 1,000 shops;
- a few data-model decisions (whole-number quantities, GST held once per shop, business types written into every schema) that block grocery, loose pharmacy sales and multi-state chains.

---

## 1. How it works today

### 1.1 Creating a shop

`backend/internal/provisioning/provisioning.go` is a 10-step state machine (TENANCY_DESIGN §6). Every step can safely be re-run.

| # | Step |
|---|---|
| 1 | Check the slug (format, reserved list, history) and the plan |
| 2 | Write the registry row in `platform.tenants` (status `provisioning`) and the `provisioning_runs` record |
| 3 | Create the role `t_<id>_rw` and the schema `t_<id>` |
| 4 | Run all tenant migrations (goose, under a per-shop advisory lock) |
| 5 | Apply grants and the shop-bound outbox trigger (done in the migration post-step) |
| 6 | Seed settings, the business type and its recommended modules, the attribute presets, the roles and permissions, a default store and a Walk-in customer |
| 7 | Create the owner (no password yet) and an invite token |
| 8 | Create the trial subscription |
| 9 | Mark the shop live (`trialing`) and clear the slug cache |
| 10 | Send the welcome email (a failure here never fails the run) |

- A failure in steps 3–8 drops the schema and role and marks the run `failed`. The Super Admin can retry it.
- A reaper catches runs stuck in `provisioning`.
- Only the Super Admin can create shops; there is no public sign-up.

### 1.2 Schema per tenant

| Schema | Contents |
|---|---|
| `platform` | Registry, plans, subscriptions, invoices, platform users and roles, platform audit, migration runs, exports, overrides |
| `river` | Job queue |
| `extensions` | `citext`, `pg_trgm` (shared, so their type IDs are the same in every shop) |
| `t_<id>` | All of one shop's business data |

Each shop schema has **53 tables, 44 identity sequences and about 170 indexes**. Counting TOAST tables and their indexes, that is **roughly 350 database objects per shop**. The design estimated about 260 objects at 36 tables.

Shop tables:

| Group | Tables |
|---|---|
| Identity | `users`, `roles`, `permissions`, `role_permissions`, `refresh_tokens`, `auth_tokens` |
| Settings | `settings`, `tenant_email_settings`, `doc_counters` |
| Master data | `stores`, `warehouses`, `categories`, `sub_categories`, `brands`, `units`, `suppliers`, `customers`, `attribute_defs` |
| Catalogue | `products`, `product_variants`, `store_products` |
| Stock | `inventories`, `stock_movements`, `stock_adjustments`, `pos_locks`, `stock_counts`, `stock_count_lines`, `write_offs`, `write_off_lines` |
| Batches | `batches`, `batch_stock`, `batch_movements` |
| Selling | `sales`, `order_items`, `order_item_batches`, `sale_payments`, `held_carts`, `receipts`, `idempotency_keys` |
| Returns | `sale_returns`, `return_items`, `return_item_batches` |
| Customers' money | `customer_payments`, `customer_payment_allocations`, `customer_credit_entries` |
| Buying | `purchases`, `purchase_items`, `purchase_receipts`, `purchase_receipt_items`, `purchase_receipt_batches` |
| Other | `files`, `email_outbox`, `audit_log` |

### 1.3 Core relationships

```
products ─┬─< product_variants ─┬─< inventories (per store) ──< stock_movements (ledger)
          │                     ├─< batches ──< batch_stock (per store) ──< batch_movements
          │                     └─< order_items ──< order_item_batches
          ├── category / sub_category / brand / unit / supplier
          └─< store_products (published / featured per store)

sales ─┬─< order_items
       ├─< sale_payments
       ├── customer, store, user
       └─< sale_returns ──< return_items ──< return_item_batches

purchases ─< purchase_items ;  purchase_receipts ─< purchase_receipt_items ─< purchase_receipt_batches
```

- Sale and return lines keep copies of the name, SKU, price, tax, MRP and option label as they were at the time of sale.
- Soft deletes (`deleted_at`) are used for master data and the catalogue.
- Stock has a ledger: the `inventories.qty` totals must match the sums of `stock_movements`, and the same holds for batches. A nightly job checks this.

### 1.4 Business-specific attributes

- `attribute_defs` defines each attribute: `key`, `label`, `level` (`product` or `variant`), `kind` (`choice` or `text`), `choices` (jsonb) and `status`.
- Values are stored in `products.attributes` and `product_variants.attributes` (jsonb). `products.variant_dims` lists the variant dimensions (up to 4), for example `{size, colour}`.
- Presets are seeded from code (`internal/modules/presets.go`):
  - Clothing: department, collection, season, size, colour.
  - Footwear: department, size system (UK/US/EU/India), size, colour.
- Modules are product attributes, barcode labels, stock counts and batches with expiry.
- There are four separate layers: business type (a preset) → plan entitlement → the owner switches the module on → user permission. This matches the four-layer design in your industry expansion plan (§3.1) and is the right shape. A business type recommends modules; it never blocks mixed businesses.

### 1.5 Data isolation (all present in the code)

| Layer | Implementation |
|---|---|
| Names | Tenant ID from `crypto/rand`. Schema and role names are validated by `^t_[a-z0-9]{6,20}$` every time they are used, and can only be built through a validating constructor |
| Resolution | The schema name always comes from the registry row. It is never derived from the Host header, the token or the request body |
| Session | Each transaction switches to the shop's role and search path for that transaction only (`SET LOCAL ROLE t_<id>_rw` and `set_config('search_path', …, true)`). Role and path are checked again before commit (`tenancy/session.go`) |
| Database roles | The app's login role `pos_app` has no table rights and an empty search path. Each shop role sees only its own schema and `extensions`. `audit_log` and the ledgers are append-only for the shop role |
| Driver | `pgx` runs queries without per-connection prepared statements (Exec mode), so a cached query can never bind to another shop's schema. No per-schema custom types |
| Types | `TenantTx` and `PlatformTx` are different Go types, so a tenant query cannot be run on a platform handle (compile error) |
| Auth | The token's tenant ID must equal the shop resolved from the host. Users' status and role are reloaded from a short-lived cache, not trusted from the token |
| Files | Object keys are `tenants/<id>/…` |
| Public shop | An allow-listed DTO (no cost, stock counts or suppliers); published products only |

---

## 2. Issues

### High

#### H1. Tenant migrations serialise on one global lock and fail as shops are added
- **Where:** `backend/internal/db/ident.go:177` (`GrantTenantPrivileges`)
- **What:**
  - The grant step takes the global lock `pg_advisory_xact_lock(7340043)`, so every shop's post-migration grant step waits for the others.
  - The migration connection runs with `lock_timeout = 5s`, so with 8 shops migrating in parallel some give up and fail.
- **Evidence:** `TestParallelRunOverManySchemas` failed on 2026-11-20 with `failed post-step: ERROR: canceling statement due to lock timeout`. It passed when run alone. This is not really flakiness: it gets worse with every shop added.
- **Cause:** the lock exists only because `GRANT USAGE ON SCHEMA extensions TO t_<id>_rw` updates one shared catalog row ("tuple concurrently updated").
- **Fix:**
  - Create one group role (for example `pos_tenant`) that has `USAGE` on `extensions`, and make every shop role a member of it once, at provisioning.
  - Remove the global lock and the per-migration `extensions` grant.

#### H2. The "move a big shop to its own database" escape hatch is not built
- **Where:** `tenancy/session.go`, `tenancy/registry.go`
- **What:**
  - `tenants.db_cluster` is read into `Tenant.DBCluster`, but `Sessions` has a single pool and never looks at it.
  - At about 350 objects per shop, the design's own sharding trigger (a catalog over 300k objects, TENANCY_DESIGN §1.1) is reached at around **850 shops**, sooner than the 1,000 the design planned for.
- **Fix:**
  - Look up a pool per cluster (`DB_CLUSTERS` from secrets), used by both `InTenantTx` and the jobs.
  - Write the move runbook (read-only → export → restore → verify → switch `db_cluster` → clear caches).
  - Make the migration runner and the drift report aware of clusters.

#### H3. Every quantity is a whole number, and there are no unit conversions
- **Where:** 18 `integer` quantity columns, including `inventories.qty`, `order_items.quantity`, `batch_stock.qty`, `stock_movements.delta`, purchase lines, return lines, count lines and write-off lines. `units` has only a name and a short name.
- **Blocks:**
  - Grocery and kirana: sale by kg or litre, and scale barcodes with the price or weight built in.
  - Pharmacy loose sales (box → strip → tablet). Your industry plan §5.4 lists this as a core need.
  - Fabric (metres), hardware and wholesale (carton → pack → piece).
- **Why it matters now:** this is the most expensive one to change later. Columns, the ledger, batch allocation, receipts, reports, imports and the frontend all assume whole numbers. Decide it before you have many shops.
- **Fix (to design):**
  - `numeric(14,3)` quantities.
  - A base unit per product, sale and purchase units with conversion factors, and a rule per product for whether fractional sales are allowed.
  - Conversion snapshots on every transaction line.
  - Use the expand/contract migration pattern.

#### H4. A memory exhaustion risk from random subdomains
- **Where:** `tenancy/cache.go` and `tenancy/registry.go`
- **What:**
  - `TTLCache` has no size limit and only removes an expired entry when that same key is read again.
  - The registry caches "not found" results for every unknown slug.
  - With wildcard DNS, requests to millions of random subdomains each leave an entry that is never read again. Memory grows until the listener reconnects and flushes the cache. Each new slug also costs one platform query.
- **Fix:** cap the cache size (LRU) or sweep expired entries periodically. Optionally, cap negative entries separately and rate-limit unknown-host lookups per IP.

### Medium

#### M1. Business types, modules and reasons are written into every schema
- **What:**
  - Allowed values are fixed in database checks in every shop schema and in `platform`, as well as in Go: `settings_business_type_check`, `settings_modules_check`, `tenants_business_type_check`, `tenant_overrides_key_check`, and the `stock_movements.reason` and `batch_movements.reason` lists.
  - Adding a new vertical (grocery, electronics) or a new module therefore needs a migration across every shop.
- **Fix:**
  - Validate these lists in the app, from the code registry or a platform catalog table.
  - Keep database checks only for real integrity rules (amounts ≥ 0, pairs of status and timestamp, and so on).

#### M2. Background jobs loop over shops one at a time
- **Where:** `worker/maintenance.go` (`eachTenant`), `inventory/ledger.go` (`reconcileStock`)
- **What:**
  - Each maintenance job runs every shop in sequence inside one job with a 30-minute timeout. This covers stock-lock cleanup, idempotency-key cleanup, ledger reconcile, usage snapshots, low-stock emails and expiry digests.
  - The nightly reconcile re-adds the whole `stock_movements` history for each shop (`GROUP BY store_id, variant_id`) with a 2-minute limit per shop.
  - Both grow with shops × history, so large shops will start timing out and the whole job will overrun.
- **Fix:**
  - Queue one River job per shop, with limited parallelism.
  - Checkpoint the reconcile: store the verified balance at a movement ID, then check only newer movements.

#### M3. GST is modelled once per shop
- **What:**
  - There is one `settings.tax_registration_no` and one `tax_split` setting (`none`, `cgst_sgst` or `igst`) for the whole shop.
  - `stores` have no state or GSTIN. `customers` and `suppliers` have no GSTIN or state.
- **Impact:**
  - A chain operating in two states cannot issue correct invoices: it needs a GSTIN per state, with CGST+SGST or IGST derived from the place of supply.
  - B2B invoices need the buyer's GSTIN, and purchase tax credit needs the supplier's.
- **Fix:**
  - Add GSTIN and state code to stores (falling back to the shop setting), and GSTIN and state to customers and suppliers.
  - Work out the tax split per sale from the store state and the place of supply, and keep a copy on the sale.

#### M4. Deleted shops are never removed
- **Where:** `platform/deletion.go`, platform migration `00013_tenant_deleted.sql` ("The schema and files are not dropped (an operator does that later, by hand)")
- **Impact:** the schema, role and files stay forever. That inflates the database catalog and the role count, and keeps personal data past the retention period (a DPDP Act exposure).
- **Fix:** an automated, audited purge after the final export and the retention window: drop the schema and role, delete the object-storage prefix, and record a tombstone.

#### M5. The business type is stored twice
- **Where:** `settings/business.go:160`
- **What:**
  - It is stored in `platform.tenants.business_type` and in the shop's `settings.business_type`.
  - Copying it to the platform is best-effort: a failure only logs a warning.
  - There is no reconciliation job, so the Super Admin console filter can drift from the shop's real setting.
- **Fix:** a periodic reconcile (the usage snapshot job already visits every shop), or an outbox-style copy.

### Low

| # | Item |
|---|---|
| L1 | The request pool is capped at 40 connections and the job pool at 10, on `max_connections = 100`. Fine for one app instance; add PgBouncer (transaction mode is compatible with Exec mode), or budget connections, before running several app instances |
| L2 | `pos_app` is a member of every shop role. PostgreSQL rebuilds its role-membership cache whenever any role grant changes, for example on each new shop. Measure role switching with a few thousand shops |
| L3 | `product_variants.expiry_date` and `manufactured_date` are superseded by `batches`. Remove them in a contract release |
| L4 | Money precision is mixed: prices are `numeric(18,4)`, MRP is `numeric(14,2)` |
| L5 | There is no GIN index on the `attributes` jsonb. Filtering products by size or colour will be a full table scan once that filter exists |
| L6 | Each variant can have only one barcode (`variants_barcode_uq`). Retail often needs several: unit and case codes, and EANs that changed |

---

## 3. Fit by business type

| Type | Fit | What works | Gaps |
|---|---|---|---|
| **Retail** | Good for packaged goods | Variants, MRP with an above-MRP warning, labels, import, new checkout, customers' dues and payments | Fractional quantities and scale barcodes (H3); price per store (today the price is per variant only, the same in every store); several barcodes per variant (L6); GST per store (M3) |
| **Clothing** | Strong | Attributes, size-colour matrix, article numbers, barcode generation, labels, stock counts, matrix import | Attribute kinds are only choice or text (no number, date or yes/no); price per store; alterations and services |
| **Footwear** | Strong | Size systems, size-colour matrix, labels, stock counts | Same as clothing |
| **Pharmacy** | Batch core is solid | Batches with expiry, earliest-expiry-first selling, expired or recalled stock blocked, quarantine, write-offs, MRP per batch, batch-aware receipts, sales, returns and counts, expiry digest | Loose sales (H3); schedule H/H1/X classification; prescription capture and register; pharmacist dispense permission; salt/composition search; drug licence number on receipts; the online shop does not hide prescription medicines |
| **Electronics** | Not ready | Variants | Serial/IMEI tracking (none in the schema); warranty per serial; repairs |
| **Grocery / supermarket** | Not ready | Packaged retail | H3 (weight, scale barcodes, units); price per store |
| **Hardware / wholesale** | Not ready | — | H3 (unit conversions); price tiers per customer group; B2B GST (M3) |
| **Restaurant / cafe** | Out of scope | — | Needs its own model (orders to the kitchen, tables, add-ons, recipes). Build it as a separate module, not as product attributes |

---

## 4. Strengths to keep

- Isolation in several layers (§1.5): a wrong schema finds no rows, and a wrong role is refused by the database.
- Every provisioning step can be re-run, with clean-up on failure and a reaper for stuck runs.
- Migrations run on two tracks (platform and tenant), with per-shop locks, run records, `--resume`, a dry run, a drift report that compares structure fingerprints, and the expand/contract rule.
- Exec mode with no custom types per schema, so the same SQL is safe in every shop.
- Business types recommend modules rather than block them, and plan entitlement and the owner's choice are kept separate.
- Stock has a ledger with nightly reconciliation, including batches.
- Sale lines keep copies of their details as they were at the time of sale (name, price, tax, MRP, batch), so changing the catalogue later does not change past sales.

---

## 5. Recommended order

| Phase | Items | Why |
|---|---|---|
| 1. Quick, low risk | H1 (grant and lock fix), H4 (cache cap), M5 (business-type reconcile) | Small changes that remove a real failure mode and a denial-of-service risk |
| 2. Before scaling past about 300 shops | M2 (one job per shop, checkpointed reconcile), M4 (automated purge), M1 (move the lists out of the schemas) | Jobs and migrations that grow with the number of shops |
| 3. Data-model decisions for the next verticals | H3 (fractional quantities and units), M3 (GST per store and per party), price per store, several barcodes | Design these together: they all touch sales, purchases, receipts, reports and imports |
| 4. When nearing the sharding trigger | H2 (database per cluster and the move runbook) | Needed at roughly 850 shops, or earlier for one large shop |
| 5. Pharmacy compliance pack | Schedules, prescriptions, dispense permission, licence on receipts, online-shop restrictions | Needs the H3 decision first (loose sales), plus legal and pharmacy review |

---

## 6. Open decisions for the team

1. How many shops do you expect in 12–24 months? This sets when H2 is needed. Above about 5,000 small shops, a shared-tables-with-RLS tier for small shops becomes worth comparing (TENANCY_DESIGN §1).
2. Fractional quantities: switch every quantity column to `numeric`, or keep whole numbers in base units (for example grams or tablets) and convert at the edges?
3. GST: are multi-state chains and B2B invoices in scope for the first paying shops?
4. Which vertical comes after pharmacy? Grocery and electronics need very different groundwork (H3 versus serial tracking).
5. Retention period for deleted shops' data, which sets the M4 purge window.
