# API Reference

All management endpoints are prefixed with the value of `API_PREFIX` (default `/api/v1`). This document uses `/api/v1` throughout.

## Authentication

All management endpoints (everything except CDN delivery and health) require a Bearer API key:

```
Authorization: Bearer ak_<key>
```

The key must have been created via `POST /api/v1/api-keys` or the `createFirstAPIKey.sh` bootstrap script and must not be revoked or expired.

## Response Envelope

Every API response uses the same JSON envelope:

```json
{
  "success": true,
  "message": "Human-readable status",
  "data": { ... }
}
```

Error responses omit `data` and include an `error` object:

```json
{
  "success": false,
  "error": {
    "code": "MACHINE_READABLE_CODE",
    "message": "Human-readable description"
  }
}
```

## Common Error Codes

| HTTP | Code | Meaning |
|---|---|---|
| 400 | `PARSE_ERROR` | Request body could not be parsed as JSON |
| 400 | `VALIDATION_ERROR` | Input failed validator constraints |
| 400 | `INVALID_ID` | URL parameter is not a valid UUID |
| 400 | `DOMAIN_ERROR` | Domain rule violated (see message) |
| 401 | `UNAUTHORIZED` | Missing, invalid, or revoked API key |
| 404 | `NOT_FOUND` | Resource does not exist |
| 409 | `CONFLICT` | Duplicate name or invalid state transition |
| 429 | `RATE_LIMIT_EXCEEDED` | Too many requests |
| 500 | `INTERNAL_ERROR` | Unexpected server error (details not exposed) |

---

## Health

### `GET /api/v1/health`

Returns the overall health of the application and its dependencies. **Requires authentication.**

**Response 200 (healthy)**
```json
{
  "status": "healthy",
  "application": "CDN Network",
  "version": "1.0.0",
  "environment": "http://localhost:4500/api/v1",
  "uptime": "5m30s",
  "uptimeSeconds": 330,
  "goVersion": "go1.26.0",
  "os": "linux",
  "arch": "amd64",
  "numCPU": 8,
  "numGoroutine": 12,
  "dependencies": {
    "redis": { "status": "up" },
    "nats":  { "status": "up" }
  },
  "timestamp": "2024-07-15T10:00:00Z"
}
```

**Response 503 (degraded)** — returned when Redis or NATS is unreachable. Same shape but `"status": "degraded"`.

### `GET /api/v1/health/live`

Liveness probe. Returns 200 immediately if the process is running.

```json
{ "status": "alive", "timestamp": "2024-07-15T10:00:00Z" }
```

### `GET /api/v1/health/ready`

Readiness probe. Returns 200 only when all dependencies are healthy.

**200:** `{ "status": "ready" }`  
**503:** `{ "status": "not_ready" }`

---

## API Keys

### `POST /api/v1/api-keys`

Create a new API key. **Requires authentication.**

**Request body**
```json
{
  "name": "my-service",
  "scopes": [],
  "expiresAt": "2025-12-31T23:59:59Z"
}
```

| Field | Type | Required | Constraints |
|---|---|---|---|
| `name` | string | Yes | 2–120 characters |
| `scopes` | string[] | No | Free-form scope strings |
| `expiresAt` | RFC 3339 timestamp | No | Omit for no expiry |

**Response 201**
```json
{
  "success": true,
  "message": "API Key Created",
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "my-service",
    "keyPrefix": "ak_XXXXXXXXXX",
    "scopes": [],
    "status": "active",
    "expiresAt": null,
    "lastUsedAt": null,
    "createdAt": "2024-07-15T10:00:00Z",
    "rawKey": "ak_<full-46-char-key>"
  }
}
```

> **Important:** `rawKey` is returned **only on creation** and is never stored. Copy it immediately.

---

### `GET /api/v1/api-keys`

List all API keys. **Requires authentication.**

**Response 200**
```json
{
  "success": true,
  "data": [
    {
      "id": "...",
      "name": "my-service",
      "keyPrefix": "ak_XXXXXXXXXX",
      "scopes": [],
      "status": "active",
      "createdAt": "...",
      "lastUsedAt": "..."
    }
  ]
}
```

`rawKey` is never included in list responses.

---

