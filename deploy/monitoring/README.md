# Host monitoring

Lightweight, agentless host checks for the single-box deploy — sized for a 2 GB
CPX12 where a full monitoring agent (Netdata, Better Stack Collector) would eat
too much RAM alongside Postgres + the app.

## Disk-space guardrail

On a single box, Postgres data + WAL + backup temp files share one disk, so a
full disk is the most likely hard outage. `disk-check.sh` runs on a timer and
alerts **before** that happens, reusing the healthchecks.io alerting already set
up for backups.

- **What runs:** `pigglet-disk-check.timer` fires `pigglet-disk-check.service`
  (a `oneshot`) every 30 min → `/opt/budget/disk-check.sh`.
- **What it does:** reads `df` for the watched filesystem; if usage ≥ threshold
  it pings the healthchecks.io check's `/fail` endpoint (usage in the body) and
  exits non-zero; otherwise it pings the base URL (healthy).
- **Two alerts for the price of one:** an explicit *disk-is-high* alert (`/fail`),
  and a *missed-heartbeat* alert if the timer itself ever stops running.

### Setup

```bash
# 1. Create a SECOND healthchecks.io check (separate from the backup one):
#    period 30 min, grace ~15 min. Add your email/Slack alert.

# 2. Add its URL + (optional) threshold to the shared ops env file:
#    HC_DISK_URL=https://hc-ping.com/your-disk-check-uuid
#    DISK_THRESHOLD=85        # optional, default 85
#    DISK_PATH=/              # optional, default / (set if PG/backups are elsewhere)
sudo nano /etc/budget/backup.env

# 3. Install the script + units (repo not checked out on the server → scp up):
scp deploy/monitoring/disk-check.sh pigglet-ca-1:/tmp/disk-check.sh
ssh pigglet-ca-1 'sudo install -o root -g root -m 0755 /tmp/disk-check.sh /opt/budget/disk-check.sh && rm /tmp/disk-check.sh'
scp deploy/monitoring/pigglet-disk-check.{service,timer} pigglet-ca-1:/tmp/
ssh pigglet-ca-1 'sudo mv /tmp/pigglet-disk-check.{service,timer} /etc/systemd/system/ \
  && sudo systemctl daemon-reload \
  && sudo systemctl enable --now pigglet-disk-check.timer'
```

`HC_DISK_URL` is optional: leave it unset and the check still runs and logs
locally (visible via `systemctl status`), just without the remote alert.

### Test

```bash
# Run once and watch:
ssh pigglet-ca-1 'sudo systemctl start pigglet-disk-check.service'
ssh pigglet-ca-1 'journalctl -u pigglet-disk-check.service -n 5 --no-pager'
# → "✓ disk / at NN% (threshold 85%)" and the healthchecks.io check goes green.

# Force a breach to confirm the alert fires (temporary low threshold):
ssh pigglet-ca-1 'sudo systemd-run --wait -p User=postgres \
  --property=EnvironmentFile=/etc/budget/backup.env -E DISK_THRESHOLD=1 \
  /opt/budget/disk-check.sh'
# → exits non-zero, pings /fail, you get an alert.

# Schedule:
ssh pigglet-ca-1 'systemctl list-timers pigglet-disk-check.timer'
```

## Requests/min + logs → Grafana Cloud Loki (Vector)

`vector.yaml` ships the app's structured JSON request logs (from journald) to
**Grafana Cloud Loki**. This delivers the **requests/min** view and, in the same
move, gets logs **off the box** (searchable + retained) instead of journald-only.
Native Vector agent (~30–50 MB, no Docker).

> **Why Grafana Cloud, not Better Stack?** Better Stack removed the card-free log
> tier (now ~$34/mo pay-as-you-go). Grafana Cloud's free tier needs **no card**
> (~50 GB logs / 30-day retention). Its own heavy "Collector" (and Netdata) were
> rejected earlier as too big for this 2 GB box — a plain Vector agent is the fit.

The app already logs one JSON line per request (`method`/`path`/`status`/
`dur_ms`/`ip`, via `slog` → journald). Vector parses those into fields Loki can
query with `| json`.

### Setup

