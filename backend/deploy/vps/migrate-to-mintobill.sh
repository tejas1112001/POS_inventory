#!/usr/bin/env bash
# One-time move of the live server from the old name (BilMitra,
# bilmitra.com) to MintoBill (mintobill.com). Run as root on the VPS:
#   bash migrate-to-mintobill.sh <dir with the deploy/vps files> [server IP]
# then run deploy.sh from the repo so the landing site is rebuilt with the
# new name.
#
# Before running: DNS for mintobill.com, www, admin and * must point at the
# server (the script checks every name and stops if one does not).
#
# What it does, in order (each step is skipped when already done, so a
# second run continues where a failed one stopped):
#   1. backs up the database, nginx site, .env and certbot renewal files to
#      /root/mintobill-migration-<time>/
#   2. gets the mintobill.com certificate (old site still up, no downtime)
#   3. stops posd, storage and the backup timer   <- downtime starts
#   4. renames the PostgreSQL cluster 18/bilmitra -> 18/mintobill and the
#      database bilmitra -> mintobill
#   5. moves /var/www, /opt, /etc and /root paths, renames the system users,
#      rewrites the paths and names in .env and the other settings files
#   6. installs the renamed systemd units and commands
#   7. installs the mintobill.com Nginx site; bilmitra.com becomes a
#      301 redirect to the same address on mintobill.com
#   8. starts everything and waits for /readyz   <- downtime ends
# Other sites, nginx.conf, the shared "main" cluster and the firewall are
# not touched. Secrets are never printed.
set -euo pipefail
umask 077

SRC=${1:?usage: migrate-to-mintobill.sh <deploy/vps dir> [server IP]}
IP=${2:-66.116.232.174}
OLD=bilmitra
NEW=mintobill
OLD_DOMAIN=bilmitra.com
NEW_DOMAIN=mintobill.com
PGVER=18
PGPORT=5433
ROOT=/var/www/$NEW
BK=/root/$NEW-migration-$(date -u +%Y%m%dT%H%M%SZ)

