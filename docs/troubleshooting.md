# Troubleshooting

---

## Application Won't Start

### `PostgreSQL connection failed: dial tcp: connect: connection refused`

PostgreSQL is not running or is not reachable at `POSTGRES_HOST:POSTGRES_PORT`.

**Diagnose:**
```bash
# Docker:
docker compose ps postgres

# Native:
pg_isready -h localhost -p 5432
```

**Resolve:**
```bash
# Start with Docker:
docker compose up postgres -d

# Verify health:
docker compose ps postgres
# Should show: (healthy)
```

---

### `PostgreSQL connection failed: password authentication failed`

The credentials in `.env` do not match what the PostgreSQL server expects.

**Diagnose:**
```bash
psql -h localhost -p 5432 -U $POSTGRES_USER -d $POSTGRES_DB
```

**Resolve:** Check `POSTGRES_USER` and `POSTGRES_PASSWORD` in `.env`.

---

### `Redis connection failed`

Redis is not running or the password is wrong.

**Diagnose:**
```bash
# Docker:
docker compose ps redis

# With password:
redis-cli -h localhost -p 6379 -a "$REDIS_PASSWORD" PING
```

**Resolve:**
```bash
docker compose up redis -d
```

---

### `NATS connection failed`

NATS is not running, not reachable, or JetStream is not enabled.

**Diagnose:**
```bash
# Check NATS is running:
docker compose ps nats

# Check JetStream is available:
curl -s http://localhost:8222/jsz | jq .config.enabled
# Should return: true
```

**Resolve:**
```bash
# Ensure NATS is started with -js flag (already in docker-compose.yml):
docker compose up nats -d
```

---

### `storage init failed`

The `./storage/` directory could not be created.

**Diagnose:** Check filesystem permissions for the working directory.

**Resolve:**
```bash
mkdir -p storage
chmod 755 storage
```

---

### `PostgreSQL credentials are required in production` (fatal at startup)

`APP_ENV=production` is set but `POSTGRES_USER`, `POSTGRES_PASSWORD`, or `POSTGRES_DB` is empty in `.env`.

**Resolve:** Set the missing values in `.env`.

### `Storage credentials are required in production` (fatal at startup)

`APP_ENV=production` is set but `STORAGE_PUBLIC_BASE_URL` or `STORAGE_SIGNING_SECRET` is empty.

**Resolve:** Set both values in `.env`.

---

## Port Conflicts

### `listen tcp :4500: bind: address already in use`

Something else is running on port 4500.

**Diagnose:**
```bash
lsof -i :4500
```

**Resolve:**
```bash
# Change the port in .env:
APP_PORT=5000

# Or kill the conflicting process:
kill <pid>
```

---

## Database Issues

### `Table 'api_keys' not found` (from `createFirstAPIKey.sh`)

Migrations have not been applied.

**Resolve:**
```bash
./scripts/migrate.sh up
```

### `migrate: error: Dirty database version N`

A migration was interrupted and left the database in a dirty state.

**Diagnose:**
```bash
./scripts/migrate.sh version
```

**Resolve:**
```bash
# Force the version to the last successful migration:
./scripts/migrate.sh force <N>
# Then re-apply:
./scripts/migrate.sh up
```

> Only use `force` if you understand the state of the database. It does not roll back any partial changes.

### `migrate: CLI not found`

```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
```

Ensure `$GOPATH/bin` (or `$GOBIN`) is in your `PATH`.

---

## File Upload Issues

### `413 Request Entity Too Large`

The upload exceeds Fiber's global body limit (10 MiB). Use the presigned URL flow for large files:

1. `POST /api/v1/buckets/:bucketId/files/upload-url` — get a presigned PUT URL.
2. `PUT <presignedUrl>` — upload directly.
3. `POST /api/v1/buckets/:bucketId/files/:fileId/complete` — confirm.

### `FILE_TOO_LARGE: file exceeds bucket max size`

The file's declared `sizeBytes` exceeds the bucket's `maxFileSizeBytes`.

**Resolve:** Update the bucket's `maxFileSizeBytes`:
```bash
curl -X PATCH "$BASE/buckets/$BUCKET_ID" \
  -H "Authorization: Bearer $CDN_KEY" \
  -H "Content-Type: application/json" \
  -d '{"maxFileSizeBytes": 104857600}'
```

