# Threat Model (STRIDE-lite)

| | |
|---|---|
| Status | Phase 1 design, awaiting approval |
| Top property | **Tenant isolation**: tenant A cannot read, write or infer tenant B's data |
| Method | Assets → trust boundaries → STRIDE per component → controls → tests (Phase 3) |

---

## 1. Assets

| Asset | Why it matters |
|---|---|
| Tenant business data (sales, customers incl. PII, stock, prices, costs, suppliers) | Confidentiality between competitors on one platform; DPDP-style personal data |
| Money & stock correctness | Direct financial loss; tax records |
| Credentials: user passwords, refresh tokens, impersonation codes, TOTP secrets, recovery codes | Account takeover |
| Platform secrets: JWT keys, master keys (KEK), DB passwords, SMTP, Razorpay, DNS token | Total compromise |
| Tenant SMTP passwords | Abuse of the tenant's mailbox (often a personal Gmail) |
| Super Admin accounts | Control over every tenant |
| Billing state | Revenue, wrongful suspension |
| Availability of POS at the counter | The shop cannot sell |

## 2. Trust boundaries

```
 Internet ─┬─▶ Caddy (TLS) ─▶ posd ─┬─▶ Postgres (roles per tenant)
           │                        ├─▶ Object storage (tenant prefixes)
 Razorpay ─┘ (webhooks)             ├─▶ Tenant SMTP servers (untrusted, tenant-chosen)  ◀── SSRF boundary
                                    ├─▶ Platform SMTP / Razorpay API
                                    └─▶ River workers (same binary, own pool)
 Actors: anonymous visitor · shop customer (public shop) · tenant user (owner/admin/biller/custom) ·
         malicious tenant (a legitimately paying, hostile customer) · platform support/billing/super admin ·
         compromised dependency · insider with DB access
```

The **malicious tenant** is the key adversary: authenticated, with full control of their own data, roles, SMTP settings, images, slugs, and with API access to probe isolation.

## 3. STRIDE by component

### 3.1 Host/subdomain routing & tenant resolution
| Threat | Example | Control | Test |
|---|---|---|---|
| S | Request to `b.<BASE>` with tenant A's token | `tid` claim must equal the host-resolved tenant; else 401 + security log | Isolation suite: every endpoint × swapped host |
| S | Forged `X-Forwarded-Host: b.<BASE>` from the internet | Forwarded headers honored only from `TRUSTED_PROXY_CIDRS` | Header forgery tests |
| T | Host-header injection into links (password-reset poisoning) | Links built from config (`PUBLIC_SCHEME`, slug, `BASE_DOMAIN`), never from the request Host; Host outside `BASE_DOMAIN` → 421 | Reset email link test with an evil Host |
| I | Tenant enumeration via 404 differences/timing | Identical body/status for unknown/deleted/failed slugs; negative cache; same code path | Timing/response diff test |
| I | Slug squatting / confusables (`admin`, `xn--`) | Regex, no `--`, reserved list, history reservation | Slug validator unit tests |
| E | Tenant API reachable on the admin host or platform API on a tenant host | Separate routers per host kind; `aud` claim check | Route matrix test |

### 3.2 Database session layer
| Threat | Example | Control | Test |
|---|---|---|---|
| I/T | Connection reused with the previous tenant's `search_path` | `SET LOCAL` only; empty default `search_path`; `AfterRelease` destroys conns in tx; distinct `TenantTx` type | Pool size 1–2, concurrent tenants, `SHOW search_path` after release |
| I | Cached prepared statement bound to the wrong schema | `QueryExecModeExec`; no custom types | Version-skew test (TENANCY §5.2) |
| E | Code path forgets the tenant session | `pos_app` has no privileges and an empty path → hard failure | Startup self-test + negative test |
| T | Schema-name injection | Server-generated IDs, `ParseSchemaName` regex, `Identifier.Sanitize` | Fuzz + injection corpus |
| T | SQL injection | sqlc/parameters only; no string-built SQL except validated identifiers; `gosec` G201/G202 | gosec, sqlmap-style payloads in the API suite |
| I | Tenant role reads `platform` or other `t_*` | Grants per TENANCY §5.3 | `has_table_privilege` matrix test |
| D | Long query blocking others | `statement_timeout`, `lock_timeout`, pool limits, per-tenant rate limits | Load test |

