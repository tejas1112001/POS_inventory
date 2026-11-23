#!/usr/bin/env bash
# posctl with MintoBill's production settings. Installed as
# /usr/local/sbin/mintobill-ctl (root only). Examples:
#   mintobill-ctl config check
#   mintobill-ctl --admin db bootstrap        # needs the cluster admin login
#   mintobill-ctl migrate platform up
#   mintobill-ctl migrate tenants up
#   mintobill-ctl tenant provision --slug demo --name "Demo Shop" --owner-email you@example.com
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
set -a
# shellcheck disable=SC1091
. /var/www/mintobill/shared/.env
if [ "${1:-}" = "--admin" ]; then
    shift
    # shellcheck disable=SC1091
    . /root/mintobill-admin.env
fi
set +a
export SSL_CERT_DIR=/etc/ssl/certs:/var/www/mintobill/shared/tls   # local S3 storage TLS
exec /var/www/mintobill/current/bin/posctl "$@"
