package usecase

import (
	"context"
	"fmt"
	"io"

	"github.com/amantyagi23/cdn-network/internal/domain/cdn"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/storage"
	"github.com/amantyagi23/cdn-network/internal/repos"
	"github.com/amantyagi23/cdn-network/pkg/cache"
	"go.uber.org/zap"
)

// DeliveryMeta holds everything the delivery handler needs to stream a response.
type DeliveryMeta struct {
	ContentType  string
	CacheControl string
	ETag         string
	SizeBytes    int64
}

// CDNDeliveryUsecase resolves a hostname + object key to a MinIO stream.
type CDNDeliveryUsecase interface {
	// Resolve maps a hostname to the underlying bucket settings.
	Resolve(ctx context.Context, hostname string) (*cdn.Domain, error)
	// Stream writes the object identified by (domain, objectKey) into w.
	Stream(ctx context.Context, domain *cdn.Domain, objectKey string, w io.Writer) (DeliveryMeta, error)
}

type cdnDeliveryUsecase struct {
	cdnRepo    *repos.CDNRepository
	bucketRepo *repos.BucketRepository
	storage    *storage.StorageClient
	cache      cache.Store
	log        *zap.Logger
}

func NewCDNDeliveryUsecase(
	cdnRepo *repos.CDNRepository,
	bucketRepo *repos.BucketRepository,
	storage *storage.StorageClient,
	redisCache cache.Store,
	log *zap.Logger,
) CDNDeliveryUsecase {
	return &cdnDeliveryUsecase{
		cdnRepo:    cdnRepo,
		bucketRepo: bucketRepo,
		storage:    storage,
		cache:      redisCache,
		log:        log,
	}
}

func (uc *cdnDeliveryUsecase) Resolve(ctx context.Context, hostname string) (*cdn.Domain, error) {
	cacheKey := cache.DomainKey(hostname)

	if cached, err := cache.Get[cdn.Domain](ctx, uc.cache, cacheKey); err == nil {
		return &cached, nil
	}

	d, err := uc.cdnRepo.GetDomainByHostname(ctx, hostname)
	if err != nil {
		return nil, err
	}

	if err := cache.Set(ctx, uc.cache, cacheKey, *d, cache.TTLDomain); err != nil {
		uc.log.Warn("CDNDelivery.Resolve: cache set failed", zap.Error(err))
	}
	return d, nil
}

func (uc *cdnDeliveryUsecase) Stream(ctx context.Context, domain *cdn.Domain, objectKey string, w io.Writer) (DeliveryMeta, error) {
	b, err := uc.bucketRepo.GetByID(ctx, domain.BucketID)
	if err != nil {
		return DeliveryMeta{}, fmt.Errorf("CDNDelivery.Stream: bucket lookup: %w", err)
	}

	info, err := uc.storage.DownloadTo(ctx, b.StorageName, objectKey, w)
	if err != nil {
		if storage.IsNotFound(err) {
			return DeliveryMeta{}, cdn.ErrDomainNotFound // reuse sentinel for 404
		}
		return DeliveryMeta{}, fmt.Errorf("CDNDelivery.Stream: %w", err)
	}

	meta := DeliveryMeta{
		ContentType: info.ContentType,
		ETag:        info.ETag,
		SizeBytes:   info.Size,
	}
	// if cc := info.Metadata.Get("Cache-Control"); cc != "" {
	// 	meta.CacheControl = cc
	// }
	return meta, nil
}
