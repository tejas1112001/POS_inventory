#!/usr/bin/env bash
# Nightly logical backup of the MintoBill database (18/mintobill cluster only).
# Installed as /usr/local/sbin/mintobill-backup, run by mintobill-backup.timer
# as the postgres user. Keeps the last 14 dumps in /var/www/mintobill/backups.
# This is the single-VPS starting point; copy the dumps off the server (or
# move to pgBackRest/managed PITR, OPERATIONS §6.1) before real shops rely on it.
set -euo pipefail
umask 077
DIR=/var/www/mintobill/backups
KEEP=14
out="$DIR/mintobill-$(date -u +%Y%m%dT%H%M%SZ).dump"
pg_dump -p 5433 -d mintobill --format=custom --compress=6 --file="$out.part"
mv "$out.part" "$out"
ls -1t "$DIR"/mintobill-*.dump | tail -n +$((KEEP + 1)) | xargs -r rm -f
