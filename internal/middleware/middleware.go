package middleware

import (
	"context"
	"strings"

	"github.com/amantyagi23/cdn-network/internal/domain/apikey"
	"github.com/amantyagi23/cdn-network/internal/security/ratelimit"
	"github.com/amantyagi23/cdn-network/pkg/response"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// APIKeyRepo is the minimal interface required by EnsureAPIKey.
type APIKeyRepo interface {
	GetByHash(ctx context.Context, hash string) (*apikey.APIKey, error)
	UpdateLastUsed(ctx context.Context, id uuid.UUID) error
}

// EnsureAPIKey validates a Bearer API key from the Authorization header.
// On success it stores the key ID in Locals under "apiKeyID".
func EnsureAPIKey(repo APIKeyRepo, log *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		hdr := c.Get("Authorization")
		if hdr == "" {
			return response.Unauthorized(c, "missing Authorization header")
		}
		const prefix = "Bearer "
		if !strings.HasPrefix(hdr, prefix) {
			return response.Unauthorized(c, "Authorization header must use Bearer scheme")
		}
		raw := strings.TrimPrefix(hdr, prefix)
		if !strings.HasPrefix(raw, "ak_") {
			return response.Unauthorized(c, "invalid API key format")
		}

		hash := apikey.HashRawKey(raw)
		k, err := repo.GetByHash(c.Context(), hash)
		if err != nil {
			return response.Unauthorized(c, "invalid API key")
		}
		if !k.IsActive() {
			return response.Unauthorized(c, "API key is revoked or expired")
		}

		// Fire-and-forget last_used update (non-fatal).
		go func() {
			if uerr := repo.UpdateLastUsed(context.Background(), k.ID); uerr != nil {
				log.Warn("EnsureAPIKey: failed to update last_used_at", zap.Error(uerr))
			}
		}()

		c.Locals("apiKeyID", k.ID)
		return c.Next()
	}
}

// APIKeyIDFromContext reads the apiKeyID stored by EnsureAPIKey.
func APIKeyIDFromContext(c *fiber.Ctx) (uuid.UUID, bool) {
	id, ok := c.Locals("apiKeyID").(uuid.UUID)
	return id, ok
}

// RateLimitMiddleware applies the given rate-limit kind to each request.
func RateLimitMiddleware(limiter *ratelimit.Limiter, kind ratelimit.Kind) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.IP()
		if limiter.InPenaltyBox(c.Context(), id, kind) {
			return response.TooManyRequests(c, "too many requests — please try again later")
		}
		d := limiter.Check(c.Context(), id, kind)
		if !d.Allowed {
			return response.TooManyRequests(c, "rate limit exceeded")
		}
		return c.Next()
	}
}
