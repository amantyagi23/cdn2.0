package repos

import (
	"context"
	"errors"
	"fmt"

	"github.com/amantyagi23/cdn-network/internal/domain/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type FileRepository struct {
	db  *pgxpool.Pool
	log *zap.Logger
}

func NewFileRepository(db *pgxpool.Pool, log *zap.Logger) *FileRepository {
	return &FileRepository{db: db, log: log}
}

func (r *FileRepository) Create(ctx context.Context, f *file.File) error {
	const q = `
		INSERT INTO files
			(id, bucket_id, object_key, original_filename,
			 content_type, size_bytes, checksum_sha256, etag, cache_control,
			 status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
	`
	_, err := r.db.Exec(ctx, q,
		f.ID, f.BucketID, f.ObjectKey, f.OriginalFilename,
		f.ContentType, f.SizeBytes, f.ChecksumSHA256, f.ETag, f.CacheControl,
		string(f.Status), f.CreatedAt, f.UpdatedAt,
	)
	if err != nil {
		if isPGXDuplicateKey(err) {
			return file.ErrObjectKeyTaken
		}
		return fmt.Errorf("FileRepository.Create: %w", err)
	}
	return nil
}

func (r *FileRepository) Update(ctx context.Context, f *file.File) error {
	const q = `
		UPDATE files SET
			checksum_sha256 = $2,
			etag = $3,
			cache_control = $4,
			status = $5,
			updated_at = NOW(),
			deleted_at = $6
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING updated_at
	`
	err := r.db.QueryRow(ctx, q,
		f.ID, f.ChecksumSHA256, f.ETag, f.CacheControl,
		string(f.Status), f.DeletedAt,
	).Scan(&f.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return file.ErrNotFound
		}
		return fmt.Errorf("FileRepository.Update: %w", err)
	}
	return nil
}

const fileColumns = `
	id, bucket_id, object_key, original_filename, content_type,
	size_bytes, checksum_sha256, etag, cache_control, status,
	created_at, updated_at, deleted_at
`

func scanFile(row pgx.Row) (*file.File, error) {
	var f file.File
	var status string
	if err := row.Scan(
		&f.ID, &f.BucketID, &f.ObjectKey, &f.OriginalFilename,
		&f.ContentType, &f.SizeBytes, &f.ChecksumSHA256, &f.ETag, &f.CacheControl,
		&status, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt,
	); err != nil {
		return nil, err
	}
	f.Status = file.Status(status)
	return &f, nil
}

func (r *FileRepository) GetByID(ctx context.Context, id uuid.UUID) (*file.File, error) {
	q := `SELECT ` + fileColumns + ` FROM files WHERE id=$1 AND deleted_at IS NULL LIMIT 1`
	f, err := scanFile(r.db.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, file.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("FileRepository.GetByID: %w", err)
	}
	return f, nil
}

func (r *FileRepository) ListByBucket(ctx context.Context, bucketID uuid.UUID, limit, offset int) ([]*file.File, error) {
	q := `SELECT ` + fileColumns + `
		FROM files
		WHERE bucket_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`
	rows, err := r.db.Query(ctx, q, bucketID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("FileRepository.ListByBucket: %w", err)
	}
	defer rows.Close()

	var out []*file.File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
