#!/usr/bin/env bash
# Disk-space guardrail for the single-box deploy. On a box where Postgres data,
# WAL, and backup temp files share one disk, a full disk is the most likely hard
# outage — so check usage on a timer and alert before it bites.
#
# Alerting reuses healthchecks.io (already used by the backup): a healthy check
# pings the base URL; a breach pings /fail (with the usage in the body). That
# gives two alerts for free — an explicit "disk is high" AND a missed-heartbeat
# alert if this timer ever stops running. HC_DISK_URL unset = no-op ping, and
# the non-zero exit still surfaces via `systemctl status` / journald.
set -euo pipefail

# Filesystem to watch. Defaults to root; set DISK_PATH if Postgres or the backup
# temp dir live on a separate mount.
DISK_PATH="${DISK_PATH:-/}"
# Alert at/above this percent used.
DISK_THRESHOLD="${DISK_THRESHOLD:-85}"
# Optional healthchecks.io check URL (a SECOND check, distinct from the backup).
HC_DISK_URL="${HC_DISK_URL:-}"

hc_ping() { # $1 = url suffix ("" ok, "/fail"), $2 = body
  [ -n "$HC_DISK_URL" ] || return 0
  curl -fsS -m 10 --retry 3 -o /dev/null --data-raw "$2" "${HC_DISK_URL}${1}" || true
}

# POSIX df (-P guarantees a fixed layout on both GNU and BSD): the capacity
# column is field 5, e.g. "83%"; strip the percent sign.
used="$(df -P "$DISK_PATH" | awk 'NR==2 {gsub(/%/,"",$5); print $5}')"
msg="disk ${DISK_PATH} at ${used}% (threshold ${DISK_THRESHOLD}%)"

if [ "$used" -ge "$DISK_THRESHOLD" ]; then
  echo "✗ $msg" >&2
  hc_ping "/fail" "$msg"
  exit 1
fi

echo "✓ $msg"
hc_ping "" "$msg"
