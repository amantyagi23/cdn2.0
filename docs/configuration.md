# Configuration Reference

All configuration is loaded from environment variables by [`internal/config/config.go`](../internal/config/config.go). When running locally, variables are read from a `.env` file in the project root via `godotenv`. In Docker, the `.env` file is injected via `env_file` in `docker-compose.yml`.

Copy `.env.example` to `.env` and fill in the required values before starting the application.

---

## Application

| Variable | Required | Default | Description |
|---|---|---|---|
| `APP_ENV` | No | `development` | Set to `production` to enable startup credential validation and file-only logging |
| `APP_NAME` | No | `API Platform` | Application name included in log entries |
| `APP_HOST` | No | `0.0.0.0` | TCP address to bind |
| `APP_PORT` | No | `4500` | TCP port to listen on |
| `API_PREFIX` | No | `/api/v1` | URL prefix for all management API routes |
| `LOG_LEVEL` | No | `debug` | Log level: `debug`, `info`, `warn`, `error` |

---

## Authentication

| Variable | Required | Default | Description |
|---|---|---|---|
| `JWT_SECRET` | No | — | Loaded but not currently used for JWT issuance. Reserved for future use |
| `TOKEN_HEADER` | No | `Authorization` | Header name where Bearer tokens are expected |

---

## CORS

| Variable | Required | Default | Description |
|---|---|---|---|
| `CORS_ALLOWED_ORIGINS` | No (Yes in prod) | `http://localhost:5173,http://localhost:3000,http://localhost:4500` (dev only) | Comma-separated list of allowed browser origins. In production, an empty value causes a warning and allows no cross-origin requests |

`AllowCredentials` is set to `true` automatically when `CORS_ALLOWED_ORIGINS` is a non-empty, non-wildcard list.

---

## Trusted Proxies

| Variable | Required | Default | Description |
|---|---|---|---|
| `TRUSTED_PROXIES` | No | — | Comma-separated IP addresses or CIDR ranges of reverse proxies that are allowed to set `X-Forwarded-For` and `X-Forwarded-Host`. When empty, Fiber trusts the direct peer address only |

**Important:** Leave this empty unless a reverse proxy (e.g. Nginx) sits in front of the application. Setting it incorrectly allows clients to spoof their IP address.

---

## PostgreSQL

| Variable | Required | Default | Description |
|---|---|---|---|
| `POSTGRES_HOST` | No | `localhost` | PostgreSQL hostname |
| `POSTGRES_PORT` | No | `5432` | PostgreSQL port |
| `POSTGRES_USER` | Yes (prod) | — | PostgreSQL username |
| `POSTGRES_PASSWORD` | Yes (prod) | — | PostgreSQL password |
| `POSTGRES_DB` | Yes (prod) | — | PostgreSQL database name |

The connection URL is assembled as `postgresql://<user>:<password>@<host>:<port>/<db>`.

Connection pool defaults (not configurable via env):
- Max pool size: 25
- Min pool size: 5
- Connect timeout: 10 s

---

## Redis

| Variable | Required | Default | Description |
|---|---|---|---|
| `REDIS_HOST` | No | `localhost` | Redis hostname |
| `REDIS_PORT` | No | `6379` | Redis port |
| `REDIS_PASSWORD` | No | — | Redis password (leave empty if Redis has no password) |
| `REDIS_DB` | No | `0` | Redis logical database index |

Redis is used for:
- Read-through caching of file, bucket, and domain records
- Rate-limiter counter and violation tracking

If Redis is unavailable at startup, the application will exit.

---

## NATS

| Variable | Required | Default | Description |
|---|---|---|---|
| `NATS_URL` | No | `nats://nats:4222` | NATS connection URL |
| `NATS_PORT` | No | `4222` | Used only by `docker-compose.yml` to map the host port |
| `NATS_MONITOR_PORT` | No | `8222` | NATS monitoring HTTP port (Docker port mapping only) |

NATS JetStream must be enabled on the NATS server (`-js` flag). The file-cleanup worker creates a JetStream stream named `CDN_FILES` on subject `file.delete`.

If NATS is unavailable at startup, the application will exit.

---

## Storage

| Variable | Required | Default | Description |
|---|---|---|---|
| `STORAGE_SIGNING_SECRET` | Yes (prod) | — | HMAC-SHA256 secret for signing presigned GET and PUT URLs. Generate with `openssl rand -hex 32` |
| `STORAGE_PUBLIC_BASE_URL` | Yes (prod) | — | Public base URL of the service as seen by external clients. Used to build presigned URL prefixes. Example: `https://cdn.example.com` |

Files are stored in `./storage/` relative to the working directory. The directory is created automatically.

---

## Public URLs

| Variable | Required | Default | Description |
|---|---|---|---|
| `PUBLIC_BASE_URL` | No | `http://localhost:4500` | Used to construct permanent CDN delivery URLs: `<PUBLIC_BASE_URL>/cdn/<objectKey>`. Should be the public-facing URL of this service |
| `BACKEND_URL` | No | `http://localhost:4500/api/v1` | Returned in health-check response metadata |

---

## CDN Delivery

| Variable | Required | Default | Description |
|---|---|---|---|
| `CDN_LOCAL_DOMAIN` | No | — | When set, delivery requests arriving from `localhost` or a bare IP address are looked up as if they came from this hostname. Useful for local development smoke testing |

---

## SMTP

These variables are loaded by the config but **SMTP sending is not currently implemented** in the application code.

| Variable | Required | Default | Description |
|---|---|---|---|
| `SMTP_HOST` | No | — | SMTP server hostname |
| `SMTP_PORT` | No | `587` | SMTP port |
| `SMTP_USERNAME` | No | — | SMTP authentication username |
| `SMTP_PASSWORD` | No | — | SMTP authentication password |
| `SMTP_FROM` | No | — | Sender address |

---

## Production Startup Validation

When `APP_ENV=production`, the application calls `log.Fatal` at startup if any of the following are empty:

- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_DB`
- `STORAGE_PUBLIC_BASE_URL`
- `STORAGE_SIGNING_SECRET`

Development mode skips this check so the server starts with partial configuration.

---

## Logging Behaviour

| Mode | Output | Format |
|---|---|---|
| Development (`APP_ENV != production`) | stdout | JSON (Zap production encoder) |
| Production (`APP_ENV=production`) | `logs/app.log` | JSON |

The `logs/` directory is created automatically. Log entries include `ts`, `level`, `msg`, `caller`, and a `service` field set to `APP_NAME`.
