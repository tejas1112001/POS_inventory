# Email & Receipts Design

| | |
|---|---|
| Status | Phase 1 design, revised 2026-10-15 after review; awaiting approval |
| Principles | A failed email never fails or rolls back a sale · outbox row commits with the sale · at-least-once delivery, exactly-one *logical* receipt per sale · tenant data only from the tenant's own schema |
| Language | **English only**: every email, template and receipt. No language setting, translation files or i18n libraries. Currency and number format come from tenant settings |

---

## 1. Interfaces

```go
// internal/mailer
type Message struct {
    From, ReplyTo  Address           // validated, no CR/LF
    To             []Address          // ≤ 10
    Subject        string             // ≤ 200 chars, no CR/LF
    HTMLBody, TextBody string
    Attachments    []Attachment       // receipts: 1 PDF ≤ 2 MiB
    MessageID      string             // "<idempotency_key@mail-domain>"
    Headers        map[string]string  // allow-list only (List-Unsubscribe for marketing)
}

type EmailSender interface {
    Send(ctx context.Context, m Message) (providerID string, err error)  // err is classified
}

// Classified errors drive retry policy
var (
    ErrTransient = errors.New("transient")   // 4xx SMTP, timeouts, connection reset → retry
    ErrPermanent = errors.New("permanent")   // 5xx recipient/content → no retry (dead)
    ErrAuth      = errors.New("auth")        // 535 etc. → mark sender config failing, maybe fallback
    ErrQuota     = errors.New("quota")       // Gmail daily limit (421/550 5.4.5) → retry later / fallback
)
```

Implementations: `smtpSender` (wneessen/go-mail) only. **Platform mail (Q-13)** goes over SMTP to Amazon SES in Mumbai (`email-smtp.ap-south-1.amazonaws.com:587`, STARTTLS) or Brevo (`smtp-relay.brevo.com:587`); switching is an env change. Provider-API senders can be added behind the same interface later. `fakeSender` for tests, Mailpit in dev and CI. The display name comes from `PLATFORM_NAME`; no product name is hard-coded.

## 2. Two sending identities

| | Platform SMTP | Tenant SMTP |
|---|---|---|
| Configured by | Operator via env / secret manager (SES ap-south-1 or Brevo SMTP) | Tenant owner/admin in **their** dashboard (`email_settings:edit`) |
| Stored | Env only | `t_<id>.tenant_email_settings`, password envelope-encrypted |
| Used for | Welcome, email verification, invite/set password, password reset, trial ending, subscription ending/expired, payment failed, grace, suspension, reactivation, announcements, **fallback** for tenant receipts | That tenant's receipts, order confirmations, customer emails |
| Never used for | — | Platform mail (never) |
| Visibility | Never exposed to tenants | Password write-only; never returned, logged, or shown to platform staff |

### 2.1 Sender selection for tenant mail

```
tenant SMTP enabled && status in (ok, unverified-but-test-passed) ──▶ tenant SMTP
     │ ErrAuth / ErrQuota / 3 consecutive transient failures
     ▼
fallback_to_platform (setting, default true) && platform cap for tenant not exceeded ──▶ platform SMTP (fallback identity)
     │ otherwise
     ▼
retry later / dead-letter, tenant sees the status in Settings ▸ Email
```

### 2.2 From-address safety
- **Tenant SMTP**: `From` must be the authenticated mailbox. If `username` is an email address, `from_email` must equal it (Gmail/Workspace rewrite otherwise); a verified alias is allowed only after a successful test send to that alias. Display name = sanitized `from_name` or business name.
- **Platform fallback**: `From: "<Business Name> via <PLATFORM_NAME>" <MAIL_FROM_ADDRESS>`, `Reply-To: <tenant settings email>` (validated), never the tenant's domain in `From` (no spoofing; SPF/DKIM/DMARC alignment stays with the platform domain).
- Display names: Unicode allowed, CR/LF/control characters stripped, quotes escaped by go-mail, ≤ 78 chars.

## 3. Tenant SMTP settings

