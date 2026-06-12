# syntax=docker/dockerfile:1.7

# ── Build stage ─────────────────────────────────────────────────────────────
FROM golang:1.26-bookworm AS builder

RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache module downloads in a separate layer.
COPY go.mod go.sum ./
RUN go mod download

# Build the JSON API.
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/budget ./cmd/api

# ── Runtime stage ───────────────────────────────────────────────────────────
FROM alpine:3.20

# CA bundle so the binary can talk to remote Postgres servers over TLS.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 budget \
 && mkdir -p /data \
 && chown -R budget:budget /data

COPY --from=builder /out/budget /usr/local/bin/budget

USER budget
WORKDIR /home/budget

# Sensible defaults — override with `docker run -e ...` or compose env.
# Multi-user deployments should point BUDGET_DB_DSN at the Postgres budget_api
# database, and BUDGET_AUTH_JWKS_URL / BUDGET_AUTH_ISSUER / BUDGET_AUTH_AUDIENCE
# at the BetterAuth (web) service so the API can verify JWTs.
ENV BUDGET_WEB_ADDR=":8080" \
    BUDGET_DB_DSN="/data/budget.db"

# Volume for SQLite users; ignore when using Postgres.
VOLUME ["/data"]

EXPOSE 8080

# Bare invocation launches the API server; subcommands (config/db/migrate)
# are available via `docker run ... <cmd>`.
ENTRYPOINT ["budget"]
