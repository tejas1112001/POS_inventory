# S10 — Design note

Status: **waiting for approval**. Nothing below is built yet. Each change to stored data (D), permissions (P) or billing rules (B) is numbered; the last section lists them for a yes/no. Shifts, cash count, day-end report and GST per sale are out of scope (later).

Common rules for every item: S4b design (Inter, navy/slate/blue, 100dvh), kit components, server checks first (the UI only hides what the server refuses), audit rows for every change, tenant-isolation tests for every new endpoint, the console under its strict CSP.

---

## 1. Platform users page (Super Admin console)

**What**: list platform users (email, name, role, status, MFA enrolled, last login); add a user; change role; deactivate / reactivate; reset another admin's MFA.

**How**
- Add: name, email, role. The server creates the user with a one-time temporary password (shown once to the adding admin, like `posctl bootstrap-superadmin`), `must_change_password` and MFA enrolment on first sign-in. The new user gets an email notice *without* the password ("an account was created for you; ask <admin> for the first password").
- Change role / deactivate / reactivate / reset MFA: step-up MFA, written reason (10+ characters), audit row, email notice to the affected user and to the platform inbox.
- Deactivate and reset MFA end all of that user's sessions at once. Reset MFA clears the TOTP secret and recovery codes; the user enrols again at the next sign-in.
- Guards: nobody changes their own role, deactivates themselves or resets their own MFA here; at least one active super_admin with MFA always remains.

**Data**: none new — existing `platform_users`, `platform_recovery_codes`, `platform_sessions`; one new email template.
**Permissions**: existing `platform_users:manage` (super_admin only).
**Risks**: account takeover by a rogue super_admin (mitigated by step-up, reason, audit, notices to the victim and the inbox); locking everyone out (last-super-admin guard).

## 2. Plan changes (owner upgrade/downgrade, console assign-plan)

**What**: the owner changes plan from Plan & billing while Razorpay bills; the console's assign-plan works again for paying shops.

**How**
- New provider call `ChangePlan(subscription, plan, when)` (Razorpay `PATCH /subscriptions/:id` with `plan_id` and `schedule_change_at`). The fake provider implements it for tests.
- **B1 Upgrade**: takes effect now; Razorpay's proration decides the charge. **B2 Downgrade**: takes effect at the end of the paid period (scheduled); it is refused while the shop uses more than the smaller plan allows (stores, users, products, storage), with the list of what to reduce. **B3** A scheduled downgrade can be cancelled by the owner before it applies.
- **B4 Console assign-plan for paying shops**: allowed again; it changes the plan at Razorpay too (so the next webhook does not set it back), needs `billing:manage`, step-up and a reason.
- Webhook handling: the plan follows `subscription.updated`/`charged` events as today; a scheduled change is applied when the provider reports it.

**Data**: **D1** `platform.subscriptions` gets `pending_plan_id` and `pending_change_at` (a scheduled downgrade).
**Permissions**: tenant `billing:manage` (owner by default) for owner changes; console as today.
**Live Razorpay test must verify** (adds to BILLING_DESIGN §10 R7): plan change allowed for cards / UPI Autopay / eMandate; proration amount on upgrade; `schedule_change_at=cycle_end` behaviour and which webhook fires; that a cancelled scheduled change really stays cancelled; plan IDs must match `plans.provider_plan_id`.
**Risks**: double charges or free upgrades if the webhook and our state disagree (covered by item 3), downgrades that strand data (blocked by B2).

## 3. Razorpay reconcile job

**What**: every hour, fetch each live subscription from Razorpay and compare status, plan and period end with ours; list differences; alert when webhooks go quiet.

**How**: report only — no automatic fixes (an operator applies a fix from the console with the existing actions). Drift rows are shown on Billing → "Drift" and counted in a metric; a Prometheus alert and a platform-inbox email fire when there are live subscriptions but no webhook for 24 hours (configurable), or when drift persists two runs in a row.
**Data**: **D2** `platform.billing_drift` (tenant, field, ours, theirs, first_seen, last_seen, resolved_at).
**Permissions**: `billing:view` to see drift.
**Risks**: Razorpay API rate limits (one fetch per subscription per hour, spread out); false alarms in quiet periods (threshold).

## 4. Nightly exports with retention, and the final deletion step

**Nightly exports**: at 01:00 (shop time zone ignored; UTC 19:30) every live tenant gets an encrypted `nightly` export (the existing export code). **B5/D3 Retention**: keep the last 7 daily and the last 4 weekly (Sunday) exports; older nightly exports have their files **deleted from storage** and their rows marked expired. Manual and final exports are never pruned automatically.

**Final deletion step** (today: `pending_deletion` → nothing; the schema is never dropped):
- Warning emails to the owner and billing email 14, 7 and 1 day(s) before the deletion date.
- A final export is queued automatically and must pass a restore test before deletion can be finalised.
- Console "Finalise deletion": Super Admin, step-up, written reason, the tenant's slug typed to confirm. Effect: status `deleted`, every session ended, the slug held for a year (existing redirect rule). **The schema and files are not dropped** (an operator does that by hand later, documented).

