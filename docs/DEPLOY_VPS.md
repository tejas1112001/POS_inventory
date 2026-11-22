# BilMitra on the VPS (bilmitra.com)

Status 2026-11-22: **live.** posd runs in production mode with Gmail SMTP and local S3 storage (Versity S3 Gateway on the VPS, owner-approved interim until Cloudflare R2). Super Admin created; shops test-retail, test-clothing, test-pharmacy provisioned.

The VPS (66.116.232.174, Ubuntu 22.04, 2 vCPU, 3.8 GB) is shared with other clients' sites. Everything BilMitra uses is separate from them, and nothing of theirs was changed.

## 1. Layout

| What | Where |
|---|---|
| Releases | `/var/www/bilmitra/releases/<id>/{bin/posd,bin/posctl,static/tenant,static/admin,RELEASE}` |
| Live release | `/var/www/bilmitra/current` → a release (switch back = rollback) |
| Settings and secrets | `/var/www/bilmitra/shared/.env` (root:bilmitra 0640), template `backend/deploy/vps/env.production.template` |
| DB admin login | `/root/bilmitra-admin.env` (root only, used only by `bilmitra-ctl --admin`) |
| Internal CA (DB TLS) | `/root/bilmitra-ca/` (key), `/var/www/bilmitra/shared/tls/ca.crt` |
| Database | its own PostgreSQL 18 cluster `18/bilmitra`, `127.0.0.1:5433`, TLS only, database `bilmitra`. The shared `main` cluster (5432) is not used |
| App service | `bilmitra.service`: posd as user `bilmitra`, `127.0.0.1:8090`, metrics on `127.0.0.1:9095`, MemoryMax 512 MB |
| Nginx | `/etc/nginx/sites-available/bilmitra.com` (source `backend/deploy/vps/nginx-bilmitra.com.conf`) |
| Certificate | Let's Encrypt `bilmitra.com` (+ `www`), webroot `/var/www/bilmitra/acme`, renewed by the existing certbot timer |
| File storage (interim) | `bilmitra-s3.service`: versitygw v1.9.0 (`/opt/bilmitra-s3`), `127.0.0.1:7070`, TLS from the internal CA, bucket `bilmitra` in `/var/www/bilmitra/data/s3`, keys in `/etc/bilmitra-s3/env`; setup `backend/deploy/vps/setup-storage.sh`. posd trusts the CA via `SSL_CERT_DIR` |
| Backups | `bilmitra-backup.timer`, 01:30 India time, `pg_dump` to `/var/www/bilmitra/backups` (14 kept) |
| Maintenance page | `/var/www/bilmitra/maintenance/` (shown on 502/503/504) |
| Logs | `journalctl -u bilmitra`, `/var/log/nginx/bilmitra.*.log`, `/var/log/postgresql/postgresql-18-bilmitra.log` |

Each file in `backend/deploy/vps/` says where it is installed. `setup-server.sh` is idempotent and never overwrites an existing `.env`.

## 2. Hosts

posd routes by host (TENANCY §7.1): `bilmitra.com` landing page, `admin.bilmitra.com` Super Admin console, `<shop>.bilmitra.com` each shop. `www` redirects to the apex in Nginx.

DNS (GoDaddy) today has only `bilmitra.com` and `www`. Add at GoDaddy:

- `A  admin  66.116.232.174`
- `A  *      66.116.232.174` (every shop address)

Then add each name to the certificate (HTTP-01 cannot issue a wildcard):

```sh
certbot certonly --webroot -w /var/www/bilmitra/acme --cert-name bilmitra.com --expand \
  -d bilmitra.com -d www.bilmitra.com -d admin.bilmitra.com -d <shop>.bilmitra.com ... \
  --deploy-hook "systemctl reload nginx"
```

