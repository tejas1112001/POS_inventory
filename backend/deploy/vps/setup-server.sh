#!/usr/bin/env bash
# One-time (idempotent) setup of BilMitra on the shared VPS. Run as root:
#   bash setup-server.sh <dir with the deploy/vps files>
# Creates only BilMitra's own things: the bilmitra user, /var/www/bilmitra,
# the 18/bilmitra PostgreSQL cluster on 127.0.0.1:5433 and the secrets in
# /var/www/bilmitra/shared. It never edits another site, the shared "main"
# PostgreSQL cluster, nginx.conf or the firewall. Secrets are never printed.
set -euo pipefail
umask 077

SRC=${1:?usage: setup-server.sh <deploy/vps dir>}
ROOT=/var/www/bilmitra
SHARED=$ROOT/shared
PGVER=18
CLUSTER=bilmitra
PGPORT=5433
PGETC=/etc/postgresql/$PGVER/$CLUSTER
DB=bilmitra

log() { printf '== %s\n' "$*"; }

# 1. Service user (no shell, no home login).
if ! id bilmitra >/dev/null 2>&1; then
    log "creating system user bilmitra"
    useradd --system --home-dir $ROOT --no-create-home --shell /usr/sbin/nologin bilmitra
fi

# 2. Directories.
log "directories under $ROOT"
install -d -m 0755 -o root -g root $ROOT $ROOT/releases $ROOT/acme $ROOT/maintenance
install -d -m 0750 -o root -g bilmitra $SHARED $SHARED/tls
install -d -m 0700 -o postgres -g postgres $ROOT/backups
install -m 0644 "$SRC/maintenance.html" $ROOT/maintenance/__bilmitra_unavailable.html

# 3. Private CA for the database TLS (sslmode=verify-full on loopback).
if [ ! -f $SHARED/tls/ca.crt ]; then
    log "creating BilMitra internal CA (database TLS)"
    install -d -m 0700 /root/bilmitra-ca
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -days 3650 \
        -subj "/CN=BilMitra internal CA" -keyout /root/bilmitra-ca/ca.key -out /root/bilmitra-ca/ca.crt 2>/dev/null
    install -m 0644 /root/bilmitra-ca/ca.crt $SHARED/tls/ca.crt
fi

# 4. Separate PostgreSQL cluster.
if ! pg_lsclusters -h | awk '{print $1"/"$2}' | grep -qx "$PGVER/$CLUSTER"; then
    log "creating PostgreSQL cluster $PGVER/$CLUSTER on port $PGPORT"
    pg_createcluster $PGVER $CLUSTER --port $PGPORT --encoding UTF8 --locale C.UTF-8 \
        -- --auth-local=peer --auth-host=scram-sha-256 >/dev/null
fi
install -d -m 0700 -o postgres -g postgres $PGETC/tls
if [ ! -f $PGETC/tls/server.crt ]; then
    log "issuing database server certificate (localhost, 127.0.0.1)"
    tmp=$(mktemp -d)
    openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -subj "/CN=localhost" \
        -keyout $tmp/server.key -out $tmp/server.csr 2>/dev/null
    printf 'subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' >$tmp/ext
    openssl x509 -req -in $tmp/server.csr -CA /root/bilmitra-ca/ca.crt -CAkey /root/bilmitra-ca/ca.key \
        -CAcreateserial -days 825 -extfile $tmp/ext -out $tmp/server.crt 2>/dev/null
    install -m 0644 -o postgres -g postgres $tmp/server.crt $PGETC/tls/server.crt
    install -m 0600 -o postgres -g postgres $tmp/server.key $PGETC/tls/server.key
    rm -rf "$tmp"
fi
install -m 0644 -o postgres -g postgres "$SRC/postgresql-bilmitra.conf" $PGETC/conf.d/bilmitra.conf
install -m 0640 -o postgres -g postgres "$SRC/pg_hba-bilmitra.conf" $PGETC/pg_hba.conf
systemctl enable --quiet postgresql@$PGVER-$CLUSTER
systemctl restart postgresql@$PGVER-$CLUSTER     # this cluster only
pg_isready -q -h 127.0.0.1 -p $PGPORT

psql_c() { runuser -u postgres -- psql -X -q -v ON_ERROR_STOP=1 -p $PGPORT "$@"; }
if ! psql_c -Atc "select 1 from pg_database where datname='$DB'" | grep -q 1; then
    log "creating database $DB"
    runuser -u postgres -- createdb -p $PGPORT $DB
fi

# 5. Secrets (generated here, once; never echoed).
rand_hex() { openssl rand -hex 24; }
rand_b64() { openssl rand -base64 32 | tr -d '\n'; }
if [ ! -f /root/bilmitra-admin.env ]; then
    log "setting the cluster's postgres password (admin login, kept in /root only)"
    pw=$(rand_hex)
    psql_c -d postgres <<<"ALTER ROLE postgres PASSWORD '$pw';"
    printf 'DATABASE_ADMIN_URL="postgres://postgres:%s@localhost:%s/%s?sslmode=verify-full&sslrootcert=%s"\n' \
        "$pw" $PGPORT $DB $SHARED/tls/ca.crt >/root/bilmitra-admin.env
    chmod 600 /root/bilmitra-admin.env
fi
if [ ! -f $SHARED/.env ]; then
    log "writing $SHARED/.env with generated secrets"
    dsn() { printf 'postgres://%s:%s@localhost:%s/%s?sslmode=verify-full&sslrootcert=%s' "$1" "$(rand_hex)" $PGPORT $DB $SHARED/tls/ca.crt; }
    # Plain bash substitution: the URLs contain "&", which sed would expand.
    declare -A val=(
        [@DATABASE_URL@]="$(dsn pos_app)"
        [@DATABASE_JOBS_URL@]="$(dsn pos_jobs)"
        [@DATABASE_MIGRATOR_URL@]="$(dsn pos_migrator)"
        [@JWT_KEYS@]="k$(date +%Y%m):$(rand_b64)"
        [@PLATFORM_JWT_KEYS@]="p$(date +%Y%m):$(rand_b64)"
        [@MASTER_KEYS@]="m$(date +%Y%m):$(rand_b64)"
    )
    while IFS= read -r line || [ -n "$line" ]; do
        for k in "${!val[@]}"; do line=${line//"$k"/"${val[$k]}"}; done
        printf '%s\n' "$line"
    done <"$SRC/env.production.template" >$SHARED/.env
    if grep -q '@[A-Z_]*@' $SHARED/.env; then echo "unfilled placeholder in .env" >&2; exit 1; fi
    chown root:bilmitra $SHARED/.env
    chmod 0640 $SHARED/.env
fi

# 6. systemd units (installed, not started: deploy.sh starts posd).
install -m 0644 "$SRC/bilmitra.service" /etc/systemd/system/bilmitra.service
install -m 0644 "$SRC/bilmitra-backup.service" /etc/systemd/system/bilmitra-backup.service
install -m 0644 "$SRC/bilmitra-backup.timer" /etc/systemd/system/bilmitra-backup.timer
install -m 0755 "$SRC/bilmitra-backup.sh" /usr/local/sbin/bilmitra-backup
install -m 0700 "$SRC/bilmitra-ctl.sh" /usr/local/sbin/bilmitra-ctl
systemctl daemon-reload
systemctl enable --quiet --now bilmitra-backup.timer

log "done. Cluster: $(runuser -u postgres -- psql -XAt -p $PGPORT -c 'show server_version') on 127.0.0.1:$PGPORT"