**Data**: **D3** `tenant_exports` gets `expired_at` (pruned nightly); **D4** `tenants` gets `deletion_due_at`, `deleted_at`, `deleted_by`; email templates for the warnings.
**Risks**: storage cost (bounded by retention); a deleted shop that wanted its data (final export kept, restore documented).

## 5. Part payment, split payment, customer dues

**What**: in the new checkout and Add Sale, pay one sale with several methods (cash, UPI, card, credit = on account), or pay part now; dues per customer; "Collect payment" against dues; a customer statement.

**How**
- **D5** new tenant table `sale_payments` (sale, method `cash|upi|card|credit|cheque|deposit|points`, amount, tendered and change for cash, reference such as a UPI ID or card last 4 digits, user, time). A sale's `paid` = sum of non-credit payments; `due` = total − paid; payment status as today (Paid / Partial / Unpaid). **Existing sales get one payment row each** from their current paid amount and method (backfill migration).
- **D6** new tenant table `customer_payments` (collection against dues) and `customer_payment_allocations` (which sales it pays, oldest first). Returns reduce dues as today.
- **D7** `customers.credit_limit` (null = no credit allowed).
- **B6** Selling on credit or part payment needs the `sales:credit` permission and a named customer (never Walk-in), and the customer's total dues after the sale must stay within their credit limit (no limit set = no credit). Only a user with `customers:credit_limit` can raise a limit; there is no per-sale override.
- Customer statement: sales, payments, returns and running balance for a date range, printable (browser print, A4).
- Receipt and PDF show each payment line and the balance due.
- **P1** new permissions: `sales:credit` (sell on credit / part payment; owner only by default), `customers:credit_limit` (set limits; owner only), `customers:collect` (collect payments; owner and admin by default, not cashiers unless granted). UPI is recorded as a method only (no QR or bank integration).

**Risks**: money correctness (server computes everything; idempotency keys as today; tests for every split/part/return combination), report totals (sales reports gain a "by method" split; "paid" stays the sum), concurrency on a customer's dues (row lock on the customer while allocating).

## 6. Low-stock alerts

**What**: dashboard card (exists), an in-app notification banner for users who can see stock, and a daily email (08:00 shop time) to the owner listing items at or below their alert level, once per day, only when the list is not empty.
**Data**: **D8** `settings.low_stock_email` (default on); tenant `email_outbox` gets a new kind `low_stock` (CHECK constraint change); de-duplicated per day.
**Risks**: noisy emails (one per day, owner can turn it off).

## 7. Held bills

**What**: in the new checkout, "Hold" parks the current cart with an optional label; "Recall" lists held carts and restores one (re-checks prices and stock through the quote; changed prices use the existing "Prices have changed" dialog).
**Data**: **D9** tenant table `held_carts` (user, store, customer, label, lines and discount as JSON, created, expires after 7 days). Holding releases the product locks; recalling takes them again.
**Permissions**: `pos:create`; a cashier sees only their own held carts, owner/admin see all of the store.
**Risks**: stale prices or stock (re-checked on recall), clutter (7-day expiry, max 20 per user).

## 8. Onboarding checklist

**What**: on the dashboard of a new shop: business profile and tax number, logo, store address, first product (or CSV import), first staff user, email sending, first sale, online shop (if in plan). Each step links to its page; ticks come from real data, not clicks. "Hide checklist" for good.
**Data**: **D10** `settings.onboarding_hidden_at`.
**Permissions**: shown to users with `settings:edit`.

---

## Changes that need your approval

| # | Item | Change |
|---|---|---|
| D1 | 2 | `platform.subscriptions.pending_plan_id`, `pending_change_at` |
| D2 | 3 | new `platform.billing_drift` |
| D3 | 4 | `tenant_exports.expired_at`; nightly exports pruned |
| D4 | 4 | `tenants.deletion_due_at`, `deleted_at`, `deleted_by` |
| D5 | 5 | new tenant `sale_payments` + backfill of existing sales |
| D6 | 5 | new tenant `customer_payments`, `customer_payment_allocations` |
| D7 | 5 | `customers.credit_limit` |
| D8 | 6 | `settings.low_stock_email`; email kind `low_stock` |
| D9 | 7 | new tenant `held_carts` |
| D10 | 8 | `settings.onboarding_hidden_at` |
| P1 | 5 | new permissions `sales:credit`, `customers:credit_limit` (owner only), `customers:collect` (owner, admin) |
| B1 | 2 | upgrades take effect now (Razorpay prorates) |
| B2 | 2 | downgrades at period end, refused while usage exceeds the smaller plan |
| B3 | 2 | owner can cancel a scheduled downgrade |
| B4 | 2 | console assign-plan allowed for paying shops (changes the plan at Razorpay; step-up, reason) |
| B5 | 4 | nightly export retention 7 daily + 4 weekly; older nightly files deleted |
| B6 | 5 | credit and part payment need `sales:credit` and a named customer; total dues stay within the credit limit (none set = no credit); no per-sale override |
| — | 1 | platform user page uses existing tables and `platform_users:manage`; temporary first password shown once to the adding admin |
| — | 4 | "Finalise deletion" marks the tenant deleted; no schema or file drop |