```sql
CREATE TABLE tenant_email_settings (
  id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  enabled boolean NOT NULL DEFAULT false,
  host text NOT NULL CHECK (host ~ '^[A-Za-z0-9.-]{1,253}$'),
  port int NOT NULL CHECK (port IN (465, 587)),
  tls_mode text NOT NULL CHECK (tls_mode IN ('implicit','starttls')),   -- 465 ⇒ implicit, 587 ⇒ starttls
  username text NOT NULL,
  password_ciphertext bytea NOT NULL,      -- AES-256-GCM(secret, DEK, aad)
  password_dek_wrapped bytea NOT NULL,     -- AES-256-GCM(DEK, KEK[key_id])
  key_id text NOT NULL,
  from_name text, from_email citext NOT NULL,
  fallback_to_platform boolean NOT NULL DEFAULT true,
  daily_cap int NOT NULL DEFAULT 300,
  status text NOT NULL DEFAULT 'unverified' CHECK (status IN ('unverified','ok','failing','disabled')),
  last_test_at timestamptz, last_success_at timestamptz, last_error_code text, consecutive_failures int NOT NULL DEFAULT 0,
  updated_by bigint REFERENCES users(id), updated_at timestamptz NOT NULL DEFAULT now()
);
```

- **Envelope encryption** (`internal/secrets`): per secret, a random 32-byte DEK; `ciphertext = AES-256-GCM(password, DEK, nonce, aad = tenant_id || "smtp_password" || key_id)`. Binding to the tenant ID means a ciphertext copied into another tenant's row fails to decrypt. The DEK is wrapped with the KEK from `MASTER_KEYS[key_id]` (env today, KMS `Encrypt/Decrypt` later behind the same interface). Rotation re-wraps DEKs (`posctl secrets rotate`). KEKs never touch the DB or logs.
- **API**: `GET /api/settings/email` → everything except the password, plus `passwordSet: true|false`. `PUT` accepts `password` (write-only; omitted = keep). `DELETE` disables and wipes. Every change → audit log (`smtp.updated`, no secret values).
- **Test**: `POST /api/settings/email/test {to}` sends synchronously (30 s cap) to an address the tenant controls (`to` defaults to the acting user's email; arbitrary `to` is rate-limited to 5/hour). The result updates `status`, and the UI shows a status indicator (OK / Failing: last error class / Unverified).

### 3.1 SSRF and abuse protection

| Control | Implementation |
|---|---|
| Ports | Only 465 (implicit TLS) and 587 (STARTTLS). **Port 25 rejected**: it is MTA-to-MTA relay, usually blocked by clouds, and not an authenticated submission port |
| Host | DNS name or literal IP; resolved by the app's resolver |
| IP filter | **After resolution**, reject loopback, private (RFC 1918), CGNAT (100.64/10), link-local (169.254/16 incl. cloud metadata), multicast, unspecified, IPv6 ULA (fc00::/7), link-local (fe80::/10), IPv4-mapped forms of all of these, and the app's own egress/VPC ranges (`SMTP_DENY_CIDRS`) |
| DNS rebinding | Resolve once, validate, then dial **that IP** (custom `DialContext` passed to go-mail); TLS `ServerName` = original host, so certificate verification still applies |
| TLS | Mandatory: implicit TLS on 465, `TLSMandatory` STARTTLS on 587 (refuse if the server doesn't offer STARTTLS); TLS ≥ 1.2; system roots; no `InsecureSkipVerify` |
| Timeouts | Dial 10 s, TLS handshake 10 s, whole send 30 s, context-cancelled |
| Auth | PLAIN/LOGIN only over TLS (go-mail enforces); no OAuth in v1 (SUGGESTION: Google OAuth2 XOAUTH2 later, which removes App Passwords) |
| Rate limits | Per tenant: daily cap (plan-dependent, default 300), 30/min burst; per recipient: 5 receipts/day for resends; test endpoint 5/hour |
| Egress | **SUGGESTION**: in production, route tenant SMTP through an egress proxy/NAT with a firewall that only allows outbound 465/587 |

### 3.2 Header-injection and template safety
- Addresses parsed with `net/mail.ParseAddress`; reject CR, LF, NUL and other control characters in any address, display name, subject and custom header *before* building the message. go-mail encodes headers (RFC 2047) as a second layer.
- Templates: `html/template` (contextual auto-escaping) for HTML, `text/template` for plain text. Tenant-customizable parts (header text, footer text, logo) are **data**, never template source: tenants cannot inject template syntax or raw HTML. Logo = a tenant file ID rendered as an `https://<slug>.<BASE_DOMAIN>/files/t/...` URL (or embedded CID for the PDF).
- Links in emails are built from `PUBLIC_SCHEME://<slug>.<BASE_DOMAIN>`, never from the request Host.

### 3.3 Gmail notes (documented for tenants in the Settings screen)
- Requires **2-Step Verification** and an **App Password** (the normal password fails with 535). Use `smtp.gmail.com:587` (STARTTLS) or `:465`.
- Daily sending limits (consumer accounts far lower than Workspace); bursts trigger temporary blocks (421 / 5.4.5). We treat these as `ErrQuota`: back off and fall back to platform SMTP if enabled.
- App Passwords are revoked when the Google password changes, so `status` flips to `failing` and the tenant sees it.
- For the **platform** (decided, Q-13): Amazon SES (ap-south-1) or Brevo over SMTP. Both give higher limits, bounce/complaint handling and DKIM on the platform domain. SES starts in sandbox mode (verified recipients only) until production access is requested; the go-live checklist covers it (OPERATIONS §9).

## 4. Transactional outbox

### 4.1 Tables (same shape in `platform` and in every tenant schema)

```sql
CREATE TABLE email_outbox (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  idempotency_key text NOT NULL UNIQUE,          -- e.g. 'sale:123:receipt', 'sale:123:receipt:resend:2'
  kind text NOT NULL CHECK (kind IN ('receipt','receipt_resend','test','customer','platform_notice')),
  to_addr citext NOT NULL, to_name text,
  template text NOT NULL, template_data jsonb NOT NULL,   -- IDs and small fields only; no secrets, no full bodies
  attachment_receipt_id bigint,                  -- tenant: receipts.id
  sender_policy text NOT NULL DEFAULT 'tenant_then_platform',
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','sending','sent','failed','dead','cancelled')),
  attempts int NOT NULL DEFAULT 0, max_attempts int NOT NULL DEFAULT 8,
  next_retry_at timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz, leased_by text,
  last_error_class text, last_error text,        -- sanitized, truncated to 500 chars, no credentials
  sent_via text CHECK (sent_via IN ('tenant','platform')), provider_message_id text,
  created_at timestamptz NOT NULL DEFAULT now(), sent_at timestamptz
);
CREATE INDEX email_outbox_due ON email_outbox (next_retry_at) WHERE status IN ('pending','failed');
```

### 4.2 Writing (same transaction as the business change)

Inside `POST /api/sales` (tenant tx):
1. Insert sale + items + stock updates + ledger.
2. Insert `receipts(sale_id, sale_version=1, status='pending')`.
3. If `receipt_email` (customer email or cashier-entered) is set: `INSERT INTO email_outbox (idempotency_key='sale:<id>:receipt', …) ON CONFLICT (idempotency_key) DO NOTHING`.
4. An `AFTER INSERT … FOR EACH STATEMENT` trigger on `email_outbox` runs `"t_<id>".mark_outbox_pending()`, which upserts `platform.mail_dispatch(tenant_id)` and calls `pg_notify('mail_dispatch', tenant_id)`. Both are transactional: they happen **only if the sale commits**.
5. Commit. If the commit fails, sale, receipt and email all vanish together. If the commit succeeds, the email is durably queued even if the process crashes a millisecond later.

**How the function knows the tenant (revised).** The first draft derived the tenant from `current_schema()`. That value comes from the caller's `search_path`, so it is caller-controlled; and a `SECURITY DEFINER` function must pin its own `search_path`, which would make `current_schema()` return `pg_catalog` anyway. The review asked for `current_user` instead. Inside a `SECURITY DEFINER` function, though, `current_user` is the **function owner** (`pos_owner`), not the caller. The caller's role (`t_<id>_rw`) is visible only to a `SECURITY INVOKER` function, and that function would then need write access to `platform.mail_dispatch`, which we don't want to grant.

So the tenant identity is bound to the **role and schema** when the function is created, not read at run time:

```sql
-- Created by provisioning step 5 and re-applied by the migration runner's post-step, from the
-- validated tenancy.TenantID (^[a-z2-7]{10}$) through internal/db/ident.go. Never from current_schema(),
-- current_user, app.tenant_id or any request value.
CREATE OR REPLACE FUNCTION "t_ab12cd34ef".mark_outbox_pending() RETURNS trigger
  LANGUAGE plpgsql SECURITY DEFINER
  SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
  INSERT INTO platform.mail_dispatch (tenant_id) VALUES ('ab12cd34ef')
  ON CONFLICT (tenant_id) DO UPDATE
    SET next_attempt_at = LEAST(platform.mail_dispatch.next_attempt_at, now());
  PERFORM pg_notify('mail_dispatch', 'ab12cd34ef');
  RETURN NULL;
END $$;
ALTER FUNCTION "t_ab12cd34ef".mark_outbox_pending() OWNER TO pos_owner;
REVOKE ALL ON FUNCTION "t_ab12cd34ef".mark_outbox_pending() FROM PUBLIC;
CREATE TRIGGER email_outbox_mark AFTER INSERT ON "t_ab12cd34ef".email_outbox
  FOR EACH STATEMENT EXECUTE FUNCTION "t_ab12cd34ef".mark_outbox_pending();
```

- The function lives in the tenant's own schema, is owned by `pos_owner` (the tenant role cannot replace it, since it has no `CREATE` on its schema), and fires only from that schema's `email_outbox`. Only `t_<id>_rw` can write to that table, so in effect the tenant comes from the role, as the review intended, but enforced by grants instead of a run-time lookup.
- Trigger functions cannot be called directly, and `EXECUTE` is revoked from `PUBLIC`.
- Why a `SECURITY DEFINER` function instead of granting the tenant role `INSERT` on `platform.mail_dispatch`: the tenant role gets one narrowly scoped capability ("tell the platform I have pending mail") instead of a table privilege. Abuse is harmless: the function can only mark its own tenant, and a spurious mark just triggers an empty check.
- Tests (step 3): an outbox insert in tenant A marks only A; tenant A's role cannot insert into `platform.mail_dispatch` or execute B's function; changing `search_path`/`app.tenant_id` inside the transaction has no effect on which tenant is marked; the drift check flags a function whose literal does not match its schema.

### 4.3 Cross-schema worker

```
┌─ LISTEN mail_dispatch ─┐   ┌─ sweeper (every 60 s) ───────────────┐
│ instant wake-up        │   │ SELECT tenant_id FROM platform.mail_dispatch
│ (dedicated connection, │   │ WHERE next_attempt_at <= now()       │
│  TENANCY §5.5)         │   │                                      │
└──────────┬─────────────┘   │                                      │
           └──────────┬──────┴──────────────────────────────────────┘
                      ▼
      River job  DeliverTenantMail{tenant_id}   (unique per tenant while pending/running)
                      ▼
   InTenantTx(tenant, short):   -- tenant-scoped session: SET LOCAL ROLE t_x_rw; search_path "t_x"
     SELECT … FROM email_outbox
      WHERE status IN ('pending','failed') AND next_retry_at <= now()
         OR (status = 'sending' AND lease_until < now())          -- crashed worker recovery
      ORDER BY id LIMIT 20
      FOR UPDATE SKIP LOCKED;
     UPDATE … SET status='sending', lease_until=now()+2 min, leased_by=<worker id>, attempts=attempts+1
   COMMIT                                  -- no SMTP inside a DB transaction
                      ▼
   for each row: load tenant data (same tenant session), render, pick sender, Send()
                      ▼
   InTenantTx: UPDATE status='sent' | 'failed' (next_retry_at = now() + backoff) | 'dead'
              (only WHERE id=$1 AND leased_by=<worker id>, so a stale worker can't overwrite)
                      ▼
   InPlatformTx: if no due rows remain → DELETE FROM platform.mail_dispatch WHERE tenant_id=$1
                 else next_attempt_at = min(next_retry_at)
```

- **Isolation**: a job handles exactly one tenant. All reads (sale, items, settings, logo, SMTP secret) happen inside that tenant's session. The rendering function receives a `TenantTx`, so it *cannot* read another schema (type + DB role). Workers never batch rows from two tenants.
- **No double send between workers**: `FOR UPDATE SKIP LOCKED` + the `sending` lease + the `leased_by` guard on completion. A worker crash *after* SMTP accepted but *before* marking `sent` leads to one retry after the lease expires. That is an inherent at-least-once property of SMTP. Mitigation: a stable `Message-ID = <idempotency_key@platform-mail-domain>`, so many receiving systems de-duplicate. Documented, and tested in Phase 3 (two workers, one crash point).
- **Backoff**: `min(2^attempt × 30 s, 6 h)` + jitter; `max_attempts = 8` (~1.5 days) → `dead`. Permanent errors → `dead` immediately. Dead-letter rows stay visible (tenant: Settings ▸ Email ▸ failed messages; platform: count only, no content) and can be retried manually.
- **Fairness**: per-tenant batch size 20 and River queue concurrency (e.g. 10 tenants in parallel), so one tenant's backlog can't starve others. Per-tenant daily cap is enforced here, not at enqueue (the sale must never fail because of mail).
- Platform mail uses the same worker type on `platform.email_outbox` with a platform session.

### 4.4 Failure handling summary

| Failure | Effect on sale | Email |
|---|---|---|
| DB error while writing the outbox row | Whole sale rolls back (it is one tx, the only coupling, by design) | — |
| SMTP down / timeout | None | Retries with backoff |
| Tenant SMTP auth fails | None | `status=failing`, fallback to platform if enabled, tenant notified in-app |
| Recipient rejected (5xx) | None | `dead`, shown in UI |
| PDF render fails | None | Retries; after 3 failures the email is sent **without** attachment but with a download link |
| Worker crash mid-send | None | Lease expiry → one retry (possible duplicate, Message-ID dedupe) |

## 5. Receipts

### 5.1 Order-completed flow
1. Sale committed (POS or back-office) with `receipts` row `pending` (+ outbox row if there's an email).
2. River job `RenderReceipt{tenant_id, receipt_id}` (enqueued by the same `mark_outbox_pending`-style trigger or by the API right after commit; the sweeper also picks up `pending` receipts older than 1 min, so nothing is lost if enqueueing after commit fails).
3. Worker opens a tenant session, loads sale + items (with **snapshotted** names/SKUs/tax) + `settings` + store, renders the PDF (maroto v2), stores it at `tenants/<tenant_id>/receipts/<yyyy>/<mm>/<sale_id>-v<version>.pdf` with SHA-256, and marks `rendered`.
4. The email worker attaches the stored PDF (rendering it on demand if not ready).
5. Sale edit → `version++` → new receipt row/file. Old files are kept (history). Void → receipt marked "VOID" on re-render. No email unless resend.

**One receipt email per sale**: idempotency key `sale:<id>:receipt` (unique). Resends use `sale:<id>:receipt:resend:<n>`, rate-limited.

### 5.2 Endpoints
- `GET /api/sales/:id/receipt` → `application/pdf` (`Content-Disposition: attachment`), permission `receipts:view` + own-sale rule. If not rendered yet, renders synchronously (5 s cap).
- `POST /api/sales/:id/receipt/resend {email?}` → `202`. Permission `receipts:resend` + own-sale rule; a new email address is saved on the sale (`receipt_email`) and audited.
- Files are streamed through the app (tenant check on host + permission), never via public bucket URLs. If S3 presigned URLs are used later, they are short-lived (60 s) and generated only after the same checks.

### 5.3 Content
Tenant business name, logo, legal name, tax registration no. (GSTIN) and address from `settings`; store name/address/phone; receipt/reference no., date/time in tenant timezone, cashier name, customer name (and email if provided); items: name, variant attributes, SKU, qty, unit price, discount, tax rate/amount (CGST/SGST or IGST split when configured), line total; subtotal, discount, tax total (per rate summary), shipping, **total**, paid, tendered, change, due; payment method; header/footer text; "Thank you" line; for void: watermark.
- All labels and fixed text are English. Currency and number formatting come from `settings.currency` and `settings.locale` (an English number-format tag such as `en-IN` or `en-US`, used only for grouping, decimal separator and date format; no hard-coded INR). Formatting uses `golang.org/x/text/currency` + `x/text/number`; no message catalogs.
- Fonts embedded (Noto Sans + Noto Sans Devanagari, SIL OFL) so `₹` and **names that tenants type in Devanagari** (customers, products, stores) render. This is data, not translation. Whether maroto can render them correctly is the subject of the spike in §5.6.
- All data from the tenant's own schema. The PDF function signature takes `TenantTx` + `receiptID` and nothing else.

### 5.4 Email templates
- All templates are English only; there is one template per message, no per-language variants.
- `receipt.html.tmpl` / `receipt.txt.tmpl`: minimal transactional body (business name, total, date, reference, store contact, "Your receipt is attached"). Tenant-editable header/footer text and logo, safely escaped.
- Platform templates: welcome/set-password, verify email, reset password, trial ending (7/3/1), subscription ending (7/3/1), renewal success, payment failed, grace ending, suspended, reactivated, announcement.
- Template data structs are typed. Template rendering is tested with golden files, including hostile inputs (`<script>`, `{{`, CR/LF, very long names).

### 5.5 Consent
- Transactional mail (receipts, password resets, billing) is exempt from marketing consent but kept **minimal**: no promotions in receipt emails.
- Marketing-type mail (future "customer emails", promotions): requires `customers.marketing_consent = true`, includes `List-Unsubscribe` + `List-Unsubscribe-Post` (RFC 8058) and a one-click unsubscribe link (HMAC-signed token → sets consent false). Consent changes are audited. Platform announcements to tenant owners are service messages; owners can opt out of non-essential ones.

### 5.6 PDF spike: Devanagari names with maroto (early, before step 9)

**Why.** Devanagari needs complex text shaping: conjuncts (`क्ष`, `ज्ञ`, `श्र`, `त्र`), the reph (`र्` above the next consonant, as in `धर्मेंद्र`), and the pre-base vowel sign `ि`, which is typed after its consonant but drawn before it (`कि`). maroto v2 draws text through a gofpdf-style engine, which as far as I can tell maps characters to glyphs one by one with no OpenType shaping. If so, conjuncts break apart and `ि` lands on the wrong side. Receipts are English, but customer names are tenant data and are often typed in Marathi or Hindi.

**Spike** (`backend-go/spikes/pdfshaping/`, throwaway, about half a day):
1. Render a one-page receipt with maroto v2.4.3 and embedded Noto Sans Devanagari, with customer names `क्षितिज ज्ञानेश्वर कुलकर्णी`, `श्रीकृष्ण त्रिपाठी`, `धर्मेंद्र सिंह`, `किरण पाटील` and a mixed line `Total ₹1,23,456.00`.
2. Render the same names in an HTML page in Chromium as the reference.
3. Compare them side by side (a person checks; text extraction cannot judge shaping). Result and screenshots go into `docs/spikes/PDF_SHAPING.md`.

**Decision rule.**
- maroto output matches the reference → keep maroto (as planned).
- It does not → **fallback: HTML-to-PDF**. The receipt is rendered from an `html/template` (the same view-model as the email) by **Gotenberg** (Chromium behind an HTTP API) running as a separate container on the internal network only. It gets no internet egress, JavaScript is disabled, and every asset (fonts, logo) is sent inline with the request, so tenant content cannot make it fetch URLs. The Go app stays distroless; only the renderer container has Chromium. Cost: about 1 GB image, 200–500 ms per render (done in the worker, so POS is not affected), one more service to monitor.
- Pure-Go shaping (`go-text/typesetting` v0.3.5, a HarfBuzz port, or `tdewolff/canvas`, which has no tagged releases) is noted as a later option, not part of the spike.

## 6. Logging & metrics
- Logs: `outbox_id`, `tenant_id`, `kind`, `status`, `error_class`, attempt. **Never** bodies, attachments, SMTP credentials, full recipient lists (recipient domain only).
- Metrics: `pos_email_outbox_depth{scope="platform|tenant"}` (sum, no per-tenant label), `pos_email_sent_total{via,kind}`, `pos_email_failed_total{class}`, `pos_email_dead_total`, `pos_receipt_render_seconds`.
- Alerts: OPERATIONS §4.
