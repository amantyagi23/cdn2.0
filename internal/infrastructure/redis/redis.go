package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var ErrNil = redis.Nil

// RedisClient is a wrapper around go-redis.
type RedisClient struct {
	client *redis.Client
}

// Config holds Redis configuration.
type Config struct {
	Host     string
	Port     string
	Password string
	DB       int

	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	PoolSize     int
	MinIdleConns int
}

// NewRedisClient creates and initializes a new Redis client.
func NewRedisClient(
	ctx context.Context,
	config Config,
	log *zap.Logger,
) (*RedisClient, error) {

	dialTimeout := config.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 5 * time.Second
	}

	readTimeout := config.ReadTimeout
	if readTimeout == 0 {
		readTimeout = 3 * time.Second
	}

	writeTimeout := config.WriteTimeout
	if writeTimeout == 0 {
		writeTimeout = 3 * time.Second
	}

	poolSize := config.PoolSize
	if poolSize == 0 {
		poolSize = 10
	}

	minIdleConns := config.MinIdleConns
	if minIdleConns == 0 {
		minIdleConns = 5
	}

	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%s", config.Host, config.Port),
		Password:     config.Password,
		DB:           config.DB,
		DialTimeout:  dialTimeout,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		PoolSize:     poolSize,
		MinIdleConns: minIdleConns,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Info(
		"Redis connected",
		zap.String("host", config.Host),
		zap.String("port", config.Port),
		zap.Int("db", config.DB),
	)

	return &RedisClient{
		client: client,
	}, nil
}

// Client returns the underlying Redis client.
// Keep this private if you don't want other packages to bypass the wrapper.
func (r *RedisClient) Client() *redis.Client {
	return r.client
}

// Close closes the Redis connection.
func (r *RedisClient) Close() error {
	if r.client == nil {
		return nil
	}

	return r.client.Close()
}

// Ping checks whether Redis is available.
func (r *RedisClient) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// -----------------------------------------------------------------------------
// STRING
// -----------------------------------------------------------------------------

// Set sets a key-value pair with expiration.
func (r *RedisClient) Set(
	ctx context.Context,
	key string,
	value interface{},
	expiration time.Duration,
) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

// Get gets a value by key.
func (r *RedisClient) Get(
	ctx context.Context,
	key string,
) (string, error) {
	return r.client.Get(ctx, key).Result()
}

// GetBytes gets a value as bytes.
func (r *RedisClient) GetBytes(
	ctx context.Context,
	key string,
) ([]byte, error) {
	return r.client.Get(ctx, key).Bytes()
}

// SetNX sets a value only if the key does not already exist.
func (r *RedisClient) SetNX(
	ctx context.Context,
	key string,
	value interface{},
	expiration time.Duration,
) (bool, error) {
	return r.client.SetNX(ctx, key, value, expiration).Result()
}

// GetSet sets a new value and returns the old value.
func (r *RedisClient) GetSet(
	ctx context.Context,
	key string,
	value interface{},
) (string, error) {
	return r.client.GetSet(ctx, key, value).Result()
}

// -----------------------------------------------------------------------------
// JSON
// -----------------------------------------------------------------------------

// SetJSON stores a JSON object.
func (r *RedisClient) SetJSON(
	ctx context.Context,
	key string,
	data interface{},
	expiration time.Duration,
) error {

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return r.Set(ctx, key, jsonData, expiration)
}

// GetJSON retrieves and unmarshals a JSON object.
func (r *RedisClient) GetJSON(
	ctx context.Context,
	key string,
	dest interface{},
) error {

	data, err := r.Get(ctx, key)
	if err != nil {
		return err
	}

	if err := json.Unmarshal([]byte(data), dest); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

// -----------------------------------------------------------------------------
// DELETE / EXISTS / EXPIRATION
// -----------------------------------------------------------------------------

// Delete removes one or more keys.
func (r *RedisClient) Delete(
	ctx context.Context,
	keys ...string,
) error {
	if len(keys) == 0 {
		return nil
	}

	return r.client.Del(ctx, keys...).Err()
}

// Exists checks if a key exists.
func (r *RedisClient) Exists(
	ctx context.Context,
	key string,
) (bool, error) {

	result, err := r.client.Exists(ctx, key).Result()
	return result > 0, err
}

// ExistsMany checks how many of the supplied keys exist.
func (r *RedisClient) ExistsMany(
	ctx context.Context,
	keys ...string,
) (int64, error) {

	return r.client.Exists(ctx, keys...).Result()
}

// Expire sets expiration time for a key.
func (r *RedisClient) Expire(
	ctx context.Context,
	key string,
	expiration time.Duration,
) error {
	return r.client.Expire(ctx, key, expiration).Err()
}

// TTL returns the remaining expiration time.
func (r *RedisClient) TTL(
	ctx context.Context,
	key string,
) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}

// Persist removes expiration from a key.
func (r *RedisClient) Persist(
	ctx context.Context,
	key string,
) error {
	return r.client.Persist(ctx, key).Err()
}

