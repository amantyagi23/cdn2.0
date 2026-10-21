package repos

import (
	"context"
	"errors"
	"fmt"

	"github.com/amantyagi23/cdn-network/internal/domain/cdn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type CDNRepository struct {
	db  *pgxpool.Pool
	log *zap.Logger
}

func NewCDNRepository(db *pgxpool.Pool, log *zap.Logger) *CDNRepository {
	return &CDNRepository{db: db, log: log}
}

// ---------- Domains ----------

func (r *CDNRepository) CreateDomain(ctx context.Context, d *cdn.Domain) error {
	const q = `
		INSERT INTO cdn_domains
			(id, bucket_id, hostname, status, verification_token,
			 verified_at, tls_enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`
	_, err := r.db.Exec(ctx, q,
		d.ID, d.BucketID, d.Hostname, string(d.Status),
		d.VerificationToken, d.VerifiedAt, d.TLSEnabled, d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		if isPGXDuplicateKey(err) {
			return cdn.ErrHostnameTaken
		}
		return fmt.Errorf("CDNRepository.CreateDomain: %w", err)
	}
	return nil
}

func (r *CDNRepository) UpdateDomain(ctx context.Context, d *cdn.Domain) error {
	const q = `
		UPDATE cdn_domains SET
			status = $2, verified_at = $3, updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`
	err := r.db.QueryRow(ctx, q, d.ID, string(d.Status), d.VerifiedAt).Scan(&d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cdn.ErrDomainNotFound
		}
		return fmt.Errorf("CDNRepository.UpdateDomain: %w", err)
	}
	return nil
}

const domainColumns = `
	id, bucket_id, hostname, status, verification_token,
	verified_at, tls_enabled, created_at, updated_at
`

func scanDomain(row pgx.Row) (*cdn.Domain, error) {
	var d cdn.Domain
	var status string
	if err := row.Scan(
		&d.ID, &d.BucketID, &d.Hostname, &status,
		&d.VerificationToken, &d.VerifiedAt, &d.TLSEnabled,
		&d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, err
	}
	d.Status = cdn.DomainStatus(status)
	return &d, nil
}

func (r *CDNRepository) GetDomainByID(ctx context.Context, id uuid.UUID) (*cdn.Domain, error) {
	q := `SELECT ` + domainColumns + ` FROM cdn_domains WHERE id=$1 LIMIT 1`
	d, err := scanDomain(r.db.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, cdn.ErrDomainNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("CDNRepository.GetDomainByID: %w", err)
	}
	return d, nil
}

func (r *CDNRepository) ListDomainsByBucket(ctx context.Context, bucketID uuid.UUID) ([]*cdn.Domain, error) {
	q := `SELECT ` + domainColumns + `
		FROM cdn_domains WHERE bucket_id=$1 ORDER BY created_at DESC`
	rows, err := r.db.Query(ctx, q, bucketID)
	if err != nil {
		return nil, fmt.Errorf("CDNRepository.ListDomainsByBucket: %w", err)
	}
	defer rows.Close()

	var out []*cdn.Domain
	for rows.Next() {
		d, err := scanDomain(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *CDNRepository) GetDomainByHostname(ctx context.Context, hostname string) (*cdn.Domain, error) {
	q := `SELECT ` + domainColumns + `
		FROM cdn_domains
		WHERE hostname = $1 AND status = 'active'
		LIMIT 1`
	d, err := scanDomain(r.db.QueryRow(ctx, q, hostname))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, cdn.ErrDomainNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("CDNRepository.GetDomainByHostname: %w", err)
	}
	return d, nil
}
func (r *CDNRepository) GetDomainByHostnameAndBucketId(ctx context.Context, hostname string, bucketId string) (*cdn.Domain, error) {
	q := `SELECT ` + domainColumns + `
		FROM cdn_domains
		WHERE hostname = $1 AND bucket_id = $2 AND status = 'active'
		LIMIT 1`
	d, err := scanDomain(r.db.QueryRow(ctx, q, hostname, bucketId))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, cdn.ErrDomainNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("CDNRepository.GetDomainByHostnameAndBucketId: %w", err)
	}
	return d, nil
}

func (r *CDNRepository) DeleteDomain(ctx context.Context, id uuid.UUID) error {
	const q = `DELETE FROM cdn_domains WHERE id = $1`
	res, err := r.db.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("CDNRepository.DeleteDomain: %w", err)
	}
	if res.RowsAffected() == 0 {
		return cdn.ErrDomainNotFound
	}
	return nil
}

// ---------- Settings ----------

func (r *CDNRepository) UpsertSettings(ctx context.Context, s *cdn.Settings) error {
	const q = `
		INSERT INTO cdn_settings
			(bucket_id, default_ttl_seconds, max_ttl_seconds, force_https,
			 compression_enabled, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (bucket_id) DO UPDATE SET
			default_ttl_seconds = EXCLUDED.default_ttl_seconds,
			max_ttl_seconds = EXCLUDED.max_ttl_seconds,
			force_https = EXCLUDED.force_https,
			compression_enabled = EXCLUDED.compression_enabled,
			updated_at = NOW()
		RETURNING updated_at
	`
	return r.db.QueryRow(ctx, q,
		s.BucketID, s.DefaultTTLSeconds, s.MaxTTLSeconds,
		s.ForceHTTPS, s.CompressionEnabled, s.UpdatedAt,
	).Scan(&s.UpdatedAt)
}

func (r *CDNRepository) GetSettings(ctx context.Context, bucketID uuid.UUID) (*cdn.Settings, error) {
	const q = `
		SELECT bucket_id, default_ttl_seconds, max_ttl_seconds, force_https,
		       compression_enabled, updated_at
		FROM cdn_settings WHERE bucket_id = $1
	`
	var s cdn.Settings
	err := r.db.QueryRow(ctx, q, bucketID).Scan(
		&s.BucketID, &s.DefaultTTLSeconds, &s.MaxTTLSeconds,
		&s.ForceHTTPS, &s.CompressionEnabled, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, cdn.ErrSettingsNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("CDNRepository.GetSettings: %w", err)
	}
	return &s, nil
}
