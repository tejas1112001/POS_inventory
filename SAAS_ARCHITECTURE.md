# ReactronPOS — SaaS (Multi-Tenant) Architecture Plan

Convert the existing **single-tenant** POS into a **SaaS product** on **one PostgreSQL database** and **one codebase**.

This is a design plan only — architecture and approach, not implementation code.

---

## Current starting point

ReactronPOS already has:

- One PostgreSQL database (Sequelize)
- JWT auth
- Roles: **admin** vs **biller**
- **Stores** as locations of one business
- Walk-in **customers**, products, inventory, purchases, sales, returns, reports

Treat a **tenant as a paying company**, not a store. Stores, billers, and walk-in customers stay nested *inside* a tenant.

---

## 0. Mental model

| Layer | Who | Today | After SaaS |
|---|---|---|---|
| Platform | You (the product owner) | n/a | Super-admin, plans, billing, all tenants |
| Tenant | A shop / chain | The whole DB | One company row + isolated data |
| Store | A location | Global `stores` table | Stores **belong to a tenant** |
| User | Admin / biller | Global unique email | Users **belong to a tenant** |
| Customer | Walk-in shopper | Global | Shoppers **belong to a tenant** |

Never reuse `store` as the tenant. A tenant can have many stores.

---

## 1. Multi-tenancy (data isolation)

**Approach:** shared database, shared schema, **row-level tenancy** (`tenant_id` on every business row). Not separate databases.

### New core table: `tenants`

Suggested fields:

- `id`
- `name`
- `slug`
- `status` — `trial` / `active` / `suspended` / `cancelled`
- `plan_id`
- billing-related fields
- timestamps

### Add `tenant_id` to every business table

Include:

- users
- stores
- warehouses
- categories, sub-categories, brands, units
- suppliers
- products, product variants
- inventory
- purchases, purchase items
- sales, order items
- sale returns, return items
- customers
- invoices (POS invoices)
- any report snapshots

**Do not** put `tenant_id` on platform tables:

- `tenants`
- `plans`
- `subscriptions`
- SaaS billing invoices / billing events

### Uniqueness becomes per-tenant

- User email: unique per tenant, **or** globally unique if you want simpler login
- Product SKU, store name, customer phone: unique **within** `tenant_id`, not globally

Today’s global unique constraints (for example SKU) will break once two shops exist.

### Request pipeline

Apply on every API call except public signup, login, and billing webhooks:

1. Authenticate JWT.
2. Read `tenantId` from the **token** (never trust a client-supplied header as the source of truth).
3. Middleware sets `req.tenantId`.
4. All queries: `WHERE tenant_id = req.tenantId`.
5. On create: always set `tenant_id` from the token.
6. On update/delete by id: also filter by `tenant_id` so IDs from another tenant return 404.

### Enforcement

- Sequelize global scope / hook so a forgotten `where` cannot leak rows.
- Composite indexes `(tenant_id, …)` on every hot query (sales by date, inventory by store, products for POS).

### Signup becomes tenant onboarding

- First signup creates a **tenant** + **tenant-admin** (owner) user — not a global admin.
- Later users are invited into that tenant.
- Existing rule “first user is admin” becomes: first user of *that tenant* is tenant-admin.

---

## 2. Subscription plans & billing

Keep billing in the **same DB**, owned by the **platform**, not mixed into POS sales logic.

### Tables

- **`plans`** — name, price, interval, limits (max stores, max users, max products), feature flags
- **`subscriptions`** — `tenant_id`, `plan_id`, status, current period, trial end, Stripe (or similar) customer/subscription IDs
- **`billing_events`** — webhook log (idempotent)

### Flow

1. Tenant signs up → `trial` with a default plan.
2. Checkout via Stripe Checkout / Customer Portal (or equivalent).
3. **Webhooks are the source of truth:** `active`, `past_due`, `cancelled`.
4. Middleware: if tenant is `suspended` / `cancelled`, allow login + billing page only; block POS/sales writes.

### Plan limits (application layer, not just UI)

On create store / user / product:

- Count rows for that `tenant_id`
- Reject if over plan cap

Feature flags (reports export, multi-store, etc.) checked the same way.

Do **not** put Stripe IDs on POS customer invoices. Keep SaaS invoices separate from POS invoices.

---

## 3. User roles (two layers)

Keep existing POS roles. Add a **platform** layer above them.