### `DELETE /api/v1/api-keys/:keyId`

Revoke an API key. **Requires authentication.**

**Path parameter:** `keyId` — UUID of the key to revoke.

**Response 200**
```json
{ "success": true, "message": "API Key Revoked" }
```

**Errors:** 404 if key not found, 409 if already revoked.

---

## Buckets

### `POST /api/v1/buckets`

Create a storage bucket. **Requires authentication.**

**Request body**
```json
{
  "name": "my-images",
  "visibility": "public",
  "maxFileSizeBytes": 10485760
}
```

| Field | Type | Required | Constraints |
|---|---|---|---|
| `name` | string | Yes | 3–63 chars, lowercase alphanumeric and hyphens, must start and end with alphanumeric |
| `visibility` | string | Yes | `"public"` or `"private"` |
| `maxFileSizeBytes` | int64 | No | Positive integer; defaults to 52428800 (50 MiB) |

**Side effects:** Creates the underlying storage directory on disk.

**Response 201**
```json
{
  "success": true,
  "message": "Bucket Created",
  "data": {
    "id": "...",
    "name": "my-images",
    "visibility": "public",
    "maxFileSizeBytes": 10485760,
    "createdAt": "...",
    "updatedAt": "..."
  }
}
```

**Errors:** 409 if a bucket with the same name already exists for this API key.

---

### `GET /api/v1/buckets`

List all buckets owned by the authenticated API key. **Requires authentication.**

**Response 200** — array of bucket objects (same shape as create response `data`).

---

### `GET /api/v1/buckets/:bucketId`

Get a bucket by ID. **Requires authentication.**

**Response 200** — single bucket object.  
**Errors:** 404 if not found.

---

### `PATCH /api/v1/buckets/:bucketId`

Update a bucket's visibility or max file size. **Requires authentication.**

**Request body** — all fields optional:
```json
{
  "visibility": "private",
  "maxFileSizeBytes": 52428800
}
```

**Side effects:** If `visibility` changes, the storage directory's read policy is updated accordingly.

**Response 200** — updated bucket object.

---

### `DELETE /api/v1/buckets/:bucketId`

Soft-delete a bucket (sets `deleted_at`, appends `__deleted__<timestamp>` to the name to free the unique slot). **Requires authentication.**

> **Note:** Deleting a bucket does not delete the files it contains. Files in a deleted bucket remain in storage until individually deleted.

**Response 200**
```json
{ "success": true, "message": "Bucket Deleted" }
```

---

## Files

All file endpoints are scoped to a bucket: `/api/v1/buckets/:bucketId/files/…`.

### `POST /api/v1/buckets/:bucketId/files`

Register a file metadata record without uploading binary content. Use this as step 1 of the presigned-URL upload flow.

**Request body**
```json
{
  "originalFilename": "photo.jpg",
  "contentType": "image/jpeg",
  "sizeBytes": 204800,
  "checksumSha256": "abc123...64hexchars",
  "cacheControl": "public, max-age=86400"
}
```

| Field | Type | Required | Constraints |
|---|---|---|---|
| `originalFilename` | string | Yes | 1–512 characters |
| `contentType` | string | Yes | ≤ 255 characters |
| `sizeBytes` | int64 | No | ≥ 0. If > bucket `maxFileSizeBytes`, rejected |
| `checksumSha256` | string | No | Exactly 64 hex characters |
| `cacheControl` | string | No | ≤ 512 characters |

**Response 201** — file metadata record with `status: "active"`. No binary content is stored yet.

---

### `POST /api/v1/buckets/:bucketId/files/upload`

Direct multipart upload — registers metadata and stores binary content in a single request. **Requires authentication.**

**Request:** `multipart/form-data`

| Field | Type | Required | Notes |
|---|---|---|---|
| `file` | file part | Yes | Binary content |
| `contentType` | string | No | Overrides the multipart `Content-Type` header |
| `cacheControl` | string | No | Cache-Control value stored with the object |
| `checksumSha256` | string | No | 64-char hex SHA-256 of the file content |

**Response 201** — full file metadata including `etag` populated from the stored object.

---

### `POST /api/v1/buckets/:bucketId/files/upload-url`

Request a presigned PUT URL for client-direct upload. Same request body as `POST /files`.

