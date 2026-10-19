package usecase

import (
	"context"
	"encoding/json"
	"time"

	"github.com/amantyagi23/cdn-network/internal/domain/usage"
	"github.com/amantyagi23/cdn-network/internal/repos"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type RecordUsageInput struct {
	APIKeyID  *uuid.UUID
	BucketID  *uuid.UUID
	EventType string
	Quantity  int64
	Metadata  json.RawMessage
}

type UsageUsecase interface {
	Record(ctx context.Context, in RecordUsageInput) error
	Summary(ctx context.Context, apiKeyID uuid.UUID, since, until time.Time) ([]usage.Summary, error)
	List(ctx context.Context, apiKeyID uuid.UUID, limit, offset int) ([]usage.Event, error)
}

type usageUsecase struct {
	repo *repos.UsageRepository
	log  *zap.Logger
}

func NewUsageUsecase(
	repo *repos.UsageRepository,
	log *zap.Logger,
) UsageUsecase {
	return &usageUsecase{repo: repo, log: log}
}

func (uc *usageUsecase) Record(ctx context.Context, in RecordUsageInput) error {
	et, err := usage.ParseEventType(in.EventType)
	if err != nil {
		return err
	}
	e, err := usage.NewEvent(in.APIKeyID, in.BucketID, et, in.Quantity, in.Metadata)
	if err != nil {
		return err
	}
	return uc.repo.Insert(ctx, e)
}

func (uc *usageUsecase) Summary(ctx context.Context, apiKeyID uuid.UUID, since, until time.Time) ([]usage.Summary, error) {
	if since.IsZero() {
		since = time.Now().UTC().AddDate(0, 0, -30)
	}
	if until.IsZero() {
		until = time.Now().UTC()
	}
	return uc.repo.AggregateByAPIKey(ctx, apiKeyID, since, until)
}

func (uc *usageUsecase) List(ctx context.Context, apiKeyID uuid.UUID, limit, offset int) ([]usage.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	return uc.repo.ListByAPIKey(ctx, apiKeyID, limit, offset)
}
