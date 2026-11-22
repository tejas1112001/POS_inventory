#!/usr/bin/env bash
# Build a release from a git ref and deploy it to the VPS.
#   backend/deploy/vps/deploy.sh [git-ref]        (default: origin/main)
# Env: BILMITRA_SSH (default root@66.116.232.174), BILMITRA_SSH_KEY.
#
# Steps: export the ref (your working tree is not touched) → build posd,
# posctl and both SPAs → upload to /var/www/bilmitra/releases/<id> →
# migrate (platform, then tenants) → switch "current" → restart posd →
# health check. On a failed health check "current" is switched back.
set -euo pipefail

REF=${1:-origin/main}
HOST=${BILMITRA_SSH:-root@66.116.232.174}
KEY=${BILMITRA_SSH_KEY:-$HOME/.ssh/id_ed25519}
SSH=(ssh -o BatchMode=yes -i "$KEY" "$HOST")
REPO=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)

git -C "$REPO" fetch -q origin
COMMIT=$(git -C "$REPO" rev-parse --short=7 "$REF")
ID=$(date -u +%Y%m%d%H%M%S)-$COMMIT
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
echo "== building $REF ($COMMIT) as release $ID"

git -C "$REPO" archive "$COMMIT" | tar -x -C "$WORK"
mkdir -p "$WORK/out/bin" "$WORK/out/static"
LD="-s -w -X github.com/tejas1112001/POS_inventory/backend/internal/buildinfo.Version=$ID -X github.com/tejas1112001/POS_inventory/backend/internal/buildinfo.Commit=$COMMIT"
(cd "$WORK/backend" && for c in posd posctl; do
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LD" -o "$WORK/out/bin/$c" ./cmd/$c
done)
(cd "$WORK" && npm ci --no-audit --no-fund --loglevel=error >/dev/null)
(cd "$WORK/frontend" && VITE_API_URL= npm run build >/dev/null && VITE_API_URL= npm run build:admin >/dev/null)
# The React marketing site, branded at build time (landing/src/lib/config.js).
(cd "$WORK" && VITE_PLATFORM_NAME=BilMitra VITE_BASE_DOMAIN=bilmitra.com npm run build:landing >/dev/null)
cp -r "$WORK/frontend/dist" "$WORK/out/static/tenant"
cp -r "$WORK/frontend/dist-admin" "$WORK/out/static/admin"
cp -r "$WORK/landing/dist" "$WORK/out/static/landing"
echo "$ID" >"$WORK/out/RELEASE"

echo "== uploading"
tar -C "$WORK/out" -czf - . | "${SSH[@]}" "set -e; d=/var/www/bilmitra/releases/$ID; mkdir -p \$d; tar -xzf - -C \$d; chown -R root:root \$d; chmod -R go-w \$d"

echo "== migrating, switching and restarting"
"${SSH[@]}" bash -s -- "$ID" <<'EOF'
set -euo pipefail
ID=$1; R=/var/www/bilmitra
prev=$(readlink -f $R/current 2>/dev/null || true)
ln -sfn $R/releases/$ID $R/current.new && mv -Tf $R/current.new $R/current
bilmitra-ctl config check
bilmitra-ctl migrate platform up
bilmitra-ctl migrate tenants up
systemctl restart bilmitra
for i in $(seq 1 30); do
    if curl -fsS -m 2 -o /dev/null http://127.0.0.1:8090/readyz; then echo "ready: $ID"; break; fi
    sleep 1
    if [ "$i" = 30 ]; then
        echo "not ready after 30 s; rolling back to ${prev:-nothing}" >&2
        journalctl -u bilmitra -n 30 --no-pager >&2 || true
        [ -n "$prev" ] && ln -sfn "$prev" $R/current && systemctl restart bilmitra
        exit 1
    fi
done
# Keep the five newest releases.
ls -1dt $R/releases/*/ | tail -n +6 | xargs -r rm -rf
EOF
echo "== deployed $ID"
