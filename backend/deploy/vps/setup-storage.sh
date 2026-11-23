#!/usr/bin/env bash
# Interim local S3 storage for MintoBill (owner-approved 2026-11-22, until
# Cloudflare R2). Idempotent; run as root after setup-server.sh:
#   bash setup-storage.sh <deploy/vps dir>
# Expects /opt/mintobill-s3/versitygw (v1.9.0, checksum-verified release).
# Moving to R2 later: copy /var/www/mintobill/data/s3/mintobill/ to the R2
# bucket (rclone), set the S3_* values in shared/.env, restart mintobill,
# then `systemctl disable --now mintobill-s3`.
set -euo pipefail
umask 077
SRC=${1:?usage: setup-storage.sh <deploy/vps dir>}
ENVF=/var/www/mintobill/shared/.env
log() { printf '== %s\n' "$*"; }

[ -x /opt/mintobill-s3/versitygw ] || { echo "missing /opt/mintobill-s3/versitygw" >&2; exit 1; }
id mintobill-s3 >/dev/null 2>&1 || useradd --system --no-create-home --home-dir /var/www/mintobill/data/s3 --shell /usr/sbin/nologin mintobill-s3
install -d -m 0755 -o root -g root /var/www/mintobill/data
install -d -m 0700 -o mintobill-s3 -g mintobill-s3 /var/www/mintobill/data/s3
install -d -m 0750 -o root -g mintobill-s3 /etc/mintobill-s3 /etc/mintobill-s3/tls

if [ ! -f /etc/mintobill-s3/tls/server.crt ]; then
    log "issuing storage TLS certificate (localhost, 127.0.0.1) from the MintoBill CA"
    t=$(mktemp -d)
    openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -subj "/CN=localhost" \
        -keyout $t/server.key -out $t/server.csr 2>/dev/null
    printf 'subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n' >$t/ext
    openssl x509 -req -in $t/server.csr -CA /root/mintobill-ca/ca.crt -CAkey /root/mintobill-ca/ca.key \
        -CAcreateserial -days 825 -extfile $t/ext -out $t/server.crt 2>/dev/null
    install -m 0640 -o root -g mintobill-s3 $t/server.crt /etc/mintobill-s3/tls/server.crt
    install -m 0640 -o root -g mintobill-s3 $t/server.key /etc/mintobill-s3/tls/server.key
    rm -rf "$t"
fi

if [ ! -f /etc/mintobill-s3/env ]; then
    log "generating storage access keys (never printed)"
    ak="MB$(openssl rand -hex 9 | tr a-f A-F)"
    sk=$(openssl rand -base64 30 | tr -d '\n/+=' | cut -c1-40)
    {
        printf 'ROOT_ACCESS_KEY_ID=%s\nROOT_SECRET_ACCESS_KEY=%s\n' "$ak" "$sk"
        printf 'VGW_CERT=/etc/mintobill-s3/tls/server.crt\nVGW_KEY=/etc/mintobill-s3/tls/server.key\nVGW_REGION=us-east-1\n'
    } >/etc/mintobill-s3/env
    chown root:mintobill-s3 /etc/mintobill-s3/env
    chmod 0640 /etc/mintobill-s3/env
    log "pointing MintoBill at the local storage"
    cp -a $ENVF /root/mintobill-backup-20261122/env.before-s3
    sed -i -E \
        -e 's|^S3_ENDPOINT=.*|S3_ENDPOINT=https://localhost:7070|' \
        -e 's|^S3_BUCKET=.*|S3_BUCKET=mintobill|' \
        -e "s|^S3_ACCESS_KEY=.*|S3_ACCESS_KEY=\"$ak\"|" \
        -e "s|^S3_SECRET_KEY=.*|S3_SECRET_KEY=\"$sk\"|" \
        -e 's|^S3_REGION=.*|S3_REGION=us-east-1|' $ENVF
    chown root:mintobill $ENVF
    chmod 0640 $ENVF
fi

install -m 0644 "$SRC/mintobill-s3.service" /etc/systemd/system/mintobill-s3.service
install -m 0644 "$SRC/mintobill.service" /etc/systemd/system/mintobill.service
systemctl daemon-reload
systemctl enable --quiet --now mintobill-s3
for i in $(seq 1 15); do
    curl -fsS -m 2 --cacert /var/www/mintobill/shared/tls/ca.crt https://localhost:7070/health >/dev/null 2>&1 && { log "storage up (${i}s)"; exit 0; }
    sleep 1
done
echo "storage did not come up" >&2
journalctl -u mintobill-s3 -n 20 --no-pager >&2
exit 1
