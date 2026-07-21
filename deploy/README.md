# Deploying to pigglet.ca (Hetzner, linux/amd64)

The web app compiles to a **single self-contained binary**: static assets,
templates, and Postgres migrations are all embedded, and migrations run
automatically on boot. Deploying is therefore: cross-compile → copy one file →
restart. nginx (already configured) terminates TLS and proxies to
`127.0.0.1:8080`.

## One-time server setup

Run these on the server (`ssh pigglet-ca-1`) once. `budget.yaml` holds your
secrets and is created by hand — it is never in git and never pushed by the
deploy script.

```bash
# 1. Dedicated, non-login service account
sudo useradd --system --no-create-home --shell /usr/sbin/nologin deploy

# 2. Directories
sudo mkdir -p /opt/budget /etc/budget

# 3. Postgres database + owner (use the password from your password manager)
sudo -u postgres psql -c "CREATE USER pigglet_user WITH PASSWORD 'REPLACE_ME';"
sudo -u postgres psql -c "CREATE DATABASE pigglet OWNER pigglet_user;"
# Postgres 15+ revoked CREATE on the public schema from non-owners, so the app
# role can't create its tables (goose fails with "permission denied for schema
# public") until we grant it. budget owns the DB, so hand it the schema too:
sudo -u postgres psql -d pigglet -c "ALTER SCHEMA public OWNER TO pigglet_user;"

# 4. Production config — see the template below. Root-owned, group-readable
#    by the service account, not world-readable (it contains secrets).
sudo nano /etc/budget/budget.yaml
sudo chown root:deploy /etc/budget/budget.yaml
sudo chmod 640 /etc/budget/budget.yaml

# 5. Install the systemd unit (from this repo) and enable it
sudo cp deploy/pigglet.service /etc/systemd/system/pigglet.service
sudo systemctl daemon-reload
sudo systemctl enable pigglet
```

Then run `./deploy/deploy.sh` from your Mac (see below) to push the first
binary and start the service.

## Production `budget.yaml`

Place at `/etc/budget/budget.yaml`. These values differ from the checked-in dev
config for a public HTTPS deployment:

```yaml
db:
  dsn: "postgres://pigglet_user:REPLACE_ME@127.0.0.1:5432/pigglet?sslmode=disable"

web:
  addr: "127.0.0.1:8080"      # loopback only — reachable via nginx, not the internet
  base_url: "https://pigglet.ca"
  level: "release"            # Gin release mode

mail:
  driver: "resend"            # "console" only logs; use resend to actually send auth mail
  from: "Pigglet <support@pigglet.ca>"
  resend_api_key: "re_..."    # ROTATED key — the old one was committed to git

auth:
  session_ttl: "720h"
  token_ttl: "1h"
  cookie_secure: true         # HTTPS-only cookies

log:
  level: "info"
```

## Routine deploys

From the repo root, inside `devbox shell` (so `templ`/`tailwind` are available):

```bash
./deploy/deploy.sh
```

It regenerates embedded CSS/templates, cross-compiles `linux/amd64`, uploads to
`/tmp`, atomically swaps `/opt/budget/budget`, and restarts the service.

Override the target with `SERVER=other-host ./deploy/deploy.sh`.

## Operations

```bash
ssh pigglet-ca-1 'journalctl -u pigglet -f'          # tail logs
ssh pigglet-ca-1 'sudo systemctl restart pigglet'    # restart
ssh pigglet-ca-1 'sudo systemctl status pigglet'     # status
```

The server logs `budget web — schema dialect=postgres version=N` on boot; if a
deploy adds migrations, that version bumps automatically.