**Response 201**
```json
{
  "success": true,
  "message": "Upload URL generated",
  "data": {
    "file": { "id": "...", "objectKey": "2024/07/uuid-photo.jpg", "status": "active", ... },
    "uploadUrl": "http://localhost:4500/storage/<bucket>/<key>?X-Signature=...&X-Expires=...",
    "objectKey": "2024/07/uuid-photo.jpg",
    "expiresInSeconds": 900
  }
}
```

The client must `PUT` the binary content to `uploadUrl` within 900 seconds. After the PUT succeeds, call `POST /files/:fileId/complete` to record the ETag.

---

### `POST /api/v1/buckets/:bucketId/files/:fileId/complete`

Complete a presigned-URL upload. Confirms the object exists in storage, reads its ETag, and updates the metadata record. **Requires authentication.**

**Response 200** — updated file metadata with `etag` populated.

**Errors:** Returns an error if the object has not yet been uploaded to storage.

---

### `GET /api/v1/buckets/:bucketId/files`

List files in a bucket. **Requires authentication.**

**Query parameters:**

| Parameter | Default | Description |
|---|---|---|
| `limit` | `50` | Max results (1–200) |
| `offset` | `0` | Pagination offset |

**Response 200** — array of file metadata objects.

---

### `GET /api/v1/buckets/:bucketId/files/:fileId`

Get file metadata by ID. **Requires authentication.**

**Response 200** — single file metadata object.

---

### `GET /api/v1/buckets/:bucketId/files/:fileId/url`

Get a URL to view or download the file. **Requires authentication.**

- **Public bucket** → permanent URL: `<PUBLIC_BASE_URL>/cdn/<objectKey>`. No expiry.
- **Private bucket** → presigned GET URL valid for 15 minutes.

**Response 200**
```json
{
  "success": true,
  "data": {
    "url": "https://cdn.example.com/cdn/2024/07/uuid-photo.jpg",
    "expiresInSeconds": null
  }
}
```

For private files, `expiresInSeconds` is `900` (15 minutes).

---

### `DELETE /api/v1/buckets/:bucketId/files/:fileId`

Mark a file for deletion and publish a `file.delete` event to NATS JetStream. **Requires authentication.**

The file's `status` changes to `"deleting"` immediately. The `FileCleanupWorker` goroutine then asynchronously removes the object from storage and marks the record `"deleted"`.

**Response 200**
```json
{ "success": true, "message": "File marked for deletion" }
```

---

## CDN Domains

### `POST /api/v1/buckets/:bucketId/cdn-domains`

Register a custom hostname as a CDN domain for a bucket. **Requires authentication.**

**Request body**
```json
{
  "hostname": "cdn.example.com",
  "tlsEnabled": true
}
```

| Field | Type | Required | Constraints |
|---|---|---|---|
| `hostname` | string | Yes | 4–253 chars, valid RFC 1123 hostname (must contain at least one dot) |
| `tlsEnabled` | bool | No | Defaults to `false` |

**Response 201** — domain record. The `verificationToken` is included **only in the creation response**.

```json
{
  "success": true,
  "message": "CDN Domain Created",
  "data": {
    "id": "...",
    "bucketId": "...",
    "hostname": "cdn.example.com",
    "status": "pending",
    "tlsEnabled": true,
    "verificationToken": "<random-base64url-token>",
    "createdAt": "...",
    "updatedAt": "..."
  }
}
```

**Errors:** 409 if the hostname is already registered.

> ⚠️ **Limitation:** DNS challenge verification is not implemented. `POST /verify` marks the domain active without checking DNS records.

---

### `GET /api/v1/buckets/:bucketId/cdn-domains`

List CDN domains for a bucket. **Requires authentication.**

**Response 200** — array of domain objects (without `verificationToken`).

---

### `POST /api/v1/buckets/:bucketId/cdn-domains/:domainId/verify`

Mark a pending domain as active. **Requires authentication.**

> **Known limitation:** No actual DNS verification is performed. The domain is unconditionally transitioned from `pending` to `active`.

**Errors:** 409 if the domain is not in `pending` status.

---

### `POST /api/v1/buckets/:bucketId/cdn-domains/:domainId/enable`

