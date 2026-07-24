# Database backups → Hetzner Object Storage

Daily logical backups of the `pigglet` Postgres database, uploaded to a Hetzner
Object Storage bucket, with old backups expired automatically by a bucket
lifecycle rule.

- **What runs:** `pigglet-backup.timer` fires `pigglet-backup.service` (a
  `oneshot`) daily at 03:00 UTC, which runs `/opt/budget/backup.sh`.
- **What it does:** `pg_dump -Fc` → integrity-check → `aws s3 cp` to
  `s3://<bucket>/pigglet/pigglet-<timestamp>.dump`.
- **Retention:** a lifecycle rule on the bucket deletes objects older than
  **14 days** (server-side — the script never prunes). Change the window in
  `lifecycle.json` and re-apply.
- **Recovery point:** up to 24h of data loss in the worst case (last dump →
  failure). This is logical backup, not point-in-time recovery.

## One-time setup

All server steps run on the box: `ssh pigglet-ca-1`.

### 1. Create the bucket (Hetzner Cloud Console → Object Storage)

Create a bucket, e.g. `pigglet-backups`, in a location (`fsn1`, `nbg1`, or
`hel1`) and generate an **S3 credential** (access key + secret) for it. Note the
endpoint for that location: `https://<location>.your-objectstorage.com`.

Keep the bucket **private** (no public read) — it contains your whole database.

### 2. Install the AWS CLI v2

Ubuntu's `apt` ships the deprecated v1; install the official v2 bundle:

```bash
curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o /tmp/awscliv2.zip
sudo apt-get install -y unzip
unzip -q /tmp/awscliv2.zip -d /tmp && sudo /tmp/aws/install
aws --version   # aws-cli/2.x ...
```

### 3. Credentials file `/etc/budget/backup.env`

Root-owned, readable by the `postgres` service user, **not** world-readable. It
holds the S3 secret and is never committed to git.

```bash
sudo tee /etc/budget/backup.env >/dev/null <<'EOF'
PGDATABASE=pigglet
S3_BUCKET=pigglet-backups
AWS_ENDPOINT_URL=https://fsn1.your-objectstorage.com
AWS_DEFAULT_REGION=fsn1
AWS_ACCESS_KEY_ID=REPLACE_ME
AWS_SECRET_ACCESS_KEY=REPLACE_ME
EOF
sudo chown root:postgres /etc/budget/backup.env
sudo chmod 640 /etc/budget/backup.env
```

Match `AWS_ENDPOINT_URL` / `AWS_DEFAULT_REGION` to your bucket's location.

### 4. Install the script and systemd units (from this repo)

```bash
# from the repo root, on the server (or scp these files up)
sudo install -o root -g root -m 0755 deploy/backup/pigglet-backup.sh /opt/budget/backup.sh
sudo cp deploy/backup/pigglet-backup.service /etc/systemd/system/
sudo cp deploy/backup/pigglet-backup.timer   /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now pigglet-backup.timer
```

### 5. Apply the retention rule (once)

The lifecycle rule lives on the bucket, not in systemd. Apply it once with the
same credentials (run wherever the AWS CLI + `/etc/budget/backup.env` are, e.g.
`sudo -i` then source the env, or pass the keys inline):

```bash
set -a; source /etc/budget/backup.env; set +a
aws s3api put-bucket-lifecycle-configuration \
  --bucket "$S3_BUCKET" \
  --endpoint-url "$AWS_ENDPOINT_URL" \
  --lifecycle-configuration file://deploy/backup/lifecycle.json

# verify:
aws s3api get-bucket-lifecycle-configuration --bucket "$S3_BUCKET" --endpoint-url "$AWS_ENDPOINT_URL"
```

To change retention, edit `Expiration.Days` in `lifecycle.json` and re-run the
`put` command.

## Test it

Run the backup on demand and watch it:

```bash
sudo systemctl start pigglet-backup.service
journalctl -u pigglet-backup.service -n 20 --no-pager

# confirm the object landed:
set -a; source /etc/budget/backup.env; set +a
aws s3 ls "s3://$S3_BUCKET/pigglet/" --endpoint-url "$AWS_ENDPOINT_URL"

# see the schedule:
systemctl list-timers pigglet-backup.timer
```

## Restore runbook

Backups are `pg_dump` custom-format files — restore with `pg_restore`.

```bash
set -a; source /etc/budget/backup.env; set +a

# 1. Pick and download a backup:
aws s3 ls "s3://$S3_BUCKET/pigglet/" --endpoint-url "$AWS_ENDPOINT_URL"
aws s3 cp "s3://$S3_BUCKET/pigglet/pigglet-YYYYMMDD-HHMMSSZ.dump" /tmp/restore.dump \
  --endpoint-url "$AWS_ENDPOINT_URL"

# 2a. Restore INTO A NEW database first to verify (safest):
sudo -u postgres createdb pigglet_restore
sudo -u postgres pg_restore --no-owner --no-privileges -d pigglet_restore /tmp/restore.dump

# 2b. Or overwrite the live database (DESTRUCTIVE — stop the app first):
sudo systemctl stop pigglet
sudo -u postgres dropdb pigglet
sudo -u postgres createdb pigglet -O pigglet_user
sudo -u postgres pg_restore --no-owner --no-privileges -d pigglet /tmp/restore.dump
sudo -u postgres psql -d pigglet -c "ALTER SCHEMA public OWNER TO pigglet_user;"
sudo systemctl start pigglet

rm -f /tmp/restore.dump
```

`--no-owner --no-privileges` lets you restore regardless of the original role
names; the app's schema ownership is re-set explicitly (as in the deploy
README).

> **Test a restore periodically.** An untested backup is a hope, not a backup.
> Do the 2a "restore into a scratch DB" flow a couple of times a year.

## Known gap: failure notifications

systemd timers are **silent on failure** — if a nightly backup errors (bad
credentials, bucket full, DB down), nothing tells you; you just quietly stop
having fresh backups. Options, in order of effort:

- **Manual:** occasionally check `aws s3 ls` (newest object should be < 24h old)
  or `systemctl status pigglet-backup.service`.
- **Automatic (recommended once stable):** add an `OnFailure=` drop-in that
  triggers a small unit which emails you (the app already sends mail via
  Resend), or point it at a healthcheck/monitoring service that alerts on a
  *missed* check-in (dead-man's-switch style, e.g. healthchecks.io).

This wasn't built here to keep the initial surface small — flag it if you'd like
the `OnFailure` email unit added.