### 3.3 Authentication & sessions (tenant)
| Threat | Control |
|---|---|
| S: credential stuffing / brute force | Rate limits per IP/email/tenant, progressive lockout, generic errors, argon2id, breached-password blocklist |
| S: stolen access token (XSS) | Token in memory only, 10 min TTL; CSP; no tokens in localStorage |
| S: stolen refresh token | HttpOnly `__Host-` cookie, rotation, reuse detection revokes the family, bound to host |
| S: CSRF on refresh/logout | `SameSite=Strict`, Origin check, custom header |
| E: deactivated/demoted user keeps access | Per-request auth state (status, token_version, current role) with ≤ 30 s cache + NOTIFY invalidation |
| R: "I didn't do that" | Audit log with user, IP, UA, request ID; append-only for the tenant role |
| I: account enumeration via signup/reset | No tenant signup; reset always 202; login generic 401 |

### 3.4 Authorization (RBAC & object level)
| Threat | Control | Test |
|---|---|---|
| E: missing guard on a route | Deny by default; startup self-check walks the router | Router walk test |
| E: privilege escalation via role/user edit | Subset rule, no self-change, immutable owner/admin, last-owner guard | Escalation test cases |
| I: IDOR on IDs (sale, receipt, variant, return's sale, purchase item, file) | Every ID read is scoped to the tenant by construction (schema) **and** to the object rule (own sales; variant belongs to product; return's sale matches store/customer) | IDOR matrix per endpoint |
| T: mass assignment | Strict DTOs, unknown fields rejected, allow-listed columns only | Payload-fuzz tests |
| T: price/discount tampering | Server pricing; `sales:price_override`; discount cap | POS tampering tests |

### 3.5 Business logic (stock, money)
| Threat | Control |
|---|---|
| T: oversell under concurrency | `FOR UPDATE` in sorted order + conditional update + `CHECK (qty >= 0)` |
| T: double stock-in / repeated returns / negative quantities | State machines (Received terminal), cumulative return checks under lock, `CHECK (quantity > 0)` |
| T: rounding drift | Decimal arithmetic, defined rounding, golden tests |
| R: silent stock edits | Stock ledger + audit |
| D: replayed POST creates duplicate sales | Idempotency-Key |

### 3.6 Super Admin & impersonation
| Threat | Control |
|---|---|
| S: phished platform password | Mandatory TOTP (passkeys later), IP allowlist option, short sessions |
| E: support staff modifies tenant data | Role matrix; read-only impersonation default; read-write super_admin + step-up |
| R: undisclosed access to tenant data | Reason required; tenant-visible audit entries; platform audit; banner |
| S: impersonation token replay | One-time code (60 s, hashed, burned); 15-min token; revocable; no refresh |
| E: tenant token used on platform API | Different `aud`, `iss` and signing keys; host routing |
| I: platform staff see secrets | No API surface returns secrets; console shows only `passwordSet` flags |

### 3.7 Email (platform + tenant SMTP)
| Threat | Control |
|---|---|
| E/I: SSRF via tenant SMTP host (cloud metadata, internal services) | Resolve-then-validate-then-dial-IP, deny private/link-local/etc., ports 465/587 only, TLS mandatory, timeouts, optional egress firewall |
| T: header injection (CR/LF in subject/name/address) | Reject control chars; `net/mail` parsing; go-mail encoding |
| T: HTML injection in templates | `html/template`; tenant text is data; no tenant-supplied template source |
| S: spoofing tenant domains via platform SMTP | Fallback sends From the platform address, tenant name only as display name, Reply-To tenant |
| D: tenant floods mail (spam, reputation) | Daily caps, burst limits, plan limits, platform cap on fallback |
| I: SMTP password disclosure | Envelope encryption with tenant-bound AAD; write-only API; redacting logger |
| I: cross-tenant data in a receipt | Worker processes one tenant per session; render takes `TenantTx` only |
| R/D: lost or duplicated receipts | Outbox in the sale tx; idempotency key; SKIP LOCKED + leases; Message-ID dedupe |

