# MintoBill on the VPS (mintobill.com)

Status 2026-11-23: **renamed to MintoBill (mintobill.com) in the repo; the server still runs under the old name (BilMitra, bilmitra.com) until the one-time move in §8 is done.** Everything below describes the server after that move.

Status 2026-11-22: **live.** posd runs in production mode with Gmail SMTP and local S3 storage (Versity S3 Gateway on the VPS, owner-approved interim until Cloudflare R2). Super Admin created; shops test-retail, test-clothing, test-pharmacy provisioned.

The VPS (66.116.232.174, Ubuntu 22.04, 2 vCPU, 3.8 GB) is shared with other clients' sites. Everything MintoBill uses is separate from them, and nothing of theirs was changed.

## 1. Layout

| What | Where |
|---|---|
| Releases | `/var/www/mintobill/releases/<id>/{bin/posd,bin/posctl,static/tenant,static/admin,RELEASE}` |
| Live release | `/var/www/mintobill/current` → a release (switch back = rollback) |
| Settings and secrets | `/var/www/mintobill/shared/.env` (root:mintobill 0640), template `backend/deploy/vps/env.production.template` |
| DB admin login | `/root/mintobill-admin.env` (root only, used only by `mintobill-ctl --admin`) |
| Internal CA (DB TLS) | `/root/mintobill-ca/` (key), `/var/www/mintobill/shared/tls/ca.crt` |
| Database | its own PostgreSQL 18 cluster `18/mintobill`, `127.0.0.1:5433`, TLS only, database `mintobill`. The shared `main` cluster (5432) is not used |
| App service | `mintobill.service`: posd as user `mintobill`, `127.0.0.1:8090`, metrics on `127.0.0.1:9095`, MemoryMax 512 MB |
| Nginx | `/etc/nginx/sites-available/mintobill.com` (source `backend/deploy/vps/nginx-mintobill.com.conf`) |
| Certificate | Let's Encrypt `mintobill.com` (+ `www`), webroot `/var/www/mintobill/acme`, renewed by the existing certbot timer |
| File storage (interim) | `mintobill-s3.service`: versitygw v1.9.0 (`/opt/mintobill-s3`), `127.0.0.1:7070`, TLS from the internal CA, bucket `mintobill` in `/var/www/mintobill/data/s3`, keys in `/etc/mintobill-s3/env`; setup `backend/deploy/vps/setup-storage.sh`. posd trusts the CA via `SSL_CERT_DIR` |
| Backups | `mintobill-backup.timer`, 01:30 India time, `pg_dump` to `/var/www/mintobill/backups` (14 kept) |
| Maintenance page | `/var/www/mintobill/maintenance/` (shown on 502/503/504) |
| Logs | `journalctl -u mintobill`, `/var/log/nginx/mintobill.*.log`, `/var/log/postgresql/postgresql-18-mintobill.log` |

Each file in `backend/deploy/vps/` says where it is installed. `setup-server.sh` is idempotent and never overwrites an existing `.env`.

## 2. Hosts

posd routes by host (TENANCY §7.1): `mintobill.com` landing page, `admin.mintobill.com` Super Admin console, `<shop>.mintobill.com` each shop. `www` redirects to the apex in Nginx.

DNS for mintobill.com (at its registrar) needs:

- `A  @      66.116.232.174`
- `A  www    66.116.232.174`
- `A  admin  66.116.232.174`
- `A  *      66.116.232.174` (every shop address)

Then add each name to the certificate (HTTP-01 cannot issue a wildcard):

```sh
certbot certonly --webroot -w /var/www/mintobill/acme --cert-name mintobill.com --expand \
  -d mintobill.com -d www.mintobill.com -d admin.mintobill.com -d <shop>.mintobill.com ... \
  --deploy-hook "systemctl reload nginx"
```

