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

echo "→ Installing binary + restarting service…"
# Atomic swap (install → rename) so we never truncate the running binary.
ssh "$SERVER" bash -s <<EOF
set -euo pipefail
if [ "\$(id -u)" -eq 0 ]; then SUDO=; else SUDO=sudo; fi
\$SUDO install -o deploy -g deploy -m 0755 /tmp/budget.new "$REMOTE_BIN"
rm -f /tmp/budget.new
\$SUDO systemctl restart "$SERVICE"
sleep 1
\$SUDO systemctl --no-pager --full status "$SERVICE" | head -n 15
EOF

echo "✓ Deployed. Logs: ssh ${SERVER} 'journalctl -u ${SERVICE} -f'"
