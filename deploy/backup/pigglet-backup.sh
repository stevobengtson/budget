#!/usr/bin/env bash
# Daily logical backup of the Pigglet Postgres database to Hetzner Object
# Storage (S3-compatible). Invoked by pigglet-backup.service (a systemd
# oneshot triggered by pigglet-backup.timer).
#
# Runs as the `postgres` OS user, so pg_dump connects over the local socket
# via peer auth — no DB password needed. S3 credentials + target come from
# /etc/budget/backup.env (loaded by the unit's EnvironmentFile=).
#
# Retention is NOT handled here: a bucket lifecycle rule (lifecycle.json)
# expires old objects server-side. This script only ever uploads.
set -euo pipefail

# --- Required environment (from /etc/budget/backup.env) ---------------------
: "${PGDATABASE:?set PGDATABASE in /etc/budget/backup.env}"
: "${S3_BUCKET:?set S3_BUCKET in /etc/budget/backup.env}"
: "${AWS_ENDPOINT_URL:?set AWS_ENDPOINT_URL in /etc/budget/backup.env}"
# AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_DEFAULT_REGION are read by
# the aws CLI directly from the environment.

# Smallest plausible good dump. A healthy custom-format dump of even an empty
# schema is several KB; anything under this means the dump is truncated/bogus.
MIN_BYTES="${MIN_BYTES:-2048}"

timestamp="$(date -u +%Y%m%d-%H%M%SZ)"
key="pigglet-db/pigglet-${timestamp}.dump"

# --- Dead-man's-switch (optional) -------------------------------------------
# If HC_PING_URL is set to a healthchecks.io check URL, signal start / success /
# failure so a MISSED or FAILED nightly backup raises an alert (that's the whole
# point — a silent backup failure is the dangerous one). Unset = no-op, so the
# backup behaves identically whether or not monitoring is configured.
HC_PING_URL="${HC_PING_URL:-}"
hc_ping() { # $1 = url suffix: "" (success), "/start", or "/fail"
  [ -n "$HC_PING_URL" ] || return 0
  curl -fsS -m 10 --retry 3 -o /dev/null "${HC_PING_URL}${1}" || true
}

tmp="$(mktemp -t pigglet-backup.XXXXXX.dump)"
# On any exit: remove the temp dump, then tell healthchecks whether we succeeded
# (rc 0) or failed. rc is captured FIRST — the rm would otherwise reset $?.
cleanup() {
  local rc=$?
  rm -f "$tmp"
  if [ "$rc" -eq 0 ]; then hc_ping ""; else hc_ping "/fail"; fi
}
trap cleanup EXIT

hc_ping "/start"

echo "→ Dumping database '${PGDATABASE}' (custom format)…"
pg_dump --format=custom --no-owner --no-privileges "$PGDATABASE" >"$tmp"

# --- Integrity gate ---------------------------------------------------------
# Verify the dump BEFORE it leaves the box, so a corrupt/partial file never
# overwrites our good backups. Two cheap checks: a size floor, and that the
# custom-format table-of-contents is actually readable.
#
# NOTE: adjust the strategy here to taste — e.g. raise MIN_BYTES to a realistic
# floor for your data, or restore into a scratch database for a full test.
size="$(stat -c %s "$tmp")"
if ((size < MIN_BYTES)); then
  echo "✗ Dump is only ${size} bytes (< ${MIN_BYTES}); refusing to upload." >&2
  exit 1
fi
if ! pg_restore --list "$tmp" >/dev/null 2>&1; then
  echo "✗ Dump failed pg_restore --list integrity check; refusing to upload." >&2
  exit 1
fi
echo "   dump ok: ${size} bytes"

# --- Upload -----------------------------------------------------------------
echo "→ Uploading to s3://${S3_BUCKET}/${key}…"
aws s3 cp "$tmp" "s3://${S3_BUCKET}/${key}" \
  --endpoint-url "$AWS_ENDPOINT_URL" \
  --only-show-errors

echo "✓ Backup complete: s3://${S3_BUCKET}/${key} (${size} bytes)"