Run it again with the full list for each new shop (Let's Encrypt allows 100 names per certificate).

**Upgrade path:** move DNS for bilmitra.com to Cloudflare (free) and use `certbot-dns-cloudflare` for one `*.bilmitra.com` certificate. New shops then need no step. The Nginx file does not change.

## 3. Deploy a new version

From WSL, in the repo (your working tree is not touched; the ref is exported):

```sh
backend/deploy/vps/deploy.sh            # origin/main
backend/deploy/vps/deploy.sh <commit>
```

It builds posd, posctl and both SPAs, uploads a release, runs `config check`, platform and tenant migrations, switches `current`, restarts posd and waits for `/readyz`. If posd is not ready after 30 s it switches back. Migrations are forward-only, so a rollback after a migration needs the old code to work with the new schema (our migrations are additive).

Manual rollback: `ln -sfn /var/www/bilmitra/releases/<old> /var/www/bilmitra/current && systemctl restart bilmitra`.

## 4. Operator commands (on the server, as root)

```sh
bilmitra-ctl config check
bilmitra-ctl migrate platform status
bilmitra-ctl migrate drift
BOOTSTRAP_ADMIN_EMAIL=you@example.com BOOTSTRAP_ADMIN_PASSWORD='<14+ chars>' bilmitra-ctl bootstrap-superadmin
bilmitra-ctl tenant provision --slug <shop> --name "<Shop name>" --owner-email <owner email>
systemctl status bilmitra;  journalctl -u bilmitra -f
```

## 5. Go-live: what is still needed

1. **File storage: move to Cloudflare R2 before real shops.** Today the files sit on the same VPS (no off-site copy). To move: create the R2 bucket, copy `/var/www/bilmitra/data/s3/bilmitra/` into it (rclone), set `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION=auto` in `shared/.env`, restart bilmitra, then `systemctl disable --now bilmitra-s3`.
2. **SMTP: done** (Gmail `smtp.gmail.com:587`, app password; sender = the Gmail address, ~500 mails/day). Later a provider on bilmitra.com with SPF/DKIM: `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `MAIL_FROM_ADDRESS` in `shared/.env`. Set up SPF/DKIM for bilmitra.com at the provider. Without mail, new shop owners never get their invite.
3. DNS `admin` and `*`: done. Certificate names: bilmitra.com, www, admin, test-retail, test-clothing, test-pharmacy.
4. Started: `bilmitra` and `bilmitra-s3` enabled.
5. Super Admin created (`bootstrap-superadmin`); first sign-in at https://admin.bilmitra.com sets a new password, TOTP, recovery codes.
6. Optional: `EXPORT_AGE_RECIPIENT` + `EXPORT_MANIFEST_KEY` for nightly encrypted per-shop exports (OPERATIONS §6.2); keep the age identity off the server.
7. Copy `/var/www/bilmitra/backups` off the server regularly (same machine = no disaster recovery).
8. After the go-live checklist: `HSTS_ENABLED=true`.

## 6. Resources and upgrades

Measured 2026-11-22: posd 146 MB, storage 9 MB, PostgreSQL 90 MB. Budget on this VPS: PostgreSQL cluster ~150 MB (`shared_buffers` 128 MB), posd capped at 512 MB (`GOMEMLIMIT` 300 MiB), DB pools 10. The server had ~1.1 GB available and was already swapping before BilMitra.

When it grows: raise `shared_buffers`/`effective_cache_size`/`max_connections` in `/etc/postgresql/18/bilmitra/conf.d/bilmitra.conf` and `DB_POOL_MAX`; raise `MemoryMax`; or move to its own server. The cluster moves as is (`pg_basebackup`, or a managed PostgreSQL 18 with OPERATIONS §8 preflight: change only the `DATABASE_*` URLs), and so does S3 (R2 already off-server).

## 7. Untouched

The other sites' Nginx files, `nginx.conf`, the shared PostgreSQL `main` cluster and its config, the other apps and their services, firewalld (5433/8090/9095 are loopback only and not opened) and the other certificates. Nginx was only reloaded (never restarted), after `nginx -t`. No `http2` was added (in nginx 1.18 it would switch every site on :443). The Nginx config backup from before the change is in `/root/bilmitra-backup-20261122/`.
