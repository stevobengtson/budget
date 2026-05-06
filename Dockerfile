# syntax=docker/dockerfile:1.7

# ── Build stage ─────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

# git is needed by `go install`; ca-certificates so https module fetches work.
RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache module downloads in a separate layer.
COPY go.mod go.sum ./
RUN go mod download

# Templ CLI matches the version used in development.
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1001

# Copy the rest of the source.
COPY . .

# Generate Templ output and produce a static binary. CGO is off because
# both database drivers (modernc/sqlite, jackc/pgx/v5) are pure Go.
RUN templ generate ./internal/web \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/budget ./cmd/budget

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
ENV BUDGET_WEB_ADDR=":8080" \
    BUDGET_DB_DSN="/data/budget.db"

# Volume for SQLite users (or to persist migrations metadata locally).
# When using Postgres simply ignore this volume.
VOLUME ["/data"]

EXPOSE 8080

# Run `budget web` by default. Pass other subcommands via `docker run ... <cmd>`.
ENTRYPOINT ["budget"]
CMD ["web"]
