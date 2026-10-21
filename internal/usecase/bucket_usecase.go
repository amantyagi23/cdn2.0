package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/amantyagi23/cdn-network/internal/domain/bucket"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/storage"
	"github.com/amantyagi23/cdn-network/internal/repos"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type CreateBucketInput struct {
	APIKeyID         uuid.UUID
	Name             string
	Visibility       string
	MaxFileSizeBytes int64
}

type UpdateBucketInput struct {
	BucketID         uuid.UUID
	Visibility       string
	MaxFileSizeBytes int64
}

type BucketUsecase interface {
	CreateBucket(ctx context.Context, in CreateBucketInput) (*bucket.BucketResponse, error)
	GetBucket(ctx context.Context, bucketID uuid.UUID) (*bucket.Bucket, error)
	ListBuckets(ctx context.Context, apiKeyID uuid.UUID) ([]bucket.BucketResponse, error)
	UpdateBucket(ctx context.Context, in UpdateBucketInput) (*bucket.BucketResponse, error)
	DeleteBucket(ctx context.Context, bucketID uuid.UUID) error
}

type bucketUsecase struct {
	bucketRepo *repos.BucketRepository
	storage    *storage.StorageClient
	log        *zap.Logger
}

func NewBucketUsecase(
	bucketRepo *repos.BucketRepository,
	storage *storage.StorageClient,
	log *zap.Logger,
) BucketUsecase {
	return &bucketUsecase{bucketRepo: bucketRepo, storage: storage, log: log}
}

// storageBucketName derives a globally-unique, filesystem-safe storage name.
func storageBucketName(apiKeyID uuid.UUID, name string) string {
	h := sha256.Sum256([]byte(apiKeyID.String() + "|" + name + "|" + uuid.NewString()))
	return "b-" + hex.EncodeToString(h[:])[:20]
}

func (uc *bucketUsecase) CreateBucket(ctx context.Context, in CreateBucketInput) (*bucket.BucketResponse, error) {
	vis, err := bucket.ParseVisibility(in.Visibility)
	if err != nil {
		return nil, err
	}
	if in.MaxFileSizeBytes <= 0 {
		in.MaxFileSizeBytes = 52428800 // 50 MiB default
	}

	storageName := storageBucketName(in.APIKeyID, strings.ToLower(strings.TrimSpace(in.Name)))
	b, err := bucket.NewBucket(in.APIKeyID, in.Name, storageName, vis, in.MaxFileSizeBytes)
	if err != nil {
		return nil, err
	}

	// Provision storage bucket before writing DB record.
	if err := uc.storage.EnsureBucket(ctx, b.StorageName, ""); err != nil {
		return nil, fmt.Errorf("CreateBucket: provision storage bucket: %w", err)
	}
	if vis == bucket.VisibilityPublic {
		if err := uc.storage.SetPublicRead(ctx, b.StorageName); err != nil {
			return nil, fmt.Errorf("CreateBucket: set public policy: %w", err)
		}
	}

	if err := uc.bucketRepo.Create(ctx, b); err != nil {
		return nil, fmt.Errorf("CreateBucket: %w", err)
	}
	resp := b.Sanitized()
	return &resp, nil
}

func (uc *bucketUsecase) GetBucket(ctx context.Context, bucketID uuid.UUID) (*bucket.Bucket, error) {
	return uc.bucketRepo.GetByID(ctx, bucketID)
}

func (uc *bucketUsecase) ListBuckets(ctx context.Context, apiKeyID uuid.UUID) ([]bucket.BucketResponse, error) {
	bs, err := uc.bucketRepo.ListByAPIKey(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	return mapBuckets(bs), nil
}

func (uc *bucketUsecase) UpdateBucket(ctx context.Context, in UpdateBucketInput) (*bucket.BucketResponse, error) {
	b, err := uc.bucketRepo.GetByID(ctx, in.BucketID)
	if err != nil {
		return nil, err
	}

	if in.Visibility != "" {
		vis, err := bucket.ParseVisibility(in.Visibility)
		if err != nil {
			return nil, err
		}
		if err := b.SetVisibility(vis); err != nil {
			return nil, err
		}
		if vis == bucket.VisibilityPublic {
			if err := uc.storage.SetPublicRead(ctx, b.StorageName); err != nil {
				return nil, fmt.Errorf("UpdateBucket: set public policy: %w", err)
			}
		} else {
			if err := uc.storage.SetPrivate(ctx, b.StorageName); err != nil {
				return nil, fmt.Errorf("UpdateBucket: set private policy: %w", err)
			}
		}
	}
	if in.MaxFileSizeBytes > 0 {
		if err := b.SetMaxFileSize(in.MaxFileSizeBytes); err != nil {
			return nil, err
		}
	}
	if err := uc.bucketRepo.Update(ctx, b); err != nil {
		return nil, err
	}
	resp := b.Sanitized()
	return &resp, nil
}

func (uc *bucketUsecase) DeleteBucket(ctx context.Context, bucketID uuid.UUID) error {
	b, err := uc.bucketRepo.GetByID(ctx, bucketID)
	if err != nil {
		return err
	}
	b.MarkDeleted()
	return uc.bucketRepo.SoftDelete(ctx, b.ID, b.Name)
}

func mapBuckets(bs []*bucket.Bucket) []bucket.BucketResponse {
	out := make([]bucket.BucketResponse, len(bs))
	for i, b := range bs {
		out[i] = b.Sanitized()
	}
	return out
}