// -----------------------------------------------------------------------------
// COUNTERS
// -----------------------------------------------------------------------------

// Increment increments a counter by 1.
func (r *RedisClient) Increment(
	ctx context.Context,
	key string,
) (int64, error) {
	return r.client.Incr(ctx, key).Result()
}

// IncrementBy increments a counter by amount.
func (r *RedisClient) IncrementBy(
	ctx context.Context,
	key string,
	amount int64,
) (int64, error) {
	return r.client.IncrBy(ctx, key, amount).Result()
}

// Decrement decrements a counter by 1.
func (r *RedisClient) Decrement(
	ctx context.Context,
	key string,
) (int64, error) {
	return r.client.Decr(ctx, key).Result()
}

// DecrementBy decrements a counter by amount.
func (r *RedisClient) DecrementBy(
	ctx context.Context,
	key string,
	amount int64,
) (int64, error) {
	return r.client.DecrBy(ctx, key, amount).Result()
}

// -----------------------------------------------------------------------------
// LIST
// -----------------------------------------------------------------------------

// LPush pushes values to the left of a list.
func (r *RedisClient) LPush(
	ctx context.Context,
	key string,
	values ...interface{},
) error {
	return r.client.LPush(ctx, key, values...).Err()
}

// RPush pushes values to the right of a list.
func (r *RedisClient) RPush(
	ctx context.Context,
	key string,
	values ...interface{},
) error {
	return r.client.RPush(ctx, key, values...).Err()
}

// LPop pops one value from the left.
func (r *RedisClient) LPop(
	ctx context.Context,
	key string,
) (string, error) {
	return r.client.LPop(ctx, key).Result()
}

// RPop pops one value from the right.
func (r *RedisClient) RPop(
	ctx context.Context,
	key string,
) (string, error) {
	return r.client.RPop(ctx, key).Result()
}

// LLen returns the length of a list.
func (r *RedisClient) LLen(
	ctx context.Context,
	key string,
) (int64, error) {
	return r.client.LLen(ctx, key).Result()
}

// LRange returns values between start and stop.
func (r *RedisClient) LRange(
	ctx context.Context,
	key string,
	start int64,
	stop int64,
) ([]string, error) {
	return r.client.LRange(ctx, key, start, stop).Result()
}

// -----------------------------------------------------------------------------
// HASH
// -----------------------------------------------------------------------------

// HSet sets one or more hash fields.
func (r *RedisClient) HSet(
	ctx context.Context,
	key string,
	values ...interface{},
) error {
	return r.client.HSet(ctx, key, values...).Err()
}

// HGet gets a hash field.
func (r *RedisClient) HGet(
	ctx context.Context,
	key string,
	field string,
) (string, error) {
	return r.client.HGet(ctx, key, field).Result()
}

// HGetAll gets all hash fields.
func (r *RedisClient) HGetAll(
	ctx context.Context,
	key string,
) (map[string]string, error) {
	return r.client.HGetAll(ctx, key).Result()
}

// HExists checks whether a hash field exists.
func (r *RedisClient) HExists(
	ctx context.Context,
	key string,
	field string,
) (bool, error) {
	return r.client.HExists(ctx, key, field).Result()
}

// HDel removes hash fields.
func (r *RedisClient) HDel(
	ctx context.Context,
	key string,
	fields ...string,
) error {
	return r.client.HDel(ctx, key, fields...).Err()
}

// -----------------------------------------------------------------------------
// SET
// -----------------------------------------------------------------------------

// SAdd adds members to a set.
func (r *RedisClient) SAdd(
	ctx context.Context,
	key string,
	members ...interface{},
) error {
	return r.client.SAdd(ctx, key, members...).Err()
}

// SRem removes members from a set.
func (r *RedisClient) SRem(
	ctx context.Context,
	key string,
	members ...interface{},
) error {
	return r.client.SRem(ctx, key, members...).Err()
}

// SIsMember checks whether a member exists in a set.
func (r *RedisClient) SIsMember(
	ctx context.Context,
	key string,
	member interface{},
) (bool, error) {
	return r.client.SIsMember(ctx, key, member).Result()
}

// SMembers returns all members of a set.
func (r *RedisClient) SMembers(
	ctx context.Context,
	key string,
) ([]string, error) {
	return r.client.SMembers(ctx, key).Result()
}

// SCard returns the number of members in a set.
func (r *RedisClient) SCard(
	ctx context.Context,
	key string,
) (int64, error) {
	return r.client.SCard(ctx, key).Result()
}

// -----------------------------------------------------------------------------
// SORTED SET - LEADERBOARD
// -----------------------------------------------------------------------------

// ZAdd adds or updates a member's score.
func (r *RedisClient) ZAdd(
	ctx context.Context,
	key string,
	score float64,
	member interface{},
) error {

	return r.client.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: member,
	}).Err()
}

