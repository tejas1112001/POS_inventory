# Billing Design — Razorpay SaaS Subscriptions

| | |
|---|---|
| Status | Phase 1: full design. Phase 2 builds the minimum (§9) |
| Provider | Razorpay Subscriptions API, INR, behind `BillingProvider` |
| Rule | Only a **verified webhook** (or a server-side reconcile pull) changes subscription status. Client-side "payment done" is never trusted |
| Separation | SaaS billing (platform ↔ tenant) is completely separate from POS payments (tenant ↔ shop customer) |

---

## 1. Interfaces

```go
// internal/billing
type BillingProvider interface {
    CreateCustomer(ctx context.Context, t TenantRef, email, name string) (providerCustomerID string, err error)
    CreateSubscription(ctx context.Context, req CreateSubscriptionReq) (Subscription, error) // returns short_url for checkout
    ChangePlan(ctx context.Context, providerSubID, newProviderPlanID string, at ChangeAt) (Subscription, error)
    CancelSubscription(ctx context.Context, providerSubID string, atPeriodEnd bool) error
    FetchSubscription(ctx context.Context, providerSubID string) (Subscription, error)       // reconcile
    ListInvoices(ctx context.Context, providerSubID string) ([]Invoice, error)
    VerifyWebhook(rawBody []byte, headers http.Header) (Event, error)                        // signature + parse
}

// internal/payments  (tenant POS payments, recorded only)
type PaymentProvider interface {
    Name() string                                  // "manual" (cash/card/UPI recorded) by default
    Capture(ctx context.Context, p PaymentIntent) (PaymentResult, error)
}
```

Razorpay implementation: plain `net/http` client with Basic auth (`RAZORPAY_KEY_ID:RAZORPAY_KEY_SECRET`), 10 s timeout, retries only on idempotent GETs. We **don't** use `razorpay-go` (v1.4.1): we need only ~6 REST calls, and a thin typed client is easier to test, keeps the dependency tree small, and lets us control timeouts and logging (no secret leakage). Key/secret/webhook secret come from env or the secret manager, are never logged and are never returned by any API.

## 2. Platform tables

```sql
CREATE TABLE platform.plans (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  code text NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9_]{2,40}$'),
  name text NOT NULL,
  price_minor bigint NOT NULL CHECK (price_minor >= 0),  -- paise
  currency char(3) NOT NULL DEFAULT 'INR',
  billing_interval text NOT NULL CHECK (billing_interval IN ('monthly','yearly')),
  razorpay_plan_id text UNIQUE,
  trial_days int NOT NULL DEFAULT 14,
  limits jsonb NOT NULL,        -- {"max_stores":1,"max_users":3,"max_products":500,"max_emails_per_day":300,"max_storage_mb":500}
  features jsonb NOT NULL,      -- {"reports_export":true,"public_shop":true,"tenant_smtp":true,"custom_domain":false,"multi_store":false}
  is_public boolean NOT NULL DEFAULT true, is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform.subscriptions (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id text NOT NULL UNIQUE REFERENCES platform.tenants(id),   -- one current subscription per tenant
  plan_id bigint NOT NULL REFERENCES platform.plans(id),
  provider text NOT NULL DEFAULT 'razorpay',
  provider_customer_id text, provider_subscription_id text UNIQUE,
  status text NOT NULL CHECK (status IN ('trialing','active','past_due','grace','suspended',
                                         'cancelled','expired','read_only')),
  provider_status text,                        -- raw Razorpay status for diagnostics
  trial_end timestamptz, current_period_start timestamptz, current_period_end timestamptz,
  grace_end timestamptz, cancel_at_period_end boolean NOT NULL DEFAULT false, cancelled_at timestamptz,
  last_event_at timestamptz,                   -- created_at of the last applied provider event (ordering guard)
  checkout_short_url text,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON platform.subscriptions (status, current_period_end);
CREATE INDEX ON platform.subscriptions (status, trial_end);

CREATE TABLE platform.invoices (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id text NOT NULL REFERENCES platform.tenants(id),
  subscription_id bigint REFERENCES platform.subscriptions(id),
  provider_invoice_id text NOT NULL UNIQUE,
  amount_minor bigint NOT NULL, currency char(3) NOT NULL,
  status text NOT NULL,                        -- issued | paid | partially_paid | cancelled | expired
  issued_at timestamptz, paid_at timestamptz, short_url text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE platform.billing_events (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  provider text NOT NULL DEFAULT 'razorpay',
  event_id text NOT NULL,                      -- x-razorpay-event-id
  event_type text NOT NULL,
  provider_created_at timestamptz,             -- payload.created_at (covered by the signature)
  signature_valid boolean NOT NULL,
  payload jsonb,                               -- stored only if signature_valid; PII-minimized
  received_at timestamptz NOT NULL DEFAULT now(),
  processed_at timestamptz, outcome text, error text,
  UNIQUE (provider, event_id)
);
```

