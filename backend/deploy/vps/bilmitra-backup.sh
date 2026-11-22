#!/usr/bin/env bash
# Nightly logical backup of the BilMitra database (18/bilmitra cluster only).
# Installed as /usr/local/sbin/bilmitra-backup, run by bilmitra-backup.timer
# as the postgres user. Keeps the last 14 dumps in /var/www/bilmitra/backups.
# This is the single-VPS starting point; copy the dumps off the server (or
# move to pgBackRest/managed PITR, OPERATIONS §6.1) before real shops rely on it.
set -euo pipefail
umask 077
DIR=/var/www/bilmitra/backups
KEEP=14
out="$DIR/bilmitra-$(date -u +%Y%m%dT%H%M%SZ).dump"
pg_dump -p 5433 -d bilmitra --format=custom --compress=6 --file="$out.part"
mv "$out.part" "$out"
ls -1t "$DIR"/bilmitra-*.dump | tail -n +$((KEEP + 1)) | xargs -r rm -f
