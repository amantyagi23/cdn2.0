# Changelog

All notable changes to CDN Network will be documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

### Added
- Custom local-filesystem storage client with presigned GET/PUT URL support (HMAC-SHA256 signing)
- Bucket management API (create / list / get / update / soft-delete)
- Public and private bucket visibility with per-bucket `maxFileSizeBytes` enforcement
- Direct multipart file upload (`POST /files/upload`)
- Presigned PUT URL upload flow (`POST /files/upload-url` + `POST /files/:id/complete`)
- Presigned GET URL generation for private files (15-minute TTL)
- CDN delivery endpoint (`GET /cdn/*`) with hostname-based domain resolution
- Custom CDN domain registration and lifecycle management (pending → active → disabled)
- Per-bucket CDN settings (default TTL, max TTL, force-HTTPS flag, compression flag)
- Redis-backed read-through caching for file records, file lists, and CDN domain lookups
- Redis-backed distributed rate limiting (fixed-window + penalty box) for API, delivery, and upload kinds
- NATS JetStream async file-deletion worker (`FileCleanupWorker` on stream `CDN_FILES`)
- API key authentication (Bearer `ak_…`; SHA-256 hash storage, plaintext shown once)
- Usage event recording and summary endpoint
- Structured JSON logging via Zap (stdout in development, file in production)
- Health, liveness, and readiness endpoints
- Graceful shutdown with 15-second drain window
- Docker Compose configuration (Postgres 17, Redis 8, NATS 2, multi-stage build)
- Nginx reverse-proxy example configuration
- `createFirstAPIKey.sh` bootstrap script
- `migrate.sh` migration management script
- `createDB.sh` database creation script

### Known Limitations
- DNS challenge verification not implemented — domain verify endpoint marks domain active without checking DNS
- `internal/security/fraud` package is a stub
- SMTP configuration loaded but not wired to any sending code
- `entryPoint.sh` is a stub (does not run migrations)
- `fmt.Println(objectKey)` debug statement in CDN delivery handler
- Bucket cache is defined but never populated

---

> Previous releases are not documented. This changelog establishes the forward-looking format for future versions.
