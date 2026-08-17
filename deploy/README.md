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

# Backs crypto.Sealer: Plaid access tokens AND TOTP secrets. 64 hex chars.
# Moved here from plaid.encryption_key (a TOTP secret is not a Plaid thing);
# the old location is still read as a fallback for one release.
secrets:
  encryption_key: "..."       # SEE THE WARNING UNDER "Auth features" BELOW

log:
  level: "info"
```

`web.trusted_proxies` defaults to loopback, which is correct here: nginx
terminates TLS on the same host and proxies to `127.0.0.1:8080`. Leave it unset
unless that changes.

## Auth features

Everything below is **optional and off by default**. Migrations run
automatically on deploy, and a feature with no configuration logs a named
warning at boot rather than failing — so the app ships fine before any of this
is set, and each feature can be switched on independently.

Already working with no configuration at all: rate limiting, account lockout,
the active-sessions list, step-up re-authentication, the expired-row sweeper,
and every new email (they use the Resend setup above).

### Verify these first

```bash
# Per-IP rate limiting rests entirely on nginx appending the real client IP.
sudo nginx -T | grep X-Forwarded-For
# want: proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
```

nginx is NOT deployed by `deploy.sh` — `deploy/nginx/pigglet` is a reference
copy and the box is authoritative. The directive must **append**: the real
client then sits rightmost and a client-forged prefix is ignored. Passing the
header through untouched would let anyone mint a fresh rate-limit bucket per
request; setting none would make every user share one bucket, which is worse
than no limiter at all.

`web.base_url` now also builds OAuth redirect URIs and magic-link URLs, so a
stale value strands users mid-sign-in.

### Two-factor (TOTP + email codes)

Needs only `secrets.encryption_key` — copy the existing `plaid.encryption_key`
value into it.

> **Add this key to the backup runbook.** Losing it means every TOTP enrolment
> stops verifying and every bank link must be redone. Recovery codes are hashed
> rather than sealed precisely so they survive a key loss, which is the only
> reason it is recoverable at all.

### Passkeys

```yaml
passkeys:
  rp_id: "pigglet.ca"         # PERMANENT — see below
  rp_display_name: "Pigglet"
  origins:
    - "https://pigglet.ca"
    - "https://www.pigglet.ca"
    - "android:apk-key-hash:<base64url sha256, upload key>"
    - "android:apk-key-hash:<base64url sha256, Play App Signing key>"
  apple_app_ids: ["<TEAMID>.ca.pigglet.budget"]
  android_package: "ca.pigglet.budget"
  android_fingerprints:       # colon-hex — a DIFFERENT encoding of the same certs
    - "AB:CD:EF:..."
    - "12:34:56:..."
```

`rp_id` is a one-way door: credentials bind to it permanently, so it is the
apex with no scheme, port, or `www`. Picking `www.pigglet.ca` would exclude the
apex forever, and changing it later invalidates every registered passkey.

The Android signing certificates appear **twice in two encodings** — base64url
in `origins`, colon-separated hex in `android_fingerprints` — and there are
usually two of them (your upload key and Google's Play App Signing key). Get
this wrong and it fails *only* on Android while web and iOS work perfectly, so
test on a real device. Fingerprints come from Play Console → Test and release →
App integrity; the Apple Team ID from Apple Developer → Membership details.

The server publishes `/.well-known/apple-app-site-association` and
`/.well-known/assetlinks.json` from these values. The mobile apps still need
their own entitlements before in-app passkeys work.

### Sign in with Google

Create an OAuth **Web application** client in Google Cloud Console with the
redirect URI `https://pigglet.ca/auth/google/callback`.

```yaml
oauth:
  google:
    client_id: "....apps.googleusercontent.com"
    client_secret: "..."
    allowed_audiences:        # the iOS and Android client ids
      - "....apps.googleusercontent.com"
      - "....apps.googleusercontent.com"
```

`allowed_audiences` is a security control, not plumbing: an ID token is only
evidence about a person if it was minted for *this* application, and `aud` is
what says so. iOS and Android are separate OAuth clients with their own ids; the
web client id is accepted automatically. An empty list fails closed — native
sign-in simply will not work.

### Sign in with Apple

Create a **Services ID** (not the bundle id), enable Sign in with Apple on it,
register the return URL `https://pigglet.ca/auth/apple/callback`, then create a
Sign in with Apple key and download the `.p8` — offered exactly once.

```yaml
oauth:
  apple:
    client_id: "ca.pigglet.budget.service"   # Services ID
    team_id: "ABCDE12345"
    key_id: "KEY1234567"
    private_key: |
      -----BEGIN PRIVATE KEY-----
      ...contents of the .p8...
      -----END PRIVATE KEY-----
    allowed_audiences: ["ca.pigglet.budget"] # iOS bundle id
```

There is no client secret to rotate. Apple's is a signed JWT it rejects beyond
six months, so the app mints a fresh five-minute assertion per exchange instead
of caching a long-lived one behind a calendar reminder. Just keep the `.p8` safe.

Apple sends the user's email **only on the first authorization**, so retesting
needs the app revoked under Settings → Apple ID → Sign in with Apple.

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

### Checking which auth features are live

Each one announces itself when it is *not* configured, so no output is the goal:

```bash
ssh pigglet-ca-1 "journalctl -u pigglet -n 200 | grep -E 'disabled'"
#   "two-factor authentication disabled: secrets.encryption_key is not set"
#   "two-factor authentication disabled: bad secrets.encryption_key"
#   "passkeys disabled"
#   "federated sign-in disabled"
```

The two app-association documents must return 200 as `application/json`, with
no redirect:

```bash
curl -sSI https://pigglet.ca/.well-known/apple-app-site-association
curl -sSI https://pigglet.ca/.well-known/assetlinks.json
```

Worth walking by hand after enabling: enrol an authenticator and sign in with a
code; register a passkey in Safari *and* on a real Android device; request a
magic link and check the URL points at `pigglet.ca`; sign in with Google and
Apple; and fail a password eight times to confirm the lockout names a wait.
