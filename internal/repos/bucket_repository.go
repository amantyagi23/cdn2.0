package repos

import (
	"context"
	"errors"
	"fmt"

	"github.com/amantyagi23/cdn-network/internal/domain/bucket"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type BucketRepository struct {
	db  *pgxpool.Pool
	log *zap.Logger
}

func NewBucketRepository(db *pgxpool.Pool, log *zap.Logger) *BucketRepository {
	return &BucketRepository{db: db, log: log}
}

func (r *BucketRepository) Create(ctx context.Context, b *bucket.Bucket) error {
	const q = `
		INSERT INTO storage_buckets
			(id, api_key_id, name, minio_bucket_name, visibility,
			 max_file_size_bytes, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`
	_, err := r.db.Exec(ctx, q,
		b.ID, b.APIKeyID, b.Name, b.MinioBucketName,
		string(b.Visibility), b.MaxFileSizeBytes, b.CreatedAt, b.UpdatedAt,
	)
	if err != nil {
		if isPGXDuplicateKey(err) {
			return bucket.ErrBucketNameTaken
		}
		return fmt.Errorf("BucketRepository.Create: %w", err)
	}
	return nil
}

func (r *BucketRepository) Update(ctx context.Context, b *bucket.Bucket) error {
	const q = `
		UPDATE storage_buckets
		SET visibility = $2, max_file_size_bytes = $3, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING updated_at
	`
	err := r.db.QueryRow(ctx, q, b.ID, string(b.Visibility), b.MaxFileSizeBytes).Scan(&b.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return bucket.ErrNotFound
		}
		return fmt.Errorf("BucketRepository.Update: %w", err)
	}
	return nil
}

const bucketColumns = `
	id, api_key_id, name, minio_bucket_name, visibility,
	max_file_size_bytes, created_at, updated_at, deleted_at
`

func scanBucket(row pgx.Row) (*bucket.Bucket, error) {
	var b bucket.Bucket
	var vis string
	if err := row.Scan(
		&b.ID, &b.APIKeyID, &b.Name, &b.MinioBucketName, &vis,
		&b.MaxFileSizeBytes, &b.CreatedAt, &b.UpdatedAt, &b.DeletedAt,
	); err != nil {
		return nil, err
	}
	b.Visibility = bucket.Visibility(vis)
	return &b, nil
}

func (r *BucketRepository) GetByID(ctx context.Context, id uuid.UUID) (*bucket.Bucket, error) {
	q := `SELECT ` + bucketColumns + ` FROM storage_buckets WHERE id=$1 AND deleted_at IS NULL LIMIT 1`
	b, err := scanBucket(r.db.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, bucket.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("BucketRepository.GetByID: %w", err)
	}
	return b, nil
}

func (r *BucketRepository) ListByAPIKey(ctx context.Context, apiKeyID uuid.UUID) ([]*bucket.Bucket, error) {
	q := `SELECT ` + bucketColumns + `
		FROM storage_buckets WHERE api_key_id=$1 AND deleted_at IS NULL
		ORDER BY created_at DESC`
	return r.scanMany(ctx, q, apiKeyID)
}

func (r *BucketRepository) scanMany(ctx context.Context, q string, args ...any) ([]*bucket.Bucket, error) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("BucketRepository.scanMany: %w", err)
	}
	defer rows.Close()

	var out []*bucket.Bucket
	for rows.Next() {
		b, err := scanBucket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *BucketRepository) SoftDelete(ctx context.Context, id uuid.UUID, newName string) error {
	const q = `
		UPDATE storage_buckets
		SET deleted_at = NOW(), updated_at = NOW(), name = $2
		WHERE id = $1 AND deleted_at IS NULL
	`
	res, err := r.db.Exec(ctx, q, id, newName)
	if err != nil {
		return fmt.Errorf("BucketRepository.SoftDelete: %w", err)
	}
	if res.RowsAffected() == 0 {
		return bucket.ErrNotFound
	}
	return nil
}