log() { printf '== %s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
# mv only when the old path exists and the new one does not.
move() { if [ -e "$1" ] && [ ! -e "$2" ]; then log "move $1 -> $2"; mv -T "$1" "$2"; fi; }
psql_p() { runuser -u postgres -- psql -X -q -v ON_ERROR_STOP=1 -p $PGPORT "$@"; }
db_exists() { psql_p -d postgres -Atc "select 1 from pg_database where datname='$1'" | grep -q 1; }

[ "$(id -u)" = 0 ] || die "run as root"
for f in $NEW.service $NEW-s3.service $NEW-backup.service $NEW-backup.timer $NEW-backup.sh $NEW-ctl.sh \
         nginx-$NEW_DOMAIN.conf nginx-old-domain-redirect.conf postgresql-$NEW.conf pg_hba-$NEW.conf maintenance.html; do
    [ -f "$SRC/$f" ] || die "missing $SRC/$f (pass the backend/deploy/vps directory from this branch)"
done
command -v pg_renamecluster >/dev/null || die "pg_renamecluster not found (postgresql-common)"

# 0. DNS: every name on the certificate must already point here.
pg_isready -q -h 127.0.0.1 -p $PGPORT || die "PostgreSQL on port $PGPORT is not running"
DB=$NEW; db_exists $NEW || DB=$OLD
slugs=$(psql_p -d $DB -Atc "select slug from platform.tenants where status <> 'deleted' order by 1") \
    || die "could not read the shop list from database $DB"
mapfile -t SLUGS < <(printf '%s\n' "$slugs" | sed '/^$/d')
NAMES=($NEW_DOMAIN www.$NEW_DOMAIN admin.$NEW_DOMAIN)
for s in "${SLUGS[@]}"; do NAMES+=("$s.$NEW_DOMAIN"); done
log "checking DNS for ${#NAMES[@]} names -> $IP"
bad=0
for n in "${NAMES[@]}"; do
    getent ahostsv4 "$n" | awk '{print $1}' | sort -u | grep -qx "$IP" || { echo "   $n does not resolve to $IP" >&2; bad=1; }
done
[ $bad = 0 ] || die "fix DNS first (A records @, www, admin and * -> $IP), then run again"

# 1. Backups.
log "backups in $BK"
install -d -m 0700 "$BK"
if systemctl cat $OLD-backup.service >/dev/null 2>&1 && db_exists $OLD; then
    systemctl start $OLD-backup.service || die "database backup failed (journalctl -u $OLD-backup)"
    log "database dump: $(ls -1t /var/www/$OLD/backups/$OLD-*.dump | head -1)"
fi
for f in /etc/nginx/sites-available/$OLD_DOMAIN /etc/nginx/sites-available/$NEW_DOMAIN \
         /var/www/$OLD/shared/.env $ROOT/shared/.env /root/$OLD-admin.env \
         /etc/$OLD-s3/env /etc/letsencrypt/renewal/$OLD_DOMAIN.conf /etc/letsencrypt/renewal/$NEW_DOMAIN.conf; do
    [ -f "$f" ] && cp -a "$f" "$BK/$(echo "$f" | tr / _)"
done

# 2. Certificate for the new names (HTTP-01 through the ACME webroot).
WEBROOT=/var/www/$OLD/acme; [ -d $WEBROOT ] || WEBROOT=$ROOT/acme
if [ ! -e /etc/nginx/sites-enabled/$NEW_DOMAIN ]; then
    log "temporary port-80 site for $NEW_DOMAIN (ACME only)"
    cat >/etc/nginx/sites-available/$NEW_DOMAIN <<EOF
server {
    listen 80;
    server_name $NEW_DOMAIN www.$NEW_DOMAIN *.$NEW_DOMAIN;
    location ^~ /.well-known/acme-challenge/ { root $WEBROOT; default_type text/plain; }
    location / { return 503; }
}
EOF
    ln -sfn /etc/nginx/sites-available/$NEW_DOMAIN /etc/nginx/sites-enabled/$NEW_DOMAIN
    nginx -t -q || { rm -f /etc/nginx/sites-enabled/$NEW_DOMAIN /etc/nginx/sites-available/$NEW_DOMAIN; die "nginx -t failed"; }
    systemctl reload nginx
fi
log "certificate $NEW_DOMAIN for: ${NAMES[*]}"
dargs=(); for n in "${NAMES[@]}"; do dargs+=(-d "$n"); done
certbot certonly --non-interactive --webroot -w $WEBROOT --cert-name $NEW_DOMAIN --expand \
    --keep-until-expiring "${dargs[@]}" --deploy-hook "systemctl reload nginx" \
    || die "certbot failed; nothing else was changed yet"

# 3. Stop the app (downtime starts).
log "stopping posd, storage and backup timer"
for u in $OLD.service $OLD-s3.service $OLD-backup.timer $OLD-backup.service; do
    systemctl cat $u >/dev/null 2>&1 && systemctl disable --quiet --now $u || true
done

# 4. PostgreSQL cluster and database.
if pg_lsclusters -h | awk '{print $1"/"$2}' | grep -qx "$PGVER/$OLD"; then
    log "renaming cluster $PGVER/$OLD -> $PGVER/$NEW"
    systemctl disable --quiet --now postgresql@$PGVER-$OLD
    pg_renamecluster $PGVER $OLD $NEW
fi
PGETC=/etc/postgresql/$PGVER/$NEW
rm -f $PGETC/conf.d/$OLD.conf
install -m 0644 -o postgres -g postgres "$SRC/postgresql-$NEW.conf" $PGETC/conf.d/$NEW.conf
install -m 0640 -o postgres -g postgres "$SRC/pg_hba-$NEW.conf" $PGETC/pg_hba.conf
systemctl enable --quiet postgresql@$PGVER-$NEW
systemctl restart postgresql@$PGVER-$NEW
for i in $(seq 1 20); do pg_isready -q -h 127.0.0.1 -p $PGPORT && break; sleep 1; done
pg_isready -q -h 127.0.0.1 -p $PGPORT || die "cluster $PGVER/$NEW did not start (journalctl -u postgresql@$PGVER-$NEW)"
if db_exists $OLD && ! db_exists $NEW; then
    log "renaming database $OLD -> $NEW"
    psql_p -d postgres -c "ALTER DATABASE $OLD RENAME TO $NEW"
fi
db_exists $NEW || die "database $NEW not found"

# 5. Paths, users and settings files.
move /var/www/$OLD $ROOT
move $ROOT/data/s3/$OLD $ROOT/data/s3/$NEW               # storage bucket
move /opt/$OLD-s3 /opt/$NEW-s3
move /etc/$OLD-s3 /etc/$NEW-s3
move /root/$OLD-ca /root/$NEW-ca
move /root/$OLD-admin.env /root/$NEW-admin.env
move /root/$OLD-backup-20261122 /root/$NEW-backup-20261122
for f in $ROOT/backups/$OLD-*.dump; do [ -e "$f" ] && mv "$f" "${f/\/$OLD-//$NEW-}"; done
move $ROOT/maintenance/__${OLD}_unavailable.html $ROOT/maintenance/__${NEW}_unavailable.html
install -m 0644 "$SRC/maintenance.html" $ROOT/maintenance/__${NEW}_unavailable.html

if id $OLD >/dev/null 2>&1; then
    log "renaming system user $OLD -> $NEW"
    usermod -l $NEW -d $ROOT $OLD && groupmod -n $NEW $OLD
fi
if id $OLD-s3 >/dev/null 2>&1; then
    log "renaming system user $OLD-s3 -> $NEW-s3"
    usermod -l $NEW-s3 -d $ROOT/data/s3 $OLD-s3 && groupmod -n $NEW-s3 $OLD-s3
fi

t=$(readlink $ROOT/current || true)
if [ -n "$t" ] && [ "${t#/var/www/$OLD/}" != "$t" ]; then
    log "re-pointing current -> ${t/\/var\/www\/$OLD\//$ROOT/}"
    ln -sfn "${t/\/var\/www\/$OLD\//$ROOT/}" $ROOT/current.new && mv -Tf $ROOT/current.new $ROOT/current
fi

log "rewriting names in .env and settings files"
sed -i -E \
    -e "s|/var/www/$OLD/|$ROOT/|g" \
    -e "s|(:$PGPORT)/$OLD\\?|\\1/$NEW?|g" \
    -e "s|^(BASE_DOMAIN=)\"?$OLD_DOMAIN\"?\$|\\1$NEW_DOMAIN|" \
    -e "s|^(PLATFORM_NAME=)\"?BilMitra\"?\$|\\1MintoBill|" \
    -e "s|^(S3_BUCKET=)\"?$OLD\"?\$|\\1$NEW|" \
    -e "s|@$OLD_DOMAIN|@$NEW_DOMAIN|g" \
    $ROOT/shared/.env /root/$NEW-admin.env
sed -i -e "s|/etc/$OLD-s3/|/etc/$NEW-s3/|g" /etc/$NEW-s3/env
sed -i -e "s|/var/www/$OLD/acme|$ROOT/acme|g" /etc/letsencrypt/renewal/*.conf
chown root:$NEW $ROOT/shared/.env && chmod 0640 $ROOT/shared/.env
if grep -qi "$OLD" $ROOT/shared/.env; then
    echo "   note: these .env settings still mention $OLD (values not shown):" >&2
    grep -i "$OLD" $ROOT/shared/.env | cut -d= -f1 | sed 's/^/     /' >&2
fi

# 6. systemd units and commands.
log "installing systemd units and commands"
rm -f /etc/systemd/system/$OLD.service /etc/systemd/system/$OLD-s3.service \
      /etc/systemd/system/$OLD-backup.service /etc/systemd/system/$OLD-backup.timer \
      /usr/local/sbin/$OLD-backup /usr/local/sbin/$OLD-ctl
install -m 0644 "$SRC/$NEW.service" /etc/systemd/system/$NEW.service
install -m 0644 "$SRC/$NEW-s3.service" /etc/systemd/system/$NEW-s3.service
install -m 0644 "$SRC/$NEW-backup.service" /etc/systemd/system/$NEW-backup.service
install -m 0644 "$SRC/$NEW-backup.timer" /etc/systemd/system/$NEW-backup.timer
install -m 0755 "$SRC/$NEW-backup.sh" /usr/local/sbin/$NEW-backup
install -m 0700 "$SRC/$NEW-ctl.sh" /usr/local/sbin/$NEW-ctl
systemctl daemon-reload

# 7. Nginx: new site, old domain redirects.
log "installing Nginx sites"
cp -a /etc/nginx/sites-available/$OLD_DOMAIN "$BK/nginx-$OLD_DOMAIN.before" 2>/dev/null || true
install -m 0644 "$SRC/nginx-$NEW_DOMAIN.conf" /etc/nginx/sites-available/$NEW_DOMAIN
ln -sfn /etc/nginx/sites-available/$NEW_DOMAIN /etc/nginx/sites-enabled/$NEW_DOMAIN
if [ -d /etc/letsencrypt/live/$OLD_DOMAIN ]; then
    install -m 0644 "$SRC/nginx-old-domain-redirect.conf" /etc/nginx/sites-available/$OLD_DOMAIN
    ln -sfn /etc/nginx/sites-available/$OLD_DOMAIN /etc/nginx/sites-enabled/$OLD_DOMAIN
else
    rm -f /etc/nginx/sites-enabled/$OLD_DOMAIN
fi
if ! nginx -t -q; then
    [ -f "$BK/nginx-$OLD_DOMAIN.before" ] && cp -a "$BK/nginx-$OLD_DOMAIN.before" /etc/nginx/sites-available/$OLD_DOMAIN
    die "nginx -t failed with the new sites; old site file restored, Nginx not reloaded. Fix and run again"
fi
systemctl reload nginx

# 8. Start (downtime ends).
log "starting storage, posd and the backup timer"
systemctl enable --quiet --now $NEW-s3.service
for i in $(seq 1 15); do
    curl -fsS -m 2 --cacert $ROOT/shared/tls/ca.crt https://localhost:7070/health >/dev/null 2>&1 && break
    sleep 1
done
$NEW-ctl config check
systemctl enable --quiet --now $NEW.service
systemctl enable --quiet --now $NEW-backup.timer
for i in $(seq 1 30); do
    if curl -fsS -m 2 -o /dev/null http://127.0.0.1:8090/readyz; then
        log "ready. https://$NEW_DOMAIN is live; https://$OLD_DOMAIN redirects to it."
        log "next: run backend/deploy/vps/deploy.sh so the landing site shows the new name."
        log "backups of the old files: $BK"
        exit 0
    fi
    sleep 1
done
journalctl -u $NEW -n 30 --no-pager >&2
die "posd not ready after 30 s"