```bash
# 1. Install Vector (current Datadog-hosted apt repo — NOT the old timber.io one):
ssh pigglet-ca-1 'bash -c "$(curl -L https://setup.vector.dev)" && sudo apt-get update && sudo apt-get install -y vector'
#    If apt "cannot locate package vector" (no build for a brand-new Ubuntu
#    codename yet), install the .deb from github.com/vectordotdev/vector/releases/latest:
#      curl -LO .../vector_<VER>-1_amd64.deb && sudo apt-get install -y ./vector_<VER>-1_amd64.deb

# 2. Let the vector user read journald:
ssh pigglet-ca-1 'sudo usermod -aG systemd-journal vector'

# 3. Config:
scp deploy/monitoring/vector.yaml pigglet-ca-1:/tmp/vector.yaml
ssh pigglet-ca-1 'sudo mv /tmp/vector.yaml /etc/vector/vector.yaml'

# 4. Credentials in /etc/default/vector. Get all three from the SAME Grafana
#    Cloud Loki "Send Logs" page (Cloud Portal → your stack → Loki):
ssh pigglet-ca-1 'sudo tee /etc/default/vector >/dev/null <<EOF
GRAFANA_LOKI_URL=https://logs-prod-018.grafana.net   # base host, no /loki/... path
GRAFANA_LOKI_USER=1694028                            # the Loki (logs) instance ID
GRAFANA_LOKI_TOKEN=glc_...                           # access-policy token, logs:write
EOF'
ssh pigglet-ca-1 'sudo chmod 640 /etc/default/vector'

# 5. Bake the creds into vector.yaml (see "gotcha" below — ${VAR} interpolation
#    does NOT work on this box), lock down, validate, enable:
ssh pigglet-ca-1 'sudo tee /tmp/bake.py >/dev/null <<PY
import os
p="/etc/vector/vector.yaml"; s=open(p).read()
for k in ("GRAFANA_LOKI_URL","GRAFANA_LOKI_USER","GRAFANA_LOKI_TOKEN"):
    s=s.replace("\${%s}"%k, os.environ[k])
open(p,"w").write(s)
PY'
ssh pigglet-ca-1 'sudo bash -c "set -a; . /etc/default/vector; set +a; python3 /tmp/bake.py"; sudo rm /tmp/bake.py'
ssh pigglet-ca-1 'sudo chown root:vector /etc/vector/vector.yaml && sudo chmod 640 /etc/vector/vector.yaml'
ssh pigglet-ca-1 'sudo -E vector validate /etc/vector/vector.yaml && sudo systemctl enable --now vector'
```

### Validate the Loki creds with curl FIRST (expect HTTP 204)

Do this **before** wiring Vector — it isolates credential problems from config:

```bash
curl -sS -w '\n%{http_code}\n' -u "USER:TOKEN" -H "Content-Type: application/json" \
  -d '{"streams":[{"stream":{"service":"pigglet-test"},"values":[["'"$(date +%s)000000000"'","test"]]}]}' \
  https://logs-prod-018.grafana.net/loki/api/v1/push
```

The **URL + USER + TOKEN baked into Vector must be byte-identical to the trio in
a `204` curl.** Every failure in this setup was a mismatch in that trio.

### Gotchas (all hit during setup — read before debugging)

- **`${VAR}` interpolation does NOT work in this Vector build** — it leaves the
  literal `${...}` in place (→ "invalid uri character" / auth with a literal
  token string). Hence the bake step that substitutes real values in.
- **Loki labels can't be a bare `{{ field }}`** ("template references event
  fields but has no literal string prefix"). Keep labels static (`service:
  pigglet`); everything else lives in the JSON line, queried via `| json`.
- **Read vs write token:** the token on the Loki *data source* page is
  `logs:read`; ingestion needs a `logs:write` access-policy token. A read token
  → `invalid scope requested`.
- **Region + instance ID must match the token:** `logs-prod-NNN` and the numeric
  `USER` both come from the same "Send Logs" page; a mismatch → `401`.

### Requests/min in Grafana (Explore → Loki datasource)

- Logs: `{service="pigglet"}`
- **Requests/min:** `sum(count_over_time({service="pigglet"} |= "http request" [1m]))`
- Error rate: `sum(count_over_time({service="pigglet"} | json | status >= 500 [1m]))`
- p95 latency: `quantile_over_time(0.95, {service="pigglet"} | json | unwrap dur_ms [5m])`

Only `pigglet.service` is shipped; add `pigglet-backup`/`pigglet-disk-check` to
`include_units` to centralize those ops logs too.

## Later: full host metrics

If you outgrow disk-only and want CPU / memory / I/O history, extend this same
Vector agent with a `host_metrics` source → a Grafana Cloud Prometheus/Mimir
endpoint.
