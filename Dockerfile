# syntax=docker/dockerfile:1.7

# ── Build stage ─────────────────────────────────────────────────────────────
# Debian-based builder (not alpine) because the Tailwind v4 standalone CLI
# is built on Bun, which is dynamically linked against glibc and refuses to
# run under musl. ca-certificates + curl are needed to fetch the tailwind
# release tarball; git is needed by `go install`.
FROM golang:1.26-bookworm AS builder

RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache module downloads in a separate layer.
COPY go.mod go.sum ./
RUN go mod download

# Templ CLI matches the version used in development.
RUN go install github.com/a-h/templ/cmd/templ@v0.3.1001

# Copy the rest of the source.
COPY . .

# Tailwind CSS v4 standalone CLI + asset compilation + templ generation +
# go build all happen in one layer so the architecture-dependent tailwind
# binary stays paired with its use site (avoids stale-cache mismatches when
# the host arch changes between builds). The asset is picked from the
# builder's own architecture so this works under docker buildx, Apple's
# `container build`, and plain docker build without depending on BuildKit's
# automatic platform ARGs.
ARG TAILWIND_VERSION=v4.3.0
RUN set -eux; \
    case "$(uname -m)" in \
      x86_64|amd64)  asset=tailwindcss-linux-x64 ;; \
      aarch64|arm64) asset=tailwindcss-linux-arm64 ;; \
      *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /usr/local/bin/tailwindcss \
      "https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/${asset}"; \
    chmod +x /usr/local/bin/tailwindcss; \
    /usr/local/bin/tailwindcss -i ./pkg/shadcntempl/tailwind/input.css -o ./internal/web/static/app.css --minify; \
    templ generate -path ./pkg/shadcntempl; \
    templ generate -path ./internal/web; \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/budget ./cmd/budget

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
