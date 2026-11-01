# S9 security note: feature flags, limit overrides and shop usage in the console

Status: **waiting for approval**. Nothing in this note is built yet. S9 part 1 (console layout, tenant list sort and saved views, contacts, notes, health card) is done and uses platform data only.

## 1. What is proposed

| Item | What it does | Why it is sensitive |
|---|---|---|
| **Limit overrides** | Per shop, raise or lower one plan limit (stores, users, products, emails a day, storage MB). | It gives paid capacity away or takes it from a paying shop: billing bypass by a console user, or harm to a live shop by mistake. |
| **Feature flags** | Per shop, switch a feature on or off against its plan (for example the online shop). | The same, for features; a flag that turns off a feature can break a shop's work. |
| **Shop usage counts** | Show users, stores and products used against the limits on the tenant page. | Today the console never reads a shop's own data. Counting needs a path from the console side into each shop's data. |
| **Per-shop email health** | Receipts sent, failed and waiting for one shop. | Same as above: receipt mail lives in each shop's data. |

## 2. Risks

1. **Insider abuse or a stolen console session**: free capacity for a friendly shop, or a competitor's shop throttled.
2. **Mistakes**: a wrong number (0 instead of 10) blocks a shop's sales of new products or new users at once.
3. **Silent drift from billing**: an override that never ends keeps a shop above its plan after it downgrades or stops paying.
4. **Enforcement gaps**: if any limit check reads the plan directly instead of "plan + overrides", the override is ignored in one place and honoured in another.
5. **Weakening isolation**: a console query that reads shop data could, if built carelessly, read more than counts or the wrong shop.
6. **Audit gaps**: changes without a reason or without before/after values cannot be reviewed.

## 3. Proposed controls

**Who and how**
- New console permission `tenants:override`, **super_admin only** (not billing_admin, not support).
- Every change needs **step-up MFA** (as suspend and slug changes do today) and a **reason of at least 10 characters**.
- Overrides and flags come from a **fixed server list** of keys; no free-form keys or values. Numbers are bounded (0 to 10 × the plan's value, with hard caps).

**Lifetime**
- Every override has an **expiry date** (default 30 days, at most 1 year); expired ones stop counting automatically.
- **Revoke**, never edit or delete: the table is append and revoke only (the app role cannot update values or delete rows, as with the notes timeline).
- When a shop changes plan, the console shows its active overrides so they can be revoked.

**Enforcement**
- One server function, `EffectiveLimits(shop)` = plan limits + active overrides, used by every limit check (plan check for stores, users and products; storage check; daily email cap) inside the same transaction and the same per-shop lock as today.
- Flags are read the same way by the feature checks (plan features + active flags).

**Visibility and audit**
- Each change writes an audit row with actor, reason, key, before, after and expiry.
- The tenant page shows an "Overrides active" box; the tenants list gets a "Has overrides" filter.
- Optional: an email to the platform inbox for every override (on by default).

**Shop usage and email health (no direct reads)**
- **Recommended:** the background worker, which already runs per shop, writes a small **usage snapshot** (counts only: users, stores, products, receipts sent, failed and waiting in the last 7 days) into a platform table every 15 minutes. The console reads only that table. The console never opens shop data, and the snapshot holds no names, emails or amounts.
- Alternative (not recommended): a database function that returns counts for one shop, callable by the console role. Faster to build, but it adds a console-side path into shop data.

## 4. Tests that would come with it

- Route matrix rows: only super_admin, step-up required, reason required.
- Bounds and fixed keys: unknown key and out-of-range value are refused.
- Expiry: an expired override no longer counts; revoke takes effect at once.
- Enforcement: plan check, storage and email cap use the override, both raised and lowered (concurrent creates at the limit still cannot both pass).
- Audit: every change has actor, reason, before/after and expiry; the app role cannot update or delete override rows.
- Snapshot: counts match the shop's data; the console role has no access to shop schemas (isolation suite).

## 5. Decisions needed

1. Permission `tenants:override` for super_admin only, with step-up and a required reason: **yes / change**.
2. Default expiry 30 days, maximum 1 year: **yes / other values**.
3. Usage snapshot written by the worker (recommended) instead of console reads of shop data: **yes / alternative**.
4. Flags to offer at first (proposed: online shop, CSV import, new checkout): **yes / list**.
5. Email the platform inbox on every override: **yes / no**.
