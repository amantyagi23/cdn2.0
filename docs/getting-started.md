# Getting Started

This guide walks a new developer through setting up the project locally from scratch.

---

## Prerequisites

### Required

| Tool | Minimum version | Install |
|---|---|---|
| Go | 1.22 | [go.dev/dl](https://go.dev/dl/) |
| PostgreSQL | 14 | [postgresql.org](https://www.postgresql.org/download/) or via Docker |
| Redis | 7 | [redis.io](https://redis.io/download) or via Docker |
| NATS | 2 (with JetStream) | [nats.io](https://docs.nats.io/running-a-nats-service/introduction/installation) or via Docker |

### Recommended

| Tool | Purpose | Install |
|---|---|---|
| Docker + Compose v2 | Run Postgres / Redis / NATS without local installation | [docs.docker.com](https://docs.docker.com/get-started/) |
| `golang-migrate` CLI | Apply SQL migrations | `go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest` |
| `air` | Live-reload during development | `go install github.com/air-verse/air@latest` |
| `jq` | Pretty-print JSON in curl examples | Package manager |

---

## 1 — Clone the Repository

```bash
git clone <repo-url> cdn-network
cd cdn-network
```

---

## 2 — Install Go Dependencies

```bash
go mod download
```

---

## 3 — Configure Environment Variables

```bash
cp .env.example .env
```

Open `.env` in your editor. At minimum, fill in:

```dotenv
# PostgreSQL
POSTGRES_USER=cdn_user
POSTGRES_PASSWORD=<choose-a-password>
POSTGRES_DB=cdn_network

# Redis
REDIS_PASSWORD=<choose-a-password>

# Storage signing
STORAGE_SIGNING_SECRET=<output of: openssl rand -hex 32>

# Public base URL (used for CDN delivery links)
STORAGE_PUBLIC_BASE_URL=http://localhost:4500
PUBLIC_BASE_URL=http://localhost:4500
```

See [`docs/configuration.md`](configuration.md) for the complete variable reference.

---

## 4 — Start Infrastructure

Use Docker Compose to start Postgres, Redis, and NATS without installing them locally:

```bash
docker compose up postgres redis nats -d
```

Wait for the services to become healthy:

```bash
docker compose ps
# postgres, redis, nats should all show "healthy"
```

Alternatively, start them natively if you have them installed locally; ensure:
- PostgreSQL is listening on `localhost:5432`
- Redis is listening on `localhost:6379` (with the password set in `.env`)
- NATS is listening on `localhost:4222` with JetStream enabled (`--jetstream` or `-js` flag)

---

## 5 — Create the Database

```bash
./scripts/createDB.sh
```

This script connects to PostgreSQL as the configured user and creates the database if it does not exist. It requires the `psql` client to be available in `PATH`.

> If you receive `❌ .env file not found`, ensure step 3 is complete and you are running the script from the project root.

---

## 6 — Run Migrations

```bash
./scripts/migrate.sh up
```

This applies `migrations/000001_initial.up.sql`, which creates all tables, indexes, triggers, and enum types.

> The script requires the `migrate` CLI. Install it with:
> ```bash
> go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
> ```

---

## 7 — Start the Application

### With live reload (recommended for development)

```bash
air
```

Air watches `.go`, `.tpl`, `.tmpl`, and `.html` files and rebuilds on change. Configuration is in [`.air.toml`](../.air.toml).

### Without live reload

```bash
go run ./cmd/server
```

### Expected output

```
{"level":"info","ts":"...","msg":"CDN Network Starting","app":"CDN Network","port":"4500","production":false}
{"level":"info","ts":"...","msg":"Postgres connected","maxPoolSize":25,"minPoolSize":5}
{"level":"info","ts":"...","msg":"NATS connected","url":"nats://localhost:4222"}
{"level":"info","ts":"...","msg":"CDN Network ready","host":"0.0.0.0","port":"4500"}
{"level":"info","ts":"...","msg":"FileCleanupWorker started","subject":"file.delete"}
```

---

## 8 — Create Your First API Key

The application has no built-in admin user. The bootstrap script inserts a key directly into PostgreSQL, bypassing the HTTP API:

```bash
./scripts/createFirstAPIKey.sh
```

The script prints the raw API key — **copy it now, it will not be shown again**.

```
✅  API key created!

  ID     : <uuid>
  PREFIX : ak_XXXXXXXXXX
  KEY    : ak_<46-char-key>

⚠️  Copy the key now — it will NOT be shown again.
```

---

## 9 — Verify the Installation

```bash
export CDN_KEY="ak_<your-key-here>"
export BASE="http://localhost:4500/api/v1"

# Health check (no auth required)
curl -s "$BASE/health" | jq .
# Expected: {"status":"healthy", ...}

# Liveness probe
curl -s "$BASE/health/live" | jq .
# Expected: {"status":"alive", ...}

# List buckets (requires auth)
curl -s -H "Authorization: Bearer $CDN_KEY" "$BASE/buckets" | jq .
# Expected: {"success":true,"data":[]}
```

---

## 10 — Try the Core Workflow

### Create a bucket

```bash
curl -s -X POST "$BASE/buckets" \
  -H "Authorization: Bearer $CDN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-bucket","visibility":"public","maxFileSizeBytes":10485760}' | jq .
```

Save the `data.id` as `BUCKET_ID`:
```bash
export BUCKET_ID="<uuid from response>"
```

### Upload a file directly

```bash
curl -s -X POST "$BASE/buckets/$BUCKET_ID/files/upload" \
  -H "Authorization: Bearer $CDN_KEY" \
  -F "file=@/path/to/your/file.jpg" | jq .
```

Save the `data.objectKey` as `OBJECT_KEY`:
```bash
export OBJECT_KEY="<objectKey from response>"
```

### Retrieve the file via CDN

```bash
curl -s "http://localhost:4500/cdn/$OBJECT_KEY" -o downloaded.jpg
```

---

## Running Tests

```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Run a specific package
go test ./internal/domain/file/...
```

---

## Common Setup Failures

### `NATS connection failed`

The NATS server was not started or JetStream is not enabled. Verify with:
```bash
curl -s http://localhost:8222/healthz
```

If using Docker, ensure the `nats` service is healthy:
```bash
docker compose ps nats
```

### `PostgreSQL connection failed` / `dial tcp: connection refused`

Postgres is not running or the credentials in `.env` are wrong. Verify:
```bash
psql -h localhost -U cdn_user -d cdn_network -c "SELECT 1;"
```

### `Redis connection failed`

Redis is not running or the password is incorrect. Verify:
```bash
redis-cli -a <REDIS_PASSWORD> PING
# Expected: PONG
```

### `migrate: no migration`

`./scripts/migrate.sh` could not find the `migrate` binary. Install it:
```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

### `❌ .env file not found`

Scripts look for `.env` in the project root. Run them from the project root, or run `cp .env.example .env` first.

### Port 4500 already in use

Another process is bound to port 4500. Change `APP_PORT` in `.env` or stop the conflicting process:
```bash
lsof -i :4500
kill <pid>
```
