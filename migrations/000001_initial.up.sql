BEGIN;

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ============================================================
-- ENUMS
-- ============================================================

CREATE TYPE bucket_visibility AS ENUM (
    'private',
    'public'
);

CREATE TYPE file_status AS ENUM (
    'active',
    'deleting',
    'deleted'
);

CREATE TYPE cdn_domain_status AS ENUM (
    'pending',
    'active',
    'disabled',
    'failed'
);

-- ============================================================
-- updated_at TRIGGER FUNCTION
-- ============================================================

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ============================================================
-- API KEYS  (no user / org / project ownership)
-- ============================================================

CREATE TABLE api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(120) NOT NULL,
    key_prefix  VARCHAR(20)  NOT NULL,
    key_hash    VARCHAR(64)  NOT NULL UNIQUE,
    scopes      TEXT[]       NOT NULL DEFAULT '{}',
    status      VARCHAR(20)  NOT NULL DEFAULT 'active',
    expires_at  TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    revoked_at  TIMESTAMPTZ
);

CREATE INDEX api_keys_status_idx ON api_keys (status);

-- ============================================================
-- STORAGE BUCKETS  (owned by an API key)
-- ============================================================

CREATE TABLE storage_buckets (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    api_key_id          UUID NOT NULL REFERENCES api_keys (id) ON DELETE CASCADE,
    name                VARCHAR(63) NOT NULL,
    minio_bucket_name   VARCHAR(63) NOT NULL UNIQUE,
    visibility          bucket_visibility NOT NULL DEFAULT 'private',
    max_file_size_bytes BIGINT NOT NULL DEFAULT 52428800
        CHECK (max_file_size_bytes > 0),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ,

    UNIQUE (api_key_id, name)
);

CREATE TRIGGER storage_buckets_set_updated_at
    BEFORE UPDATE ON storage_buckets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ============================================================
-- FILES  (metadata only; binary contents live in MinIO)
-- ============================================================

CREATE TABLE files (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket_id         UUID NOT NULL
        REFERENCES storage_buckets (id) ON DELETE RESTRICT,
    object_key        TEXT NOT NULL,
    original_filename TEXT NOT NULL,
    content_type      VARCHAR(255) NOT NULL,
    size_bytes        BIGINT NOT NULL CHECK (size_bytes >= 0),
    checksum_sha256   VARCHAR(64),
    etag              TEXT,
    cache_control     VARCHAR(512),
    status            file_status NOT NULL DEFAULT 'active',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at        TIMESTAMPTZ,

    UNIQUE (bucket_id, object_key)
);

CREATE INDEX files_bucket_created_idx
    ON files (bucket_id, created_at DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX files_status_idx
    ON files (status)
    WHERE deleted_at IS NULL;

CREATE TRIGGER files_set_updated_at
    BEFORE UPDATE ON files
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ============================================================
-- CDN DOMAINS
-- ============================================================

CREATE TABLE cdn_domains (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bucket_id          UUID NOT NULL
        REFERENCES storage_buckets (id) ON DELETE CASCADE,
    hostname           VARCHAR(253) NOT NULL,
    status             cdn_domain_status NOT NULL DEFAULT 'pending',
    verification_token TEXT NOT NULL,
    verified_at        TIMESTAMPTZ,
    tls_enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX cdn_domains_bucket_idx ON cdn_domains (bucket_id);
CREATE INDEX cdn_domains_status_idx ON cdn_domains (status);

CREATE TRIGGER cdn_domains_set_updated_at
    BEFORE UPDATE ON cdn_domains
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ============================================================
-- CDN SETTINGS  (default behaviour per bucket)
-- ============================================================

CREATE TABLE cdn_settings (
    bucket_id           UUID PRIMARY KEY
        REFERENCES storage_buckets (id) ON DELETE CASCADE,
    default_ttl_seconds INTEGER NOT NULL DEFAULT 3600
        CHECK (default_ttl_seconds >= 0),
    max_ttl_seconds     INTEGER NOT NULL DEFAULT 86400
        CHECK (max_ttl_seconds >= default_ttl_seconds),
    force_https         BOOLEAN NOT NULL DEFAULT TRUE,
    compression_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER cdn_settings_set_updated_at
    BEFORE UPDATE ON cdn_settings
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- ============================================================
-- USAGE EVENTS  (metering)
-- ============================================================

CREATE TABLE usage_events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    api_key_id UUID REFERENCES api_keys (id) ON DELETE SET NULL,
    bucket_id  UUID REFERENCES storage_buckets (id) ON DELETE SET NULL,
    file_id    UUID REFERENCES files (id) ON DELETE SET NULL,
    event_type VARCHAR(40) NOT NULL,
    quantity   BIGINT NOT NULL CHECK (quantity >= 0),
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata   JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX usage_events_occurred_at_idx
    ON usage_events (occurred_at DESC);

CREATE INDEX usage_events_type_occurred_idx
    ON usage_events (event_type, occurred_at DESC);

CREATE INDEX usage_events_apikey_occurred_idx
    ON usage_events (api_key_id, occurred_at DESC)
    WHERE api_key_id IS NOT NULL;

COMMIT;
