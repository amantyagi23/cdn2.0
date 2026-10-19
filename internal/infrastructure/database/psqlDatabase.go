package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Config struct {
	URI            string
	MaxPoolSize    uint64
	MinPoolSize    uint64
	ConnectTimeout time.Duration
	SocketTimeout  time.Duration
}

// DefaultConfig returns sane defaults.
func DefaultConfig(uri string) Config {

	return Config{
		URI:            uri,
		MaxPoolSize:    25,
		MinPoolSize:    5,
		ConnectTimeout: 10 * time.Second,
		SocketTimeout:  10 * time.Second,
	}
}

// Connect initializes PostgreSQL connection pool.
func Connect(ctx context.Context, cfg Config, log *zap.Logger) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URI)
	if err != nil {
		return nil, fmt.Errorf("database: parse config: %w", err)
	}

	poolConfig.MaxConns = int32(cfg.MaxPoolSize)
	poolConfig.MinConns = int32(cfg.MinPoolSize)
	poolConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout

	// PostgreSQL/pgx doesn't have a direct "socket timeout" setting
	// equivalent to some other database drivers.
	// Use context timeouts for operations when needed.

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping failed: %w", err)
	}

	log.Info(
		"Postgres connected",
		zap.Uint64("maxPoolSize", cfg.MaxPoolSize),
		zap.Uint64("minPoolSize", cfg.MinPoolSize),
	)

	return pool, nil
}
