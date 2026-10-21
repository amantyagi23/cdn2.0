# Caching

CDN Network uses Redis as a read-through cache for three categories of data: file records, CDN domain lookups, and file list results. Caching is implemented in the `pkg/cache` package and called from use cases.

---

## What Is Cached

| Entity | Cache key pattern | TTL | Use case |
|---|---|---|---|
| Single file record | `file:<uuid>` | 5 min | `GetFile`, `UploadFile` |
| File list by bucket | `filelist:<bucketId>:<limit>:<offset>` | 5 min | `ListFiles` |
| CDN domain by hostname | `domain:<hostname>` | 15 min | `CDNDeliveryUsecase.Resolve` |

Bucket records are **not** currently cached. The `BucketKey` helper exists in `pkg/cache` but is not called from any use case.

---

## Cache Key Construction

All keys are constructed in one place: `pkg/cache/cache.go`.

```go
FileKey(fileID)                      → "file:<fileID>"
FileListKey(bucketID, limit, offset) → "filelist:<bucketID>:<limit>:<offset>"
DomainKey(hostname)                  → "domain:<hostname>"
BucketKey(bucketID)                  → "bucket:<bucketID>"  // defined but unused
```

---

## TTL and Eviction Policy

TTLs are set at write time using Redis `PEXPIRE`. Redis handles expiry passively (on access) or actively (background sampling). There is no explicit LRU or capacity limit configured — Redis uses its default `noeviction` policy unless you configure `maxmemory-policy` separately.

| TTL constant | Value | Used for |
|---|---|---|
| `cache.TTLFile` | 5 minutes | File records and file list pages |
| `cache.TTLDomain` | 15 minutes | CDN domain hostname lookups |
| `cache.TTLBucket` | 10 minutes | Defined, currently unused |

---

## Cache Hit and Miss Behaviour

The `cache.Get[T]` helper returns `(T, ErrMiss)` on a miss. Use cases follow a consistent read-through pattern:

```go
if cached, err := cache.Get[file.File](ctx, uc.cache, cacheKey); err == nil {
    return &cached, nil  // cache hit
}
// cache miss — query PostgreSQL
f, err := uc.fileRepo.GetByID(ctx, fileID)
// ...
_ = cache.Set(ctx, uc.cache, cacheKey, *f, cache.TTLFile)
return f, nil
```

A cache error other than `ErrMiss` is treated identically to a miss — the request always falls through to the database. Cache failures are never fatal to the request.

---

## Cache Invalidation

Invalidation is performed by `cache.Invalidate`, which calls `Redis.Delete`. Errors are silently swallowed since cache invalidation is best-effort and must not fail business logic.

| Trigger | Key(s) invalidated |
|---|---|
| `MarkFileDeleted` | `file:<fileID>` |
| `CompleteUpload` | `file:<fileID>` |

**Not invalidated:**
- Bucket visibility changes do not invalidate `bucket:<id>` (key is unused anyway).
- CDN domain status changes do not invalidate `domain:<hostname>`. Stale domain entries expire after 15 minutes. A domain marked `disabled` or `pending` will continue to serve files for up to 15 minutes after the status change.
- File list pages (`filelist:…`) are never explicitly invalidated. After an upload or deletion, the list cache will serve stale results until the 5-minute TTL expires.

---

## Rate Limiting: Redis Usage

The rate limiter (`internal/security/ratelimit`) uses a separate Redis key namespace:

| Key pattern | Purpose |
|---|---|
| `ratelimit:count:<kind>:<ip>` | Fixed-window request counter (expires after window) |
| `ratelimit:viol:<kind>:<ip>` | Violation counter (expires after penalty window) |
| `ratelimit:viol:<kind>:<ip>:last` | Timestamp of last violation |

The `checkScript` Lua script atomically increments the counter and, on the first increment of a window, sets the TTL. Violations are tracked separately with their own TTL. Reads for rate limiting bypass the `pkg/cache` layer and use the raw `*redis.Client` directly.

---

## Behaviour During Cache Outages

Redis is a **required** dependency — the application will not start if Redis is unavailable. During normal operation:

- If a `cache.Get` call returns an error (other than `ErrMiss`), the use case proceeds without cache and queries PostgreSQL directly.
- If a `cache.Set` call fails, a warning is logged and the operation continues.
- Rate-limit checks that fail (Redis errors) **fail open** — the request is allowed rather than rejected. This prevents a Redis outage from taking down the API.

---

## Consistency Guarantees

Caching is **best-effort** with no strong consistency guarantees:

- File list pages may be stale for up to 5 minutes after a new upload.
- CDN domain status changes may take up to 15 minutes to be reflected in delivery.
- File records may be stale for up to 5 minutes unless explicitly invalidated.
- There is no distributed lock or cache-aside pattern to prevent cache stampedes. Under high concurrency, multiple requests for the same key may all miss the cache simultaneously and all query PostgreSQL. This is acceptable for the current scale.

---

## Cache Stampede Protection

No stampede protection (e.g. `SetNX` with a lock, probabilistic early expiry) is implemented. For the current use cases (metadata reads), the impact of simultaneous cache misses is low — each request makes at most one additional PostgreSQL query.

---

## Memory Considerations

File list keys are generated per `(bucketId, limit, offset)` tuple. A large number of distinct pagination combinations will create many cache keys. With a 5-minute TTL and Redis's built-in passive expiry, stale keys are reclaimed over time. No explicit bounds are set on the number of cache keys.

---

## Private Content Isolation

Private files are served via presigned URLs that include an HMAC signature. These URLs are not cached by Redis. The CDN delivery path only serves content from **public** buckets or objects reached via valid presigned URLs; there is no risk of serving private content through a cached domain lookup.
