# Razorpay test-mode checklist

Status: **not run yet** (no Razorpay keys). Step 16 was built and tested against the fake provider (`internal/billing/fake.go`), which sends signed events shaped like Razorpay's. The real client (`internal/billing/razorpay.go`) follows Razorpay's documentation but has never talked to Razorpay. Run this list once test keys exist and write the results in the **Result** lines.

The questions come from BILLING_DESIGN §10. R1 to R6 change code paths and must be answered before billing goes live. R7 to R11 are listed at the end.

## Setup

1. Razorpay dashboard, **test mode**: copy the key ID (`rzp_test_…`) and key secret.
2. Create a plan: monthly, ₹1 (100 paise) is enough. For R2, R3 and R10, also try the shortest period the dashboard accepts. Note each plan ID (`plan_…`).
3. Make the dev stack reachable from the internet: Razorpay must reach the webhook. Use a tunnel (cloudflared or ngrok) to `https://admin.<BASE_DOMAIN>`.
4. Dashboard → Webhooks → add `https://<tunnel host>/api/platform/webhooks/razorpay` with a new random secret of at least 16 characters. Enable: `subscription.authenticated`, `activated`, `charged`, `pending`, `halted`, `cancelled`, `completed`, `paused`, `resumed`, `updated`, plus `invoice.paid`, `invoice.expired` and `payment.failed`.
5. In `deploy/.env` (never commit it): `BILLING_PROVIDER=razorpay`, `RAZORPAY_KEY_ID`, `RAZORPAY_KEY_SECRET`, `RAZORPAY_WEBHOOK_SECRET`. Then `make up`.
6. Super Admin console: `POST /api/platform/plans` with `razorpayPlanId` and `isPublic: true`, or `PATCH` an existing plan to set it (it can be set only once).
7. Sign in to a test tenant as its owner, open the billing page, then `POST /api/billing/checkout {"planCode": …}`. This first call also checks that customer and subscription creation work.

What to look at after each action:

```sql
-- every webhook we received, in order, and what we did with it
SELECT received_at, event_type, event_id, provider_created_at, outcome
FROM platform.billing_events ORDER BY id DESC LIMIT 20;

-- the subscription as we see it
SELECT status, provider_status, current_period_end, grace_end, last_event_at
FROM platform.subscriptions WHERE tenant_id = '<id>';
```

Also look at the app logs (`docker compose … logs app`): provider call failures are logged there (the client only gets a 503), and webhook signature failures are counted in `pos_webhook_signature_failures_total`.

## Client calls (check during setup)

| # | Check | Expected | Result |
|---|---|---|---|
| C1 | Checkout creates the customer (`POST /v1/customers`, `fail_existing=0`) and the subscription (`POST /v1/subscriptions`, `notes.tenant_id`, `start_at` at trial end) | 200; the stored `provider_subscription_id` starts with `sub_`; the returned `short_url` opens Razorpay's payment page | |
| C2 | A second checkout before paying | Reuses the stored customer and creates a new subscription; the first stays `created` at Razorpay (clean-up can wait for reconcile). Separately: a new tenant whose email already has a Razorpay customer gets that customer back (`fail_existing=0`) | |
| C3 | Console cancel (`POST /v1/subscriptions/:id/cancel`, `cancel_at_cycle_end` 0 and 1) | 200 both ways | |
| C4 | Webhook payload shape: `payload.subscription.entity`, `payload.payment.entity.invoice_id` on `subscription.charged`, `notes` as `{}` or `[]` | Parsed (`outcome` is not `rejected`) | |

## R1 to R6

### R1: can a `halted` subscription be revived by paying? (most important)

Why: a suspended tenant cannot sign in. The suspended email must carry a link that brings it back.

What is built: the **revivable** branch. When the tenant is suspended, the email's pay link is the newest unpaid invoice's `short_url`, or else the subscription's checkout link (`Service.Notify`, `pay_url`). A verified `subscription.activated` or `subscription.charged` reactivates a suspended tenant, unless an admin suspended it. (`resumed` only lifts read-only, which is what `paused` sets.)

Steps:
1. Subscribe with a test card that later fails, or use a UPI failure VPA. Let the retries run out until `subscription.halted` arrives. R3 tells how long that takes.
2. Check that the tenant is `suspended` (after `BILLING_GRACE_DAYS`, or set `grace_end` in the past and let the hourly job run), and that the email arrived with a link.
3. Pay with that link: first the invoice `short_url`, then on another subscription the subscription's own `short_url`.
4. Record which events arrive and in what order, and the final Razorpay status.

