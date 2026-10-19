BEGIN;

DROP TABLE IF EXISTS usage_events;
DROP TABLE IF EXISTS cdn_settings;
DROP TABLE IF EXISTS cdn_domains;
DROP TABLE IF EXISTS files;
DROP TABLE IF EXISTS storage_buckets;
DROP TABLE IF EXISTS api_keys;
DROP FUNCTION IF EXISTS set_updated_at;
DROP TYPE IF EXISTS cdn_domain_status;
DROP TYPE IF EXISTS file_status;
DROP TYPE IF EXISTS bucket_visibility;

COMMIT;
