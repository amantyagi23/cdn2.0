# Deployment

This document covers running CDN Network in production using the provided Docker Compose configuration and an optional Nginx reverse proxy.

---

## Prerequisites

- Docker ≥ 24 with Compose v2
- A server with sufficient disk space for the `./storage/` volume
- (Optional) A domain name pointed at your server's IP for CDN delivery

---

## Configuration

Copy and fill in the environment file:

```bash
cp .env.example .env
```

Minimum production values:

```dotenv
APP_ENV=production

POSTGRES_USER=cdn_user
POSTGRES_PASSWORD=<strong-password>
POSTGRES_DB=cdn_network

REDIS_PASSWORD=<strong-password>

STORAGE_SIGNING_SECRET=<output of: openssl rand -hex 32>
STORAGE_PUBLIC_BASE_URL=https://cdn.example.com

PUBLIC_BASE_URL=https://cdn.example.com
BACKEND_URL=https://cdn.example.com/api/v1

CORS_ALLOWED_ORIGINS=https://your-frontend.example.com

TRUSTED_PROXIES=172.20.0.0/16
```

`TRUSTED_PROXIES` should be set to the Docker bridge network CIDR so that Nginx can set `X-Forwarded-For` correctly. Check your Docker network with:

```bash
docker network inspect cdn-alpha-network --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}'
```

---

## Database Migration

Before the first start (and after every update that includes new migration files), run migrations manually:

```bash
# Install the migrate CLI if not already installed:
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Apply migrations (connects to POSTGRES_HOST — must be running):
./scripts/migrate.sh up
```

> ⚠️ The `entryPoint.sh` file in this repository is a stub and does **not** run migrations automatically. Do not rely on it.

---

## Docker Compose

### Start all services

```bash
docker compose up --build -d
```

This starts four services:

| Service | Image | Port |
|---|---|---|
| `postgres` | postgres:17-alpine | `POSTGRES_PORT:5432` |
| `redis` | redis:8 | `REDIS_PORT:6379` |
| `nats` | nats:2 | `NATS_PORT:4222`, `NATS_MONITOR_PORT:8222` |
| `api` | built from `Dockerfile` | `4500:4500` |

The `api` service uses `depends_on` with `condition: service_healthy`, so Postgres, Redis, and NATS must pass their healthchecks before the API starts.

### View logs

```bash
docker compose logs -f api
docker compose logs -f postgres
```

### Stop

```bash
docker compose down
```

> ⚠️ `docker compose down -v` removes the named Docker volumes (`cdn_alpha_postgres`, `cdn_alpha_redis`, `cdn_alpha_nats`) and **permanently deletes all stored data**. Do not run this in production.

---

## Persistent Storage

The `./storage/` directory on the host is **not** a Docker volume in the current compose file — it is bind-mounted from the application's working directory inside the container. The `api` service must be able to write to this path.

**For production**, ensure the `storage/` directory is on a persistent volume:

Option A — bind-mount a host directory (edit `docker-compose.yml`):
```yaml
api:
  volumes:
    - /var/cdn-network/storage:/app/storage
```

Option B — use a named Docker volume:
```yaml
api:
  volumes:
    - cdn_storage:/app/storage

volumes:
  cdn_storage:
    name: cdn_storage
```

---

## Dockerfile

The `Dockerfile` uses a two-stage build:

1. **Builder** (`golang:1.25-alpine`) — downloads dependencies and compiles the binary with `CGO_ENABLED=0`.
2. **Runtime** (`alpine:3.22`) — copies only the compiled binary. The image is ~10 MB.

Build manually:

```bash
docker build -t cdn-network:latest .
```

---

## Nginx Reverse Proxy (Optional)

An example Nginx configuration is provided at [`deploy/nginx/nginx.conf`](../deploy/nginx/nginx.conf). It:

- Proxies `/api/*` to the API container.
- Serves a static SPA from `/usr/share/nginx/html` (optional).
- Sets security headers.
- Forwards `X-Forwarded-For` and `X-Forwarded-Host`.

To use it, add a Nginx service to `docker-compose.yml` or run Nginx separately on the host.

### Key Nginx settings for this application

```nginx
proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
proxy_set_header X-Forwarded-Host  $host;
proxy_buffering off;   # required for streaming downloads
client_max_body_size 100M;
```

Ensure `TRUSTED_PROXIES` in `.env` includes the Nginx container's IP range so the application honours these headers.

### TLS

TLS termination must be handled at the proxy layer. The application itself listens on plain HTTP. A minimal Certbot/Let's Encrypt setup:

```bash
certbot --nginx -d cdn.example.com
```

---

## Health Checks

The application exposes three health endpoints (all require authentication):

| Endpoint | Use |
|---|---|
| `GET /api/v1/health` | Full health including dependency status |
| `GET /api/v1/health/live` | Liveness — always 200 if process is running |
| `GET /api/v1/health/ready` | Readiness — 503 if Redis or NATS is down |

Use `/api/v1/health/ready` for orchestration readiness probes.

---

## Graceful Shutdown

On `SIGINT` or `SIGTERM`:

1. The application context is cancelled, stopping the `FileCleanupWorker`.
2. `app.ShutdownWithContext` waits up to 15 seconds for in-flight requests to complete.
3. Deferred `Close()` calls run for PostgreSQL, Redis, and NATS.

Docker sends `SIGTERM` to the container process on `docker compose stop` or `docker compose down`. The 15-second shutdown window is sufficient for most workloads.

---

## Environment-Specific Notes

### Logs

In production (`APP_ENV=production`), logs are written to `logs/app.log` in JSON format. The `logs/` directory is relative to the working directory (`/app` in the Docker image).

To persist logs, mount a volume:
```yaml
api:
  volumes:
    - /var/cdn-network/logs:/app/logs
```

Or stream logs from Docker:
```bash
docker compose logs -f api
```

### Log Level

Set `LOG_LEVEL=info` or `LOG_LEVEL=warn` in production to reduce log volume.

### NATS JetStream Storage

NATS persists JetStream data to `/data` inside the container, backed by the `cdn_alpha_nats` volume. The `CDN_FILES` stream has a 24-hour retention window. Unprocessed deletion events older than 24 hours will be dropped.

---

## First-Time Deployment Checklist

- [ ] `.env` created from `.env.example` with all production values filled in
- [ ] `STORAGE_SIGNING_SECRET` set to a strong random value
- [ ] `CORS_ALLOWED_ORIGINS` set to your frontend origin(s)
- [ ] `TRUSTED_PROXIES` set if running behind a reverse proxy
- [ ] `APP_ENV=production` set
- [ ] Docker volumes created / host paths writable
- [ ] `docker compose up -d` postgres redis nats
- [ ] Migrations run: `./scripts/migrate.sh up`
- [ ] First API key created: `./scripts/createFirstAPIKey.sh`
- [ ] `docker compose up --build -d api`
- [ ] Health check passing: `curl https://cdn.example.com/api/v1/health/ready`