Re-enable a disabled domain (transitions `disabled` → `active`). **Requires authentication.**

**Errors:** 409 if domain is not in `disabled` status.

---

### `POST /api/v1/buckets/:bucketId/cdn-domains/:domainId/disable`

Disable an active domain (transitions `active` → `disabled`). **Requires authentication.**

**Errors:** 409 if domain is not in `active` status.

---

### `DELETE /api/v1/buckets/:bucketId/cdn-domains/:domainId`

Delete a CDN domain registration. **Requires authentication.**

**Response 200:** `{ "success": true, "message": "Domain Deleted" }`

---

## CDN Settings

### `GET /api/v1/buckets/:bucketId/cdn-settings`

Get CDN settings for a bucket. **Requires authentication.**

If no settings record exists yet, returns the defaults.

**Response 200**
```json
{
  "success": true,
  "data": {
    "bucketId": "...",
    "defaultTtlSeconds": 3600,
    "maxTtlSeconds": 86400,
    "forceHttps": true,
    "compressionEnabled": true,
    "updatedAt": "..."
  }
}
```

---

### `PUT /api/v1/buckets/:bucketId/cdn-settings`

Create or update CDN settings for a bucket. **Requires authentication.**

**Request body**
```json
{
  "defaultTtlSeconds": 3600,
  "maxTtlSeconds": 86400,
  "forceHttps": true,
  "compressionEnabled": true
}
```

| Field | Type | Required | Constraints |
|---|---|---|---|
| `defaultTtlSeconds` | int32 | No | ≥ 0 |
| `maxTtlSeconds` | int32 | No | ≥ `defaultTtlSeconds` |
| `forceHttps` | bool | No | — |
| `compressionEnabled` | bool | No | — |

> **Note:** `forceHttps` and `compressionEnabled` are stored but not enforced by the delivery handler. TLS termination must be configured at the reverse proxy level.

**Response 200** — updated settings object.

---

## Usage Events

### `POST /api/v1/usage-events`

Record a usage event. **Requires authentication.**

**Request body**
```json
{
  "bucketId": "...",
  "eventType": "upload",
  "quantity": 204800,
  "metadata": { "filename": "photo.jpg" }
}
```

| Field | Type | Required | Constraints |
|---|---|---|---|
| `eventType` | string | Yes | Free-form label (e.g. `upload`, `download`, `delete`) |
| `quantity` | int64 | No | ≥ 0 |
| `bucketId` | UUID | No | Associated bucket |
| `metadata` | JSON object | No | Arbitrary key-value metadata |

**Response 201:** `{ "success": true, "message": "Usage Event Recorded" }`

---

### `GET /api/v1/usage-events`

List usage events for the authenticated API key. **Requires authentication.**

**Query parameters:**

| Parameter | Default | Description |
|---|---|---|
| `limit` | `100` | Max results |
| `offset` | `0` | Pagination offset |

---

### `GET /api/v1/usage-events/summary`

Summarise usage events within a time range. **Requires authentication.**

**Query parameters:**

| Parameter | Format | Description |
|---|---|---|
| `since` | RFC 3339 | Start of time range (inclusive) |
| `until` | RFC 3339 | End of time range (inclusive) |

If `since` or `until` cannot be parsed, the parameter is silently ignored.

---

## CDN Delivery

### `GET /cdn/<object-key>`

Deliver a file from storage. **No authentication required.**

The `Host` header (or `X-Forwarded-Host` from a trusted proxy) is used to look up the registered CDN domain and resolve the associated bucket. The object at `<object-key>` is then streamed from storage.

**Response headers:**
- `Content-Type` — from stored object metadata
- `ETag` — from stored object metadata
- `Cache-Control` — from stored object metadata, or `public, max-age=3600` if not set
- `X-CDN-Origin: storage`

**Rate limit:** 25 requests/second per IP.

**Response 200** — raw binary content.  
**Response 404** — CDN domain not registered, domain not in `active` status, or object not found.

**Example:**
```bash
curl https://cdn.example.com/cdn/2024/07/uuid-photo.jpg
```

> **Note:** Only `active` CDN domains serve files. Domains in `pending`, `disabled`, or `failed` status return 404.