No card/UPI/bank details are ever stored. Razorpay's hosted checkout collects them.

## 3. Lifecycle

```
             trial ends,                     charge fails            grace_end passes /
 [trialing] ──no payment──▶ [read_only*]    [active] ──(pending)──▶ [past_due → grace] ──(halted)──▶ [suspended]
     │ first charge                ▲            ▲  │ charged                │ charged                  │ charged / manual
     └──(authenticated/activated)──┼────────────┘  └──(cancelled)──▶ [cancelled]──period end──▶ [read_only]
                                   │                                                                  │
                                   └───────────────── reactivated (charged / super admin) ◀────────────┘
 [read_only] ──READ_ONLY_DAYS──▶ [pending_deletion] ──RETENTION──▶ [deleted]  (DROP SCHEMA after final export)
```
\* Trial expiry without payment goes to `read_only` (data visible, writes blocked) instead of a hard lock, so owners can still subscribe. Configurable.

| Tenant status | Login/refresh | Reads | Writes (POS, sales…) | Billing actions |
|---|---|---|---|---|
| trialing, active | ✔ | ✔ | ✔ | ✔ |
| past_due / grace | ✔ + banner | ✔ | ✔ | ✔ |
| suspended | **✘** (`403 TENANT_SUSPENDED`), refresh tokens rejected | ✘ | ✘ | payment via emailed link (Q-11) |
| cancelled (until period end) | ✔ | ✔ | ✔ | resubscribe |
| read_only | ✔ + banner | ✔ | ✘ `403 TENANT_READ_ONLY` | resubscribe |
| pending_deletion | ✘ | ✘ | ✘ | super admin can restore |

Event → transition mapping (applied only if `event.created_at >= subscriptions.last_event_at`, otherwise logged as `stale_ignored`):

| Razorpay event | Transition |
|---|---|
| `subscription.authenticated` | record provider ids; stays `trialing` (or → `active` if no trial) |
| `subscription.activated` | → `active`; set period |
| `subscription.charged` | → `active`; extend `current_period_end`; clear grace; upsert invoice; "renewal success" notice (not on the first charge) |
| `subscription.pending` | → `past_due`; `grace_end = now + GRACE_DAYS` (only set once per incident) → "payment failed" notice |
| `subscription.halted` | → `suspended` if `now ≥ grace_end`, else stay `grace` (job suspends at `grace_end`) |
| `subscription.cancelled` | → `cancelled`; `read_only` at period end (job) |
| `subscription.completed` | → `expired` → `read_only` |
| `subscription.paused` / `resumed` | → `read_only` / `active` |
| `subscription.updated` | update plan/period fields only |
| `invoice.paid`, `invoice.expired` | upsert invoice |
| `payment.failed` | log only (the subscription events drive state) |

All transitions go through one function `billing.Apply(tx, sub, event)` with a table-driven transition matrix, unit-tested for every (state, event) pair, including illegal ones.

**Plan limits** (server-side, from the active plan, cached 60 s): stores, users, products, emails/day, storage. Checked in the service layer inside the tenant transaction, with `pg_advisory_xact_lock(hashtextextended('<tenant>:limit:<resource>',0))` so two concurrent creates can't both pass. Over limit → `403 PLAN_LIMIT_REACHED`. Downgrade below current usage: allowed, but creation stays blocked until usage is under the limit. Nothing is deleted. Feature flags (`reports_export`, `public_shop`, `tenant_smtp`, `custom_domain`) → `403 FEATURE_NOT_IN_PLAN`.

## 4. Webhook `/api/platform/webhooks/razorpay`

Served only on the admin host (Razorpay is configured with `https://admin.<BASE_DOMAIN>/api/platform/webhooks/razorpay`).

1. Rate limit (per source IP, 600/min) and body cap 256 KiB. Optionally restrict to Razorpay's published webhook IPs at the proxy.
2. Read the **raw body** bytes before any JSON parsing.
3. `expected = hex(HMAC-SHA256(RAZORPAY_WEBHOOK_SECRET, rawBody))`; compare with the `X-Razorpay-Signature` header using `hmac.Equal` (constant time). Missing/invalid → `400`, store a `billing_events` row with `signature_valid=false` and **no payload**, increment `pos_webhook_signature_failures_total` (alert).
4. Dedupe: `INSERT INTO billing_events (event_id = header x-razorpay-event-id, …) ON CONFLICT DO NOTHING RETURNING id`. No row returned → duplicate → `200` immediately (Razorpay may deliver the same event more than once).
5. Staleness: Razorpay's signature has no timestamp, but `payload.created_at` is inside the signed body. Events older than `WEBHOOK_MAX_AGE` (72 h) are stored with outcome `stale` and **not applied**; the reconcile job pulls the truth from the API instead. Out-of-order events are handled by the `last_event_at` guard.
6. Apply in one platform transaction: lock subscription `FOR UPDATE`, `billing.Apply`, update `tenants.status`, write `platform_audit_log` (actor `system:razorpay`), enqueue notifications (outbox in the same tx), `NOTIFY tenant_changed`.
7. Respond `200` quickly. Heavy work (emails) is async. On internal error → `500` so Razorpay retries; the event row stays with `processed_at IS NULL` and the retry is picked up idempotently.

