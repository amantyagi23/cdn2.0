package repos

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/amantyagi23/cdn-network/internal/domain/usage"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type UsageRepository struct {
	db  *pgxpool.Pool
	log *zap.Logger
}

func NewUsageRepository(db *pgxpool.Pool, log *zap.Logger) *UsageRepository {
	return &UsageRepository{db: db, log: log}
}

func (r *UsageRepository) Insert(ctx context.Context, e *usage.Event) error {
	const q = `
		INSERT INTO usage_events
			(api_key_id, bucket_id, event_type, quantity, occurred_at, metadata)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id
	`
	err := r.db.QueryRow(ctx, q,
		e.APIKeyID, e.BucketID, string(e.EventType),
		e.Quantity, e.OccurredAt, e.Metadata,
	).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("UsageRepository.Insert: %w", err)
	}
	return nil
}

func (r *UsageRepository) AggregateByAPIKey(
	ctx context.Context,
	apiKeyID uuid.UUID,
	since, until time.Time,
) ([]usage.Summary, error) {
	const q = `
		SELECT event_type, COALESCE(SUM(quantity), 0)
		FROM usage_events
		WHERE api_key_id = $1
		  AND occurred_at >= $2
		  AND occurred_at <  $3
		GROUP BY event_type
	`
	rows, err := r.db.Query(ctx, q, apiKeyID, since, until)
	if err != nil {
		return nil, fmt.Errorf("UsageRepository.AggregateByAPIKey: %w", err)
	}
	defer rows.Close()

	var out []usage.Summary
	for rows.Next() {
		var s usage.Summary
		if err := rows.Scan(&s.EventType, &s.Total); err != nil {
			return nil, err
		}
		s.Since = since
		s.Until = until
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *UsageRepository) ListByAPIKey(
	ctx context.Context,
	apiKeyID uuid.UUID,
	limit, offset int,
) ([]usage.Event, error) {
	const q = `
		SELECT id, api_key_id, bucket_id, event_type, quantity, occurred_at, metadata
		FROM usage_events
		WHERE api_key_id = $1
		ORDER BY occurred_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Query(ctx, q, apiKeyID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("UsageRepository.ListByAPIKey: %w", err)
	}
	defer rows.Close()

	var out []usage.Event
	for rows.Next() {
		var e usage.Event
		var eventType string
		var meta []byte
		if err := rows.Scan(
			&e.ID, &e.APIKeyID, &e.BucketID, &eventType,
			&e.Quantity, &e.OccurredAt, &meta,
		); err != nil {
			return nil, err
		}
		e.EventType = usage.EventType(eventType)
		e.Metadata = json.RawMessage(meta)
		out = append(out, e)
	}
	return out, rows.Err()
}
