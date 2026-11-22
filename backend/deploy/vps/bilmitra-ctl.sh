#!/usr/bin/env bash
# posctl with BilMitra's production settings. Installed as
# /usr/local/sbin/bilmitra-ctl (root only). Examples:
#   bilmitra-ctl config check
#   bilmitra-ctl --admin db bootstrap        # needs the cluster admin login
#   bilmitra-ctl migrate platform up
#   bilmitra-ctl migrate tenants up
#   bilmitra-ctl tenant provision --slug demo --name "Demo Shop" --owner-email you@example.com
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
set -a
# shellcheck disable=SC1091
. /var/www/bilmitra/shared/.env
if [ "${1:-}" = "--admin" ]; then
    shift
    # shellcheck disable=SC1091
    . /root/bilmitra-admin.env
fi
set +a
export SSL_CERT_DIR=/etc/ssl/certs:/var/www/bilmitra/shared/tls   # local S3 storage TLS
exec /var/www/bilmitra/current/bin/posctl "$@"