| Outcome | What to do |
|---|---|
| Paying the link sends the subscription back to `active` and fires `activated` or `charged` | Nothing to build. Record which link works. If only one of them does, make `pay_url` prefer that one. If only `resumed` fires, add `Suspended: Active` to its row in the transition matrix |
| It stays `halted`, or the link cannot be paid | Build the **not revivable** branch (BILLING_DESIGN §10): when suspending, the lifecycle job creates a new subscription on the same plan, cancels the halted one, and emails the new `short_url`. Its first charge reactivates the tenant. This is about a day of work, tests included |

Result:

### R2: period dates after revival or a new subscription

Why: `current_period_end` drives read-only and reminders.

Steps: after R1, compare `current_start` and `current_end` in the event with what we stored. Does the billing date move to the payment day?

Expected with the current code: we store whatever the newest subscription event says (`Apply` copies `current_end`). Code changes only if Razorpay sends no period at all on the reviving event.

Result:

### R3: retries before `pending` → `halted`

Why: our grace period (`BILLING_GRACE_DAYS`, 7 days by default) starts at the first `subscription.pending`, or at `halted` if no `pending` came first. `payment.failed` alone changes nothing. Grace should not run out long before Razorpay stops retrying.

Steps: from a failing payment, record when each `payment.failed`, `subscription.pending` and `subscription.halted` arrives. Also check whether test mode lets you speed this up.

Expected: Razorpay's documentation describes several retries over a few days. If `halted` comes later than 7 days after the first failure, raise `BILLING_GRACE_DAYS`. Today a `halted` within grace keeps the grace end date, and a `halted` after it suspends at once.

Result:

### R4: trial with a future `start_at`

Why: checkout during a trial creates the subscription with `start_at` = trial end, and the trial keeps running.

Steps: check out from a tenant that is still in trial. Authorise the mandate with a card, then (separately) with UPI Autopay.

Record: does `subscription.authenticated` arrive at mandate setup? Is there a small authorisation charge, and is it refunded? Does `activated` or `charged` come at `start_at`?

Expected with the current code: `authenticated` never changes our status; the tenant stays `trialing`, and the first charge makes it `active`. If no `activated` or `charged` arrives, the hourly job makes the tenant read-only 24 h after the trial end (`TrialChargeWait`).

Result:

### R5: event ID header and `created_at`

Why: duplicates are found by `X-Razorpay-Event-Id`. Out-of-order events are found by `created_at`.

Steps: look at `billing_events.event_id` and `provider_created_at` for every event received during R1 to R4. Then redeliver one event from the dashboard.

Expected: every `event_id` starts with `evt_` (an ID starting with `body:` means the header was missing, and the body-hash fallback was used). `provider_created_at` is never empty. The redelivered event gets `200` and no new row.

If the header is missing: the fallback already dedupes identical bodies. Record that here; nothing else to change.

If `created_at` is missing: the staleness guard cannot order events. Fall back to fetching the subscription (`FetchSubscription`) before applying. That is a small change in `HandleWebhook`.

Result:

### R6: webhook retries after a `500` or timeout

Why: our webhook answers `500` ("try again") when the database fails, so that Razorpay retries. `400` means a bad signature and `404` means billing is off; Razorpay should not retry those.

Steps: stop the database container (or point the tunnel at a closed port), trigger an event, and watch the dashboard's delivery log. Start the database again.

Record: how often and for how long Razorpay retries, and whether the webhook gets disabled after repeated failures, and after how many.

Expected: retries continue for at least several hours. If Razorpay disables webhooks after repeated failures, we need reconcile (a periodic `FetchSubscription` for every live subscription, deferred in the step 16 report) and an alert on webhook silence before going live.

Result:

## R7 to R11 (not needed before go-live)

| # | Question | Depends on it |
|---|---|---|
| R7 | Are plan changes (`PATCH /subscriptions/:id`) allowed for UPI Autopay and eMandate, or only for cards? | Plan changes (deferred): `ChangePlan` or cancel plus new subscription. Today the console refuses a local plan while the provider bills |
| R8 | `cancel_at_cycle_end=1`: which event fires, and when? Does the subscription stay `active` until the end? | We set `cancel_at_period_end` at once and wait for `subscription.cancelled` |
| R9 | Does Razorpay email customers itself (`customer_notify=1` is sent today), and can that be turned off? | Double emails next to ours |
| R10 | Shortest plan period in test mode | Watching real renewals |
| R11 | Are `subscription.paused` and `resumed` available on the account? | Mapped today: paused → read-only, resumed → active |
