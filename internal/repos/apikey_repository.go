package repos

import (
	"context"
	"errors"
	"fmt"

	"github.com/amantyagi23/cdn-network/internal/domain/apikey"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type APIKeyRepository struct {
	db  *pgxpool.Pool
	log *zap.Logger
}

func NewAPIKeyRepository(db *pgxpool.Pool, log *zap.Logger) *APIKeyRepository {
	return &APIKeyRepository{db: db, log: log}
}

func (r *APIKeyRepository) Create(ctx context.Context, k *apikey.APIKey) error {
	const q = `
		INSERT INTO api_keys
			(id, name, key_prefix, key_hash, scopes, status, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.db.Exec(ctx, q,
		k.ID, k.Name, k.KeyPrefix, k.KeyHash,
		k.Scopes, string(k.Status), k.ExpiresAt, k.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("APIKeyRepository.Create: %w", err)
	}
	return nil
}

const apikeyColumns = `
	id, name, key_prefix, key_hash,
	scopes, status, expires_at, last_used_at, created_at, revoked_at
`

func scanAPIKey(row pgx.Row) (*apikey.APIKey, error) {
	var k apikey.APIKey
	var status string
	if err := row.Scan(
		&k.ID, &k.Name, &k.KeyPrefix, &k.KeyHash,
		&k.Scopes, &status, &k.ExpiresAt, &k.LastUsedAt, &k.CreatedAt, &k.RevokedAt,
	); err != nil {
		return nil, err
	}
	k.Status = apikey.Status(status)
	return &k, nil
}

func (r *APIKeyRepository) GetByID(ctx context.Context, id uuid.UUID) (*apikey.APIKey, error) {
	q := `SELECT ` + apikeyColumns + ` FROM api_keys WHERE id = $1 LIMIT 1`
	k, err := scanAPIKey(r.db.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apikey.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("APIKeyRepository.GetByID: %w", err)
	}
	return k, nil
}

func (r *APIKeyRepository) GetByHash(ctx context.Context, hash string) (*apikey.APIKey, error) {
	q := `SELECT ` + apikeyColumns + ` FROM api_keys WHERE key_hash = $1 LIMIT 1`
	k, err := scanAPIKey(r.db.QueryRow(ctx, q, hash))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apikey.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("APIKeyRepository.GetByHash: %w", err)
	}
	return k, nil
}

func (r *APIKeyRepository) List(ctx context.Context) ([]*apikey.APIKey, error) {
	q := `SELECT ` + apikeyColumns + `
		FROM api_keys
		ORDER BY created_at DESC`
	rows, err := r.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("APIKeyRepository.List: %w", err)
	}
	defer rows.Close()

	var out []*apikey.APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("APIKeyRepository.List scan: %w", err)
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (r *APIKeyRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	const q = `
		UPDATE api_keys
		SET status = 'revoked', revoked_at = NOW()
		WHERE id = $1 AND status = 'active' AND revoked_at IS NULL
	`
	res, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("APIKeyRepository.Revoke: %w", err)
	}
	if res.RowsAffected() == 0 {
		return apikey.ErrAPIKeyNotFound
	}
	return nil
}

func (r *APIKeyRepository) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`
	if _, err := r.db.Exec(ctx, q, id); err != nil {
		return fmt.Errorf("APIKeyRepository.UpdateLastUsed: %w", err)
	}
	return nil
}

// isPGXDuplicateKey reports whether err is a PostgreSQL unique violation (SQLSTATE 23505).
func isPGXDuplicateKey(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}

func isPGXForeignKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return false
}
