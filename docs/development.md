# Development Guide

This document describes the code organisation, conventions, and workflows used in this project.

---

## Code Organisation

```
internal/           Application code (not importable by external packages)
  config/           Single-pass env loading; no side effects
  domain/           Pure domain logic — no DB, no HTTP, no infrastructure imports
  handlers/         HTTP layer — parse input → call use case → format response
  health/           Health handler and service
  infrastructure/   External-service clients (DB, Redis, NATS, storage, logger)
  middleware/       Fiber middleware (auth, rate limiting)
  repos/            SQL queries; thin wrappers around pgx pool
  routes/           Dependency injection wiring, route registration
  security/         Rate limiting, fraud detection (stub)
  usecase/          Business logic; calls repos, storage, cache
  utils/            Shared validation helpers
  workers/          Background goroutines consuming NATS subjects
pkg/                Reusable packages with no internal imports
  cache/            Typed Redis helpers
  crypto/           Cryptographic utilities
  errors/           Shared sentinel errors
  response/         Standardised JSON envelope
cmd/server/         Application entry point only — no logic
migrations/         golang-migrate SQL files
scripts/            Shell utility scripts
deploy/             Deployment artifacts (Nginx config)
```

### Dependency Rules

- `internal/domain` packages must not import from `internal/infrastructure`, `internal/repos`, or `internal/usecase`.
- `internal/usecase` packages must not import from `internal/handlers`.
- `internal/handlers` packages must not import from `internal/repos` directly — go through use cases.
- `pkg/` packages must not import from `internal/`.

---

## Naming Conventions

### Files

- One domain concept per file: `bucket.go` defines the `Bucket` aggregate; `bucket_test.go` tests it.
- Handler files: `<entity>_handler.go`.
- Use case interfaces and implementations: `<entity>_usecase.go`.
- Repository files: `<entity>_repository.go`.

### Packages

- Package names are singular nouns: `bucket`, `file`, `cdn`, `apikey`.
- Infrastructure packages use their service name: `redis`, `nats`, `storage`, `database`.

### Interfaces

- Use case interfaces are defined in the same file as the implementation, e.g. `FileUsecase` interface in `file_usecase.go`.
- Repository types are concrete structs — there are no repository interfaces currently.

---

## Error Handling Conventions

1. **Domain errors** are sentinel `errors.New(…)` values defined in each domain package (e.g. `bucket.ErrNotFound`).
2. **Use cases** return domain errors directly when the cause is a domain rule violation, or wrap infrastructure errors with `fmt.Errorf("operationName: %w", err)`.
3. **Handlers** use `errors.Is` to map domain errors to HTTP status codes via `pkg/response` helpers.
4. **Infrastructure errors** should not leak into use cases. Infrastructure packages define their own sentinels (e.g. `storage.ErrNotFound`).

```go
// Domain sentinel:
var ErrNotFound = errors.New("bucket not found")

// Use case wrapping:
return nil, fmt.Errorf("CreateBucket: provision storage bucket: %w", err)

// Handler mapping:
case errors.Is(err, bucket.ErrNotFound):
    return response.NotFound(c, "bucket not found")
```

---

## Adding a New Endpoint

1. **Define domain model** in `internal/domain/<entity>/`. Add sentinel errors.
2. **Write use case interface and implementation** in `internal/usecase/<entity>_usecase.go`. Follow existing patterns.
3. **Write repository** in `internal/repos/<entity>_repository.go`. Use pgx named queries.
4. **Write handler** in `internal/handlers/<entity>_handler.go`. Call `RegisterRoutes(router)` to attach routes.
5. **Register in `routes.Register`** (`internal/routes/route.go`): instantiate repo, use case, handler.
6. **Add migration** if new tables are needed (see [Migrations](#migrations)).
7. **Write tests** for domain model and use case.
8. **Document** the endpoint in [`docs/api-reference.md`](api-reference.md).

---

## Adding a Service or Storage Operation

New storage operations should be added to `internal/infrastructure/storage/storage.go` as methods on `StorageClient`. Follow the existing pattern:
- Accept a `context.Context` as the first parameter.
- Return typed errors using the storage sentinel values (`ErrNotFound`, `ErrAlreadyExists`, etc.).

---

## Adding Configuration

1. Add a field to `internal/config/Config`.
2. Read the env variable in `config.Load()` using `getEnv(key, defaultValue)`.
3. Add the variable to `.env.example` with a comment explaining its purpose.
4. Document it in [`docs/configuration.md`](configuration.md).
5. If the variable is required in production, add a check to the production startup validation block in `main.go`.

---

## Adding Middleware

Fiber middleware is registered in `cmd/server/main.go` (global) or in `routes.Register` (group-specific). Follow the existing pattern:
- Global middleware (security headers, CORS, logging) goes in `main.go`.
- Route-group middleware (authentication, rate limiting) goes in `routes.Register`.

---

## Migrations

Migrations use [golang-migrate](https://github.com/golang-migrate/migrate) with PostgreSQL.

### File naming

```
migrations/
  000001_initial.up.sql
  000001_initial.down.sql
  000002_<description>.up.sql
  000002_<description>.down.sql
```

Always provide both `up` and `down` files.

### Running migrations

```bash
# Apply all pending:
./scripts/migrate.sh up

# Roll back one:
./scripts/migrate.sh down

# Check current version:
./scripts/migrate.sh version
```

### Writing migrations

- Wrap migration SQL in `BEGIN; ... COMMIT;`.
- Use `CREATE INDEX CONCURRENTLY` for large tables to avoid locking (note: cannot be used inside a transaction).
- Add `IF NOT EXISTS` where idempotency matters.

---

## Testing Strategy

Tests are co-located with the code they test (`_test.go` files in the same package).

Existing test coverage:
- `internal/domain/apikey/apikey_test.go`
- `internal/domain/bucket/bucket_test.go`
- `internal/domain/file/file_test.go`
- `internal/handlers/cdn_delivery_handler_test.go`
- `internal/handlers/cdn_delivery_proxy_test.go`
- `internal/infrastructure/storage/storage_security_test.go`
- `internal/usecase/file_usecase_security_test.go`

```bash
# Run all tests:
go test ./...

# Run with race detector (recommended before PRs):
go test -race ./...

# Run a specific package:
go test ./internal/domain/bucket/...

# Verbose output:
go test -v ./...
```

### Test conventions

- Domain tests do not require any infrastructure (no DB, no Redis, no NATS).
- Use case tests may use the `InMemoryStore` rate limiter and a mock or real storage client.
- Integration tests requiring PostgreSQL or Redis should be skipped with `t.Skip` when the service is unavailable, or use build tags.

---

## Running Local Checks

```bash
# Format:
gofmt -l .

# Vet:
go vet ./...

# Build:
go build ./...

# Tests with race detector:
go test -race ./...
```

There is no `Makefile` or CI configuration in this repository. Run these checks manually before opening a pull request.

---

## Live Reload

[Air](https://github.com/air-verse/air) is configured in `.air.toml`:

```bash
air
```

Air rebuilds and restarts the binary when `.go`, `.tpl`, `.tmpl`, or `.html` files change. The build output binary is written to `./tmp/main`. Build errors are logged to `./scripts/tmp/build-errors.log`.

---

## Dependency Management

```bash
# Add a dependency:
go get github.com/some/package

# Tidy:
go mod tidy

# Verify checksums:
go mod verify
```

Do not vendor dependencies (`/vendor/` is gitignored).