### `CompleteUpload: object not yet uploaded`

`POST /files/:fileId/complete` was called before the presigned PUT was completed.

**Resolve:** Complete the PUT upload to the `uploadUrl`, then retry `complete`.

---

## CDN Delivery Issues

### `404 CDN domain not found`

The `Host` header does not match any registered CDN domain.

**Common causes:**
- The domain was never registered (`POST /cdn-domains`).
- The domain is registered but not in `active` status (must be verified).
- Running locally: the request host is `localhost` but `CDN_LOCAL_DOMAIN` is not set.

**For local development:**
```dotenv
CDN_LOCAL_DOMAIN=your-registered-domain.example.com
```

This routes `localhost` requests as if they came from `your-registered-domain.example.com`.

### `404 object not found`

The object key does not exist in storage.

**Common causes:**
- The file was deleted or never uploaded.
- The `CompleteUpload` step was skipped.

**Diagnose:**
```bash
ls storage/<storage-bucket-name>/<object-key-path>
```

### Objects appear in stdout as plain text

The application has a `fmt.Println(objectKey)` debug statement in `CDNDeliveryHandler.Serve`. This is a known issue. The output is harmless but indicates debug code that should be removed.

---

## Authentication Issues

### `401 missing Authorization header`

Include the `Authorization` header:
```bash
curl -H "Authorization: Bearer $CDN_KEY" ...
```

### `401 invalid API key format`

The key must start with `ak_`. Keys created by `createFirstAPIKey.sh` have this prefix.

### `401 API key is revoked or expired`

The key was revoked (`DELETE /api-keys/:keyId`) or its `expiresAt` has passed. Create a new key.

---

## Rate Limiting

### `429 rate limit exceeded`

You have exceeded the request rate for your IP. The limits are:

| Endpoint group | Max | Window |
|---|---|---|
| `/api/v1/*` | 10 req | 1 second |
| `/cdn/*` | 25 req | 1 second |

After 5 violations, the IP is blocked for 5 minutes.

**Resolve:** Wait for the penalty window to expire, or reset from the Redis CLI (development only):
```bash
redis-cli -a "$REDIS_PASSWORD" DEL "ratelimit:count:api:<your-ip>"
redis-cli -a "$REDIS_PASSWORD" DEL "ratelimit:viol:api:<your-ip>"
```

---

## Docker Issues

### Services start in wrong order

`depends_on` with `condition: service_healthy` ensures the API waits for Postgres/Redis/NATS. If a service fails its healthcheck, the API will not start. Check:
```bash
docker compose ps
docker compose logs postgres
docker compose logs redis
docker compose logs nats
```

### `api` container exits immediately

```bash
docker compose logs api
```

Common causes: missing env variables (see startup validation above), port conflict, or migration not applied.

### Permission denied writing to `./storage/`

If using a bind-mount volume, ensure the host path is writable by the container user (UID 0 in the Alpine runtime image):
```bash
chmod 755 /var/cdn-network/storage
```

---

## NATS / FileCleanupWorker Issues

### `FileCleanupWorker exited: ...`

The worker stopped unexpectedly. Check:
```bash
docker compose logs api | grep FileCleanupWorker
```

If NATS became unavailable mid-run, restart the API container:
```bash
docker compose restart api
```

The JetStream `file.delete` stream retains messages for 24 hours. Pending deletions will be processed when the worker restarts.

### Files stuck in `deleting` status

The `FileCleanupWorker` did not receive or process the deletion event. This can happen if NATS was unavailable when the delete was requested.

**Diagnose:**
```bash
# Check NATS message count on the stream:
curl -s http://localhost:8222/jsz | jq '.streams[] | select(.config.name == "CDN_FILES") | .state.messages'
```

**Resolve:** Ensure NATS is running and restart the API container to reconnect the worker.

---

## Log Files

Logs in development mode go to stdout. In production (`APP_ENV=production`), they go to `logs/app.log`.

```bash
# Follow application logs:
tail -f logs/app.log | jq .

# Docker:
docker compose logs -f api
```