### 3.8 Billing (Razorpay)
| Threat | Control |
|---|---|
| S: forged webhook activates a tenant | HMAC-SHA256 on the raw body, constant-time compare |
| T: replayed/duplicated/out-of-order event | Unique event ID, `last_event_at` ordering guard, 72 h staleness window, reconcile pull |
| S: client claims "payment done" | Ignored; only webhooks/pulls change status |
| D: webhook flood | Rate limit, 256 KiB body cap, cheap rejection before DB work |
| I: secret leakage | Env only, redaction, never returned |

### 3.9 Files & images
| Threat | Control |
|---|---|
| T: malicious upload (polyglot, SVG with script, decompression bomb) | Sniff with `mimetype`, allow png/jpeg/webp only, `image.DecodeConfig` dimension cap (e.g. 4096²) before full decode, re-encode (strips metadata/polyglots), size ≤ 2 MiB |
| I: cross-tenant file access by URL guessing | Keys `tenants/<tenant_id>/<kind>/<uuid>`; served only through the app after host→tenant match + permission; no public bucket listing |
| I: path traversal (fs driver) | Keys generated server-side; `filepath.IsLocal`/`os.Root` (Go 1.24+) for the fs driver |

### 3.10 Public shop
| Threat | Control |
|---|---|
| I: unpublished products, costs, suppliers, stock counts exposed | Explicit public DTO (allow-list); published-only queries; no dev bypass |
| I: enumeration of stores/products across tenants | Tenant from host; store by slug within the tenant; no numeric-ID store lookup |
| D: scraping/DoS | Rate limit per IP, `limit ≤ 48`, caching headers, CDN-friendly |

### 3.11 Supply chain, secrets, infrastructure
| Threat | Control |
|---|---|
| T: vulnerable/compromised dependency | Minimal deps, pinned versions + `go.sum`, `govulncheck` in CI (fail on reachable vulns), Dependabot/Renovate, `GOFLAGS=-mod=readonly`, `GOPROXY` default + checksum DB |
| I: secrets in git (already happened: P0-1) | Rotate, gitleaks pre-commit + CI on full history, `.env` in `.gitignore`, `.env.example` only |
| E: container escape / lateral movement | Distroless nonroot, read-only FS, no shell, dropped capabilities |
| I: DB insider/backup theft | Encrypted backups, tenant exports encrypted (age), SMTP secrets encrypted with KEK outside the DB |
| E: Electron LAN exposure | Loopback bind, per-launch token, no CORS |

## 4. Residual risks (accepted or deferred)
- SMTP is at-least-once: a crash between SMTP acceptance and DB update can cause one duplicate receipt (mitigated by Message-ID).
- Row-level isolation inside a tenant (e.g. store-scoped billers) is not in scope v1 (S8).
- A superuser/insider with DB credentials can read everything. Mitigated by access control, auditing and encrypted secrets, not eliminated.
- `'unsafe-inline'` styles in CSP until MUI/Emotion inline styles are addressed.
- Tenant SMTP is a feature that intentionally connects to tenant-chosen hosts. Defenses reduce SSRF, but an egress firewall is the strong control in production.

## 5. Phase 3 verification map
The test plan in Phase 3 (API security matrix, isolation suite with 3+ tenants, search_path suite, migration/provisioning/backup suite, Super Admin suite, email suite, billing suite, scans, e2e, performance) maps 1:1 to the "Test" columns above. Each threat row gets at least one automated test or a documented accepted risk in `PHASE3_REPORT.md`.
