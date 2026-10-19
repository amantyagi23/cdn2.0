package usecase

import (
	"context"

	"github.com/amantyagi23/cdn-network/internal/domain/bucket"
	"github.com/amantyagi23/cdn-network/internal/domain/cdn"
	"github.com/amantyagi23/cdn-network/internal/repos"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type CreateCDNDomainInput struct {
	BucketID   uuid.UUID
	Hostname   string
	TLSEnabled bool
}

type UpdateCDNSettingsInput struct {
	BucketID           uuid.UUID
	DefaultTTLSeconds  int32
	MaxTTLSeconds      int32
	ForceHTTPS         bool
	CompressionEnabled bool
}

type CDNUsecase interface {
	CreateDomain(ctx context.Context, in CreateCDNDomainInput) (*cdn.DomainResponse, error)
	ListDomains(ctx context.Context, bucketID uuid.UUID) ([]cdn.DomainResponse, error)
	VerifyDomain(ctx context.Context, domainID uuid.UUID) error
	DisableDomain(ctx context.Context, domainID uuid.UUID) error
	EnableDomain(ctx context.Context, domainID uuid.UUID) error
	DeleteDomain(ctx context.Context, domainID uuid.UUID) error

	GetSettings(ctx context.Context, bucketID uuid.UUID) (*cdn.SettingsResponse, error)
	UpsertSettings(ctx context.Context, in UpdateCDNSettingsInput) (*cdn.SettingsResponse, error)
}

type cdnUsecase struct {
	repo       *repos.CDNRepository
	bucketRepo *repos.BucketRepository
	log        *zap.Logger
}

func NewCDNUsecase(
	repo *repos.CDNRepository,
	bucketRepo *repos.BucketRepository,
	log *zap.Logger,
) CDNUsecase {
	return &cdnUsecase{repo: repo, bucketRepo: bucketRepo, log: log}
}

func (uc *cdnUsecase) CreateDomain(ctx context.Context, in CreateCDNDomainInput) (*cdn.DomainResponse, error) {
	_, err := uc.bucketRepo.GetByID(ctx, in.BucketID)
	if err != nil {
		return nil, bucket.ErrNotFound
	}

	d, err := cdn.NewDomain(in.BucketID, in.Hostname, in.TLSEnabled)
	if err != nil {
		return nil, err
	}
	if err := uc.repo.CreateDomain(ctx, d); err != nil {
		return nil, err
	}
	resp := d.Sanitized()
	resp.VerificationToken = d.VerificationToken // one-time reveal
	return &resp, nil
}

func (uc *cdnUsecase) ListDomains(ctx context.Context, bucketID uuid.UUID) ([]cdn.DomainResponse, error) {
	_, err := uc.bucketRepo.GetByID(ctx, bucketID)
	if err != nil {
		return nil, err
	}

	ds, err := uc.repo.ListDomainsByBucket(ctx, bucketID)
	if err != nil {
		return nil, err
	}
	out := make([]cdn.DomainResponse, len(ds))
	for i, d := range ds {
		out[i] = d.Sanitized()
	}
	return out, nil
}

func (uc *cdnUsecase) VerifyDomain(ctx context.Context, domainID uuid.UUID) error {
	d, err := uc.repo.GetDomainByID(ctx, domainID)
	if err != nil {
		return err
	}
	if err := d.Verify(); err != nil {
		return err
	}
	return uc.repo.UpdateDomain(ctx, d)
}

func (uc *cdnUsecase) DisableDomain(ctx context.Context, domainID uuid.UUID) error {
	d, err := uc.repo.GetDomainByID(ctx, domainID)
	if err != nil {
		return err
	}
	if err := d.Disable(); err != nil {
		return err
	}
	return uc.repo.UpdateDomain(ctx, d)
}

func (uc *cdnUsecase) EnableDomain(ctx context.Context, domainID uuid.UUID) error {
	d, err := uc.repo.GetDomainByID(ctx, domainID)
	if err != nil {
		return err
	}
	if err := d.Enable(); err != nil {
		return err
	}
	return uc.repo.UpdateDomain(ctx, d)
}

func (uc *cdnUsecase) DeleteDomain(ctx context.Context, domainID uuid.UUID) error {
	_, err := uc.repo.GetDomainByID(ctx, domainID)
	if err != nil {
		return err
	}
	return uc.repo.DeleteDomain(ctx, domainID)
}

// ----- Settings -----

func (uc *cdnUsecase) GetSettings(ctx context.Context, bucketID uuid.UUID) (*cdn.SettingsResponse, error) {
	_, err := uc.bucketRepo.GetByID(ctx, bucketID)
	if err != nil {
		return nil, err
	}

	s, err := uc.repo.GetSettings(ctx, bucketID)
	if err != nil {
		dd, derr := cdn.NewSettings(bucketID)
		if derr != nil {
			return nil, derr
		}
		resp := dd.Sanitized()
		return &resp, nil
	}
	resp := s.Sanitized()
	return &resp, nil
}

func (uc *cdnUsecase) UpsertSettings(ctx context.Context, in UpdateCDNSettingsInput) (*cdn.SettingsResponse, error) {
	_, err := uc.bucketRepo.GetByID(ctx, in.BucketID)
	if err != nil {
		return nil, err
	}

	s, err := uc.repo.GetSettings(ctx, in.BucketID)
	if err != nil {
		s, err = cdn.NewSettings(in.BucketID)
		if err != nil {
			return nil, err
		}
	}
	if err := s.Update(in.DefaultTTLSeconds, in.MaxTTLSeconds, in.ForceHTTPS, in.CompressionEnabled); err != nil {
		return nil, err
	}
	if err := uc.repo.UpsertSettings(ctx, s); err != nil {
		return nil, err
	}
	resp := s.Sanitized()
	return &resp, nil
}