### Platform (your company)

`super_admin` — tenants, plans, optional impersonation, support.

Prefer a separate `platform_users` table so tenant queries never mix in your staff.  
Alternative: `users` with `tenant_id = NULL` and a distinct role (easier to leak if you are not careful).

### Inside a tenant

| Role | Meaning |
|---|---|
| `owner` | Billing, delete tenant, transfer ownership |
| `admin` | Same as today’s admin: master data, inventory, all sales |
| `biller` | POS + own sales (keep current behavior) |

- Existing `isAdmin` middleware becomes: **admin of this tenant**, not of the whole database.
- Billers still filtered to `sale.user_id = req.user.id` **and** `tenant_id`.

Optional later: `manager` per store via a `user_stores` join. Not required for v1.

### JWT claims

`{ userId, tenantId, role, planFeatures? }`

Keep short-lived access tokens (you already expire at 1 hour). Add refresh if sessions need to last longer.

---

## 4. Tenant-specific settings

Do **not** keep shop settings in `.env`. `.env` is for the **platform** (DB, JWT, Stripe keys).

### Table: `tenant_settings` (1:1 with tenant)

- Currency
- Timezone
- Tax %
- Receipt header / footer
- Invoice prefix
- Language
- Logo URL
- POS defaults (default store, default biller)

Load settings once per request (or cache by `tenantId`) and use them in receipts, tax calculation, and reports.

If you need arbitrary extras later, add `tenant_settings_kv` (`tenant_id`, `key`, `value`). Start with a typed row so POS tax/currency stay consistent.

### Files (logos)

Store in object storage under `tenants/{tenantId}/…`.  
Never a shared `/uploads` folder without a tenant prefix.

---

## 5. Deployment for many customers on one codebase

**Deploy one API + one web app + one Postgres.** Tenants are rows, not deploys.

- **Subdomain (optional):** `acme.app.com` → resolve slug. This is branding, not security. After login, tenant still comes from the JWT.
- **Electron:** one build; after login the desktop app is another client of the same API. Do not ship a per-customer database inside the desktop app for SaaS.
- **Config:** one `DB_*`, one `STRIPE_*`, one `JWT_SECRET`.
- **Background jobs** (reports, dunning): always include `tenant_id`; never scan unscoped tables.
- **Seed data:** **platform** seed (plans) vs **per-tenant** demo seed. Today’s global seed must not run across tenants.

### Ops

- Backups of the single database.
- Optional later: Postgres Row Level Security (`SET app.tenant_id` + RLS) as a second lock.
- Application scoping (`WHERE tenant_id = …`) is **mandatory** either way.

---

## Suggested build order

1. **Introduce `tenants` + `tenant_id`**, backfill existing data as tenant #1, add query scoping + JWT claim. Prove no cross-tenant reads.
2. **Change signup** to create tenant + owner; stop treating the first global user as god.
3. **Split roles:** platform super-admin vs tenant owner / admin / biller.
4. **`tenant_settings`** — move currency / tax / receipt out of hardcoding.
5. **Plans + limit checks** (even before payment).
6. **Stripe (or similar) + webhooks + suspend-on-failure.**
7. **Tenant admin UI:** invite users, stores, billing portal. **Platform UI:** list tenants, change plan, suspend.
8. **Harden:** composite unique indexes, optional RLS, file path prefixing, webhook idempotency.

After step 1, current POS flows (stores, inventory, checkout, billers) stay the same; they just always run inside one tenant’s slice of the same database.

---

## What not to do

- Do not give each customer their own database if you want **one DB** and **one codebase**.
- Do not trust `X-Tenant-Id` from the browser.
- Do not confuse **POS customer** (shopper) with **SaaS tenant** (paying company).
- Do not leave unique constraints global (`users.email` unique worldwide can be OK; `products.sku` unique worldwide is not).
- Do not skip `tenant_id` on inventory / sales — that is where leaks and billing disputes happen.

---

## Target data shape (one DB)

```
PLATFORM
  platform_users
  plans
  tenants ──────────────┐
  subscriptions         │
  billing_events        │
                        │ tenant_id
TENANT SLICE            ▼
  tenant_settings
  users (owner / admin / biller)
  stores, warehouses
  categories, brands, units, suppliers
  products + variants
  inventory (per variant, per store)
  customers (shoppers)
  purchases / sales / returns / POS invoices
```
