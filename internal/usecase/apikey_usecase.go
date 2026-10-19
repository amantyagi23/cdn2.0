package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/amantyagi23/cdn-network/internal/domain/apikey"
	"github.com/amantyagi23/cdn-network/internal/repos"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type CreateAPIKeyInput struct {
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

type APIKeyUsecase interface {
	CreateAPIKey(ctx context.Context, in CreateAPIKeyInput) (*apikey.APIKeyResponse, error)
	ListAPIKeys(ctx context.Context) ([]apikey.APIKeyResponse, error)
	RevokeAPIKey(ctx context.Context, keyID uuid.UUID) error
	// ValidateRawKey is called from middleware; returns the key aggregate on success.
	ValidateRawKey(ctx context.Context, rawKey string) (*apikey.APIKey, error)
}

type apiKeyUsecase struct {
	repo *repos.APIKeyRepository
	log  *zap.Logger
}

func NewAPIKeyUsecase(
	repo *repos.APIKeyRepository,
	log *zap.Logger,
) APIKeyUsecase {
	return &apiKeyUsecase{repo: repo, log: log}
}

func (uc *apiKeyUsecase) CreateAPIKey(ctx context.Context, in CreateAPIKeyInput) (*apikey.APIKeyResponse, error) {
	k, raw, err := apikey.NewAPIKey(in.Name, in.Scopes, in.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if err := uc.repo.Create(ctx, k); err != nil {
		return nil, fmt.Errorf("CreateAPIKey: %w", err)
	}
	resp := k.Sanitized(raw)
	return &resp, nil
}

func (uc *apiKeyUsecase) ListAPIKeys(ctx context.Context) ([]apikey.APIKeyResponse, error) {
	ks, err := uc.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]apikey.APIKeyResponse, len(ks))
	for i, k := range ks {
		out[i] = k.Sanitized("") // no raw key
	}
	return out, nil
}

func (uc *apiKeyUsecase) RevokeAPIKey(ctx context.Context, keyID uuid.UUID) error {
	k, err := uc.repo.GetByID(ctx, keyID)
	if err != nil {
		return err
	}
	if k.Status == apikey.StatusRevoked {
		return apikey.ErrAPIKeyRevoked
	}
	return uc.repo.Revoke(ctx, keyID)
}

func (uc *apiKeyUsecase) ValidateRawKey(ctx context.Context, rawKey string) (*apikey.APIKey, error) {
	if rawKey == "" {
		return nil, apikey.ErrAPIKeyNotFound
	}
	hash := apikey.HashRawKey(rawKey)
	k, err := uc.repo.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, apikey.ErrAPIKeyNotFound) {
			return nil, apikey.ErrAPIKeyNotFound
		}
		return nil, err
	}
	if k.Status == apikey.StatusRevoked {
		return nil, apikey.ErrAPIKeyRevoked
	}
	if k.IsExpired() {
		return nil, apikey.ErrAPIKeyExpired
	}
	_ = uc.repo.UpdateLastUsed(ctx, k.ID)
	return k, nil
}
