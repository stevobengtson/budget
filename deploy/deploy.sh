#!/usr/bin/env bash
# Cross-compile the web binary for the server (linux/amd64), upload it, and
# restart the systemd service. Run from anywhere; paths resolve to the repo.
#
#   ./deploy/deploy.sh
#
# Requires the dev toolchain (run inside `devbox shell` so templ + tailwind are
# on PATH). Assets and migrations are embedded, so only the binary ships — the
# server's /etc/budget/budget.yaml is managed by hand and never touched here.
set -euo pipefail

SERVER="${SERVER:-pigglet-ca-1}" # ssh alias
REMOTE_BIN="/opt/budget/budget"
SERVICE="pigglet"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "→ Regenerating embedded assets (tailwind css + templ)…"
task css
task templ

echo "→ Cross-compiling ${SERVICE} for linux/amd64…"
OUT="bin/web/budget-linux-amd64"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o "$OUT" ./cmd/web
echo "   built $(du -h "$OUT" | cut -f1) → $OUT"

echo "→ Uploading to ${SERVER}…"
scp "$OUT" "${SERVER}:/tmp/budget.new"

echo "→ Installing binary, migrating, restarting service…"
# Atomic swap (install → rename) so we never truncate the running binary.
# Migrations run explicitly from the NEW binary before the restart: if one
# fails, the script aborts here and the OLD binary keeps serving on the old
# schema — instead of the service crash-looping after a blind restart. The
# app still migrates at boot too, which then finds nothing left to do.
ssh "$SERVER" bash -s <<EOF
set -euo pipefail
if [ "\$(id -u)" -eq 0 ]; then SUDO=; else SUDO=sudo; fi
\$SUDO install -o deploy -g deploy -m 0755 /tmp/budget.new "$REMOTE_BIN"
rm -f /tmp/budget.new
echo "  → running migrations…"
\$SUDO -u deploy "$REMOTE_BIN" db up --config /etc/budget/budget.yaml
\$SUDO -u deploy "$REMOTE_BIN" db status --config /etc/budget/budget.yaml | tail -n 3
\$SUDO systemctl restart "$SERVICE"
EOF

echo "→ Waiting for health…"
# The app answers /healthz only when it can reach Postgres; ten tries with a
# short pause covers a slow start without hiding a real failure.
for _ in {1..10}; do
  if ssh "$SERVER" 'curl -fsS -o /dev/null http://127.0.0.1:8080/healthz' 2>/dev/null; then
    HEALTHY=1
    break
  fi
  sleep 2
done
if [ "${HEALTHY:-}" != "1" ]; then
  echo "✗ Service failed health check after deploy — investigate:" >&2
  ssh "$SERVER" "sudo systemctl --no-pager --full status $SERVICE | head -n 15" >&2 || true
  exit 1
fi
ssh "$SERVER" "sudo systemctl --no-pager --full status $SERVICE | head -n 8"

echo "✓ Deployed + migrated + healthy. Logs: ssh ${SERVER} 'journalctl -u ${SERVICE} -f'"
