package ratelimit

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// checkScript is a fixed-window counter that increments and, on first
// touch of a window, sets the expiry. Returns:
//
//	[1] new count
//	[2] TTL in milliseconds
//	[3] violations (persisted separately, see below)
//
// KEYS[1] = counter key, KEYS[2] = violations key
// ARGV[1] = window ms, ARGV[2] = max requests, ARGV[3] = now ms
const checkScript = `
local count = redis.call("INCR", KEYS[1])
if count == 1 then
    redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
local ttl = redis.call("PTTL", KEYS[1])
if ttl < 0 then ttl = tonumber(ARGV[1]) end
local violations = 0
if tonumber(count) > tonumber(ARGV[2]) then
    violations = redis.call("INCR", KEYS[2])
    if violations == 1 then
        redis.call("PEXPIRE", KEYS[2], ARGV[4])
    end
    redis.call("SET", KEYS[2] .. ":last", ARGV[3], "PX", ARGV[4])
end
return {count, ttl, violations}
`

// RedisStore is a fixed-window counter backed by Redis. Safe across
// replicas; the counter key is shared.
type RedisStore struct {
	client *redis.Client
}

func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (s *RedisStore) Check(ctx context.Context, kind Kind, id string, cfg Config) (Decision, error) {
	counterKey := "ratelimit:count:" + string(kind) + ":" + id
	violKey := "ratelimit:viol:" + string(kind) + ":" + id

	res, err := s.client.Eval(ctx, checkScript,
		[]string{counterKey, violKey},
		cfg.WindowMs, cfg.MaxRequests, time.Now().UnixMilli(),
		cfg.PenaltyWindowMs,
	).Result()
	if err != nil {
		return Decision{}, err
	}

	slice, ok := res.([]interface{})
	if !ok || len(slice) != 3 {
		return Decision{}, redis.ErrClosed
	}
	count := toInt64(slice[0])
	ttl := toInt64(slice[1])
	violations := toInt64(slice[2])

	if count > cfg.MaxRequests {
		retrySec := ttl / 1000
		if retrySec < 1 {
			retrySec = 1
		}
		return Decision{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: retrySec,
			Violations: violations,
		}, nil
	}
	remaining := cfg.MaxRequests - count
	if remaining < 0 {
		remaining = 0
	}
	return Decision{Allowed: true, Remaining: remaining, Violations: violations}, nil
}

func (s *RedisStore) Violations(ctx context.Context, kind Kind, id string) (int64, error) {
	n, err := s.client.Get(ctx, "ratelimit:viol:"+string(kind)+":"+id).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return n, err
}

func (s *RedisStore) InPenaltyBox(ctx context.Context, kind Kind, id string, cfg Config) (bool, error) {
	v, err := s.Violations(ctx, kind, id)
	if err != nil || v < cfg.PenaltyViolations {
		return false, err
	}
	// If the violations key still exists, we are within the penalty
	// window. The TTL is refreshed on each new violation, so existence
	// implies "recent enough".
	exists, err := s.client.Exists(ctx, "ratelimit:viol:"+string(kind)+":"+id).Result()
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

func (s *RedisStore) Reset(ctx context.Context, kind Kind, id string) error {
	if kind != "" {
		_, err := s.client.Del(ctx,
			"ratelimit:count:"+string(kind)+":"+id,
			"ratelimit:viol:"+string(kind)+":"+id,
			"ratelimit:viol:"+string(kind)+":"+id+":last",
		).Result()
		return err
	}
	// Reset across kinds — scan and delete. Not atomic but acceptable
	// for an admin operation.
	for _, k := range []Kind{KindAPI, KindUpload, KindDelivery} {
		_ = s.Reset(ctx, k, id)
	}
	return nil
}

func toInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	case []byte:
		n, _ := strconv.ParseInt(string(x), 10, 64)
		return n
	}
	return 0
}