// ZAddMany adds or updates multiple leaderboard members.
func (r *RedisClient) ZAddMany(
	ctx context.Context,
	key string,
	members ...redis.Z,
) error {
	return r.client.ZAdd(ctx, key, members...).Err()
}

// ZScore gets a member's score.
func (r *RedisClient) ZScore(
	ctx context.Context,
	key string,
	member string,
) (float64, error) {
	return r.client.ZScore(ctx, key, member).Result()
}

// ZIncrBy increments a member's score.
func (r *RedisClient) ZIncrBy(
	ctx context.Context,
	key string,
	increment float64,
	member string,
) (float64, error) {

	return r.client.ZIncrBy(
		ctx,
		key,
		increment,
		member,
	).Result()
}

// ZRevRank returns rank from highest score.
// Redis uses zero-based rank.
func (r *RedisClient) ZRevRank(
	ctx context.Context,
	key string,
	member string,
) (int64, error) {
	return r.client.ZRevRank(ctx, key, member).Result()
}

// ZRank returns rank from lowest score.
func (r *RedisClient) ZRank(
	ctx context.Context,
	key string,
	member string,
) (int64, error) {
	return r.client.ZRank(ctx, key, member).Result()
}

// ZRevRange returns members ordered by highest score.
func (r *RedisClient) ZRevRange(
	ctx context.Context,
	key string,
	start int64,
	stop int64,
) ([]string, error) {
	return r.client.ZRevRange(ctx, key, start, stop).Result()
}

// ZRevRangeWithScores returns members and scores ordered highest first.
func (r *RedisClient) ZRevRangeWithScores(
	ctx context.Context,
	key string,
	start int64,
	stop int64,
) ([]redis.Z, error) {

	return r.client.ZRevRangeWithScores(
		ctx,
		key,
		start,
		stop,
	).Result()
}

// ZRange returns members ordered by lowest score.
func (r *RedisClient) ZRange(
	ctx context.Context,
	key string,
	start int64,
	stop int64,
) ([]string, error) {
	return r.client.ZRange(ctx, key, start, stop).Result()
}

// ZRangeWithScores returns members and scores ordered lowest first.
func (r *RedisClient) ZRangeWithScores(
	ctx context.Context,
	key string,
	start int64,
	stop int64,
) ([]redis.Z, error) {

	return r.client.ZRangeWithScores(
		ctx,
		key,
		start,
		stop,
	).Result()
}

// ZCard returns the number of members.
func (r *RedisClient) ZCard(
	ctx context.Context,
	key string,
) (int64, error) {
	return r.client.ZCard(ctx, key).Result()
}

// ZRem removes one or more members.
func (r *RedisClient) ZRem(
	ctx context.Context,
	key string,
	members ...interface{},
) error {
	return r.client.ZRem(ctx, key, members...).Err()
}

// -----------------------------------------------------------------------------
// PUB / SUB
// -----------------------------------------------------------------------------

// Publish sends a message to a Redis channel.
func (r *RedisClient) Publish(
	ctx context.Context,
	channel string,
	message interface{},
) error {
	return r.client.Publish(ctx, channel, message).Err()
}

// Subscribe creates a Redis Pub/Sub subscription.
func (r *RedisClient) Subscribe(
	ctx context.Context,
	channels ...string,
) *redis.PubSub {
	return r.client.Subscribe(ctx, channels...)
}

// PSubscribe subscribes to channels using patterns.
func (r *RedisClient) PSubscribe(
	ctx context.Context,
	patterns ...string,
) *redis.PubSub {
	return r.client.PSubscribe(ctx, patterns...)
}

// -----------------------------------------------------------------------------
// KEYS / SCAN
// -----------------------------------------------------------------------------

// GetAllKeys returns all keys matching a pattern.
//
// WARNING: KEYS can block Redis on a large database.
// Prefer Scan for production systems with many keys.
func (r *RedisClient) GetAllKeys(
	ctx context.Context,
	pattern string,
) ([]string, error) {
	return r.client.Keys(ctx, pattern).Result()
}

// Scan returns keys incrementally.
func (r *RedisClient) Scan(
	ctx context.Context,
	cursor uint64,
	pattern string,
	count int64,
) ([]string, uint64, error) {

	return r.client.Scan(
		ctx,
		cursor,
		pattern,
		count,
	).Result()
}

// -----------------------------------------------------------------------------
// DATABASE
// -----------------------------------------------------------------------------

// FlushDB removes all keys from the current database.
func (r *RedisClient) FlushDB(
	ctx context.Context,
) error {
	return r.client.FlushDB(ctx).Err()
}

// FlushAll removes all keys from all Redis databases.
func (r *RedisClient) FlushAll(
	ctx context.Context,
) error {
	return r.client.FlushAll(ctx).Err()
}

// -----------------------------------------------------------------------------
// ERROR HELPERS
// -----------------------------------------------------------------------------

// IsNotFound returns true when Redis reports that the key does not exist.
func IsNotFound(err error) bool {
	return errors.Is(err, redis.Nil)
}
