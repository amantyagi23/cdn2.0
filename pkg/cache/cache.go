// Package cache provides thin Redis-backed typed helpers used by usecases.
// Keys follow the pattern: <prefix>:<id>
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Store is the minimal Redis interface the usecases need.
// *redis.RedisClient satisfies it through its embedded client.
type Store interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, keys ...string) error
}

// ErrMiss is returned when the key is absent from the cache.
var ErrMiss = errors.New("cache miss")

// IsNotFound reports whether the error is a cache miss.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrMiss) || errors.Is(err, redis.Nil)
}

// Set encodes v as JSON and stores it with the given TTL.
func Set[T any](ctx context.Context, s Store, key string, v T, ttl time.Duration) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("cache.Set(%s): marshal: %w", key, err)
	}
	return s.Set(ctx, key, b, ttl)
}

// Get retrieves and decodes a JSON value. Returns ErrMiss on a cache miss.
func Get[T any](ctx context.Context, s Store, key string) (T, error) {
	var zero T
	raw, err := s.Get(ctx, key)
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return zero, ErrMiss
		}
		return zero, fmt.Errorf("cache.Get(%s): %w", key, err)
	}
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return zero, fmt.Errorf("cache.Get(%s): unmarshal: %w", key, err)
	}
	return v, nil
}

// Invalidate removes one or more keys. Errors are silently swallowed; cache
// invalidation is best-effort and must not fail business logic.
func Invalidate(ctx context.Context, s Store, keys ...string) {
	_ = s.Delete(ctx, keys...)
}

// Key helpers — keep key construction in one place.

const (
	TTLFile   = 5 * time.Minute
	TTLBucket = 10 * time.Minute
	TTLDomain = 15 * time.Minute
)

// FileKey returns the cache key for a single file record.
func FileKey(fileID string) string { return "file:" + fileID }

// BucketKey returns the cache key for a bucket record.
func BucketKey(bucketID string) string { return "bucket:" + bucketID }

// DomainKey returns the cache key for a CDN domain record.
func DomainKey(hostname string) string { return "domain:" + hostname }

// FileListKey returns the cache key for a paginated file list within a bucket.
func FileListKey(bucketID string, limit, offset int) string {
	return fmt.Sprintf("filelist:%s:%d:%d", bucketID, limit, offset)
}