**Reconcile job** (hourly, and on demand): for subscriptions in non-terminal states and any with unprocessed events, `FetchSubscription` → apply as a synthetic event with the provider's timestamps. It fixes missed or stale webhooks. Drift found is reported in the Super Admin billing view.

## 5. Checkout flow (tenant owner)
1. Owner (`billing:manage`) clicks Subscribe/Change plan → `POST /api/billing/checkout {planCode}`.
2. Server (platform tx, tenant from host) creates the Razorpay customer if missing, then a subscription for `plans.razorpay_plan_id` (`notes: {tenant_id}`), stores `provider_subscription_id` + `short_url`, returns `{url: short_url}`.
3. Browser goes to Razorpay hosted page. Returning to the app shows "processing". The SPA polls `GET /api/billing/subscription`; **status changes only when the webhook arrives**.
4. For suspended tenants (no login), the "payment failed / suspended" emails contain the subscription `short_url` (Razorpay-hosted; no app session needed).

## 6. Notifications (scheduled job, deduplicated)

Daily job at `BILLING_NOTIFY_HOUR` (default 03:30 UTC = 09:00 IST). Each notice writes `platform.notification_log (tenant_id, kind, period_key)` with a **unique** constraint in the same transaction as the outbox row, so re-runs and multiple workers can't send duplicates.

| Kind | When | period_key | Channel |
|---|---|---|---|
| `trial_ending_7/3/1` | `trial_end - now` crosses 7/3/1 days | `trial_end::date` | email (owner + billing email) + banner |
| `subscription_ending_7/3/1` | `cancel_at_period_end` and `current_period_end` crosses 7/3/1 | `current_period_end::date` | email + banner |
| `renewal_success` | `subscription.charged` (not first) | invoice id | email |
| `payment_failed` | `subscription.pending` | grace incident start date | email + banner |
| `grace_ending` | 1 day before `grace_end` | `grace_end::date` | email + banner |
| `suspended` | → suspended | suspension timestamp | email |
| `reactivated` | suspended/read_only → active | reactivation timestamp | email + banner |

- Sent via **platform SMTP** to the owner (and billing contact).
- In-app banner: `GET /api/notifications/banners` merges the billing banner (derived from status) and platform announcements; per-user dismissal (except for critical billing banners).
- Super Admin: "Expiring soon" (trials/periods ending ≤ 7 d) and "Past due" lists, plus a manual reminder action (rate-limited 1/day/tenant, audited).

## 7. Super Admin billing capabilities
Assign/change plan (provider `ChangePlan`, or local-only for comped plans), extend trial (`trial_end += n`, audited, reason required), apply grace (`grace_end = …`), cancel (step-up), view invoices + Razorpay status, run reconcile. Every action → platform audit log with before/after.

## 8. Security checklist
- Raw-body HMAC-SHA256, `hmac.Equal`, secret from env only, ≥ 16 random chars enforced at startup.
- Idempotency by `x-razorpay-event-id` (unique), out-of-order guard, staleness window, rate limit, body cap.
- Webhook endpoint has no auth cookies, no CORS, and does not reflect input.
- Status changes only from verified events or provider pulls. `POST /api/billing/checkout` never activates anything.
- Amounts in integer paise in platform tables; no floats.
- Tests (Phase 3): bad/missing signature, body tampered after signing, duplicate event, out-of-order events, stale event, each lifecycle transition, grace → suspension job, notification dedupe across two workers, plan-limit races (parallel creates at the limit).

## 9. Phase 2 minimum
Tables + migrations; `BillingProvider` interface + Razorpay create/fetch/cancel + `VerifyWebhook`; webhook endpoint with all checks; transition matrix; plan-limit enforcement (stores, users, products, emails/day); scheduled notifications (trial/subscription ending, payment failed, suspended, reactivated); Super Admin: assign plan, extend trial, expiring/past-due lists; tenant: subscription view + checkout link + banners. Deferred: proration UI, coupons, GST invoices for the SaaS itself (Razorpay invoices cover it initially), custom domains.