Run it again with the full list for each new shop (Let's Encrypt allows 100 names per certificate).

**Upgrade path:** move DNS for mintobill.com to Cloudflare (free) and use `certbot-dns-cloudflare` for one `*.mintobill.com` certificate. New shops then need no step. The Nginx file does not change.

## 3. Deploy a new version

From WSL, in the repo (your working tree is not touched; the ref is exported):

```sh
backend/deploy/vps/deploy.sh            # origin/main
backend/deploy/vps/deploy.sh <commit>
```

It builds posd, posctl and both SPAs, uploads a release, runs `config check`, platform and tenant migrations, switches `current`, restarts posd and waits for `/readyz`. If posd is not ready after 30 s it switches back. Migrations are forward-only, so a rollback after a migration needs the old code to work with the new schema (our migrations are additive).

Manual rollback: `ln -sfn /var/www/mintobill/releases/<old> /var/www/mintobill/current && systemctl restart mintobill`.

**Every production release gets a Semantic Versioning number (`vMAJOR.MINOR.PATCH`) and is published on GitHub as a Release (not only a tag).** Steps, the numbering rules and the release history are in [`RELEASES.md`](../RELEASES.md).

## 4. Operator commands (on the server, as root)

```sh
mintobill-ctl config check
mintobill-ctl migrate platform status
mintobill-ctl migrate drift
BOOTSTRAP_ADMIN_EMAIL=you@example.com BOOTSTRAP_ADMIN_PASSWORD='<14+ chars>' mintobill-ctl bootstrap-superadmin
mintobill-ctl tenant provision --slug <shop> --name "<Shop name>" --owner-email <owner email>
systemctl status mintobill;  journalctl -u mintobill -f
```

## 5. Go-live: what is still needed

1. **File storage: move to Cloudflare R2 before real shops.** Today the files sit on the same VPS (no off-site copy). To move: create the R2 bucket, copy `/var/www/mintobill/data/s3/mintobill/` into it (rclone), set `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION=auto` in `shared/.env`, restart mintobill, then `systemctl disable --now mintobill-s3`.
2. **SMTP: done** (Gmail `smtp.gmail.com:587`, app password; sender = the Gmail address, ~500 mails/day). Later a provider on mintobill.com with SPF/DKIM: `SMTP_HOST`, `SMTP_PORT`, `SMTP_TLS`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `MAIL_FROM_ADDRESS` in `shared/.env`. Set up SPF/DKIM for mintobill.com at the provider. Without mail, new shop owners never get their invite.
3. DNS `@`, `www`, `admin` and `*` for mintobill.com, then the move in §8 (it issues the certificate: mintobill.com, www, admin and every shop).
4. Started: `mintobill` and `mintobill-s3` enabled.
5. Super Admin created (`bootstrap-superadmin`); first sign-in at https://admin.mintobill.com sets a new password, TOTP, recovery codes.
6. Optional: `EXPORT_AGE_RECIPIENT` + `EXPORT_MANIFEST_KEY` for nightly encrypted per-shop exports (OPERATIONS §6.2); keep the age identity off the server.
7. Copy `/var/www/mintobill/backups` off the server regularly (same machine = no disaster recovery).
8. After the go-live checklist: `HSTS_ENABLED=true`.

## 6. Resources and upgrades

Measured 2026-11-22: posd 146 MB, storage 9 MB, PostgreSQL 90 MB. Budget on this VPS: PostgreSQL cluster ~150 MB (`shared_buffers` 128 MB), posd capped at 512 MB (`GOMEMLIMIT` 300 MiB), DB pools 10. The server had ~1.1 GB available and was already swapping before MintoBill.

When it grows: raise `shared_buffers`/`effective_cache_size`/`max_connections` in `/etc/postgresql/18/mintobill/conf.d/mintobill.conf` and `DB_POOL_MAX`; raise `MemoryMax`; or move to its own server. The cluster moves as is (`pg_basebackup`, or a managed PostgreSQL 18 with OPERATIONS §8 preflight: change only the `DATABASE_*` URLs), and so does S3 (R2 already off-server).

## 7. Untouched

The other sites' Nginx files, `nginx.conf`, the shared PostgreSQL `main` cluster and its config, the other apps and their services, firewalld (5433/8090/9095 are loopback only and not opened) and the other certificates. Nginx was only reloaded (never restarted), after `nginx -t`. No `http2` was added (in nginx 1.18 it would switch every site on :443). The Nginx config backup from before the change is in `/root/mintobill-backup-20261122/`.

## 8. One-time move from bilmitra.com to mintobill.com

The server was set up as BilMitra (`/var/www/bilmitra`, `bilmitra.service`, cluster and database `18/bilmitra`, users `bilmitra` and `bilmitra-s3`, `bilmitra-ctl`, bilmitra.com). `backend/deploy/vps/migrate-to-mintobill.sh` moves all of it to the names above. Until it has run, `deploy.sh` from this branch does not work against the server (it expects the new paths).

1. Point the mintobill.com DNS records in §2 at the server and wait until they resolve.
2. Copy the files and run the move (about a minute of downtime, after the certificate is issued):

   ```sh
   scp -r backend/deploy/vps root@66.116.232.174:/root/mintobill-deploy
   ssh root@66.116.232.174 bash /root/mintobill-deploy/migrate-to-mintobill.sh /root/mintobill-deploy
   ```

   It checks DNS for every name first and stops before changing anything if one is missing. It backs up the database, the Nginx site, `.env` and the certbot renewal files to `/root/mintobill-migration-<time>/`. A second run continues where a failed one stopped.
3. Run `backend/deploy/vps/deploy.sh` so the landing site is rebuilt with the new name (the release on the server was built as BilMitra). The SSH settings are now `MINTOBILL_SSH` and `MINTOBILL_SSH_KEY`.
4. bilmitra.com stays as a 301 redirect to the same address on mintobill.com (`nginx-old-domain-redirect.conf`) while it still points here. Its file says how to drop it.

Existing TOTP entries in authenticator apps keep the old label; sign-in is not affected.
