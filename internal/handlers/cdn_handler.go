package handlers

import (
	"context"
	"errors"

	"github.com/amantyagi23/cdn-network/internal/domain/bucket"
	"github.com/amantyagi23/cdn-network/internal/domain/cdn"
	"github.com/amantyagi23/cdn-network/internal/usecase"
	"github.com/amantyagi23/cdn-network/pkg/response"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type CDNHandler struct {
	uc       usecase.CDNUsecase
	validate *validator.Validate
	log      *zap.Logger
}

func NewCDNHandler(uc usecase.CDNUsecase, log *zap.Logger) *CDNHandler {
	return &CDNHandler{uc: uc, validate: validator.New(), log: log}
}

func (h *CDNHandler) RegisterRoutes(router fiber.Router) {
	g := router.Group("/buckets/:bucketId")
	g.Post("/cdn-domains", h.CreateDomain)
	g.Get("/cdn-domains", h.ListDomains)
	g.Post("/cdn-domains/:domainId/verify", h.VerifyDomain)
	g.Post("/cdn-domains/:domainId/disable", h.DisableDomain)
	g.Post("/cdn-domains/:domainId/enable", h.EnableDomain)
	g.Delete("/cdn-domains/:domainId", h.DeleteDomain)

	g.Get("/cdn-settings", h.GetSettings)
	g.Put("/cdn-settings", h.UpsertSettings)
}

type createCDNDomainRequest struct {
	Hostname   string `json:"hostname"   validate:"required,min=4,max=253"`
	TLSEnabled bool   `json:"tlsEnabled"`
}

func (h *CDNHandler) CreateDomain(c *fiber.Ctx) error {
	bucketID, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	var req createCDNDomainRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "PARSE_ERROR", "invalid request body")
	}
	if err := h.validate.Struct(&req); err != nil {
		return response.BadRequest(c, "VALIDATION_ERROR", err.Error())
	}
	resp, err := h.uc.CreateDomain(c.Context(), usecase.CreateCDNDomainInput{
		BucketID:   bucketID,
		Hostname:   req.Hostname,
		TLSEnabled: req.TLSEnabled,
	})
	if err != nil {
		return h.mapError(c, err)
	}
	return response.Created(c, "CDN Domain Created", resp)
}

func (h *CDNHandler) ListDomains(c *fiber.Ctx) error {
	bucketID, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	out, err := h.uc.ListDomains(c.Context(), bucketID)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", out)
}

func (h *CDNHandler) VerifyDomain(c *fiber.Ctx) error {
	return h.transition(c, h.uc.VerifyDomain, "Domain Verified")
}
func (h *CDNHandler) DisableDomain(c *fiber.Ctx) error {
	return h.transition(c, h.uc.DisableDomain, "Domain Disabled")
}
func (h *CDNHandler) EnableDomain(c *fiber.Ctx) error {
	return h.transition(c, h.uc.EnableDomain, "Domain Enabled")
}

func (h *CDNHandler) transition(
	c *fiber.Ctx,
	fn func(ctx context.Context, domainID uuid.UUID) error,
	msg string,
) error {
	domainID, err := uuid.Parse(c.Params("domainId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid domain id")
	}
	if err := fn(c.Context(), domainID); err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, msg, nil)
}

func (h *CDNHandler) DeleteDomain(c *fiber.Ctx) error {
	domainID, err := uuid.Parse(c.Params("domainId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid domain id")
	}
	if err := h.uc.DeleteDomain(c.Context(), domainID); err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "Domain Deleted", nil)
}

func (h *CDNHandler) GetSettings(c *fiber.Ctx) error {
	bucketID, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	resp, err := h.uc.GetSettings(c.Context(), bucketID)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", resp)
}

type upsertSettingsRequest struct {
	DefaultTTLSeconds  int32 `json:"defaultTtlSeconds" validate:"gte=0"`
	MaxTTLSeconds      int32 `json:"maxTtlSeconds"     validate:"gte=0"`
	ForceHTTPS         bool  `json:"forceHttps"`
	CompressionEnabled bool  `json:"compressionEnabled"`
}

func (h *CDNHandler) UpsertSettings(c *fiber.Ctx) error {
	bucketID, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	var req upsertSettingsRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "PARSE_ERROR", "invalid request body")
	}
	if err := h.validate.Struct(&req); err != nil {
		return response.BadRequest(c, "VALIDATION_ERROR", err.Error())
	}
	resp, err := h.uc.UpsertSettings(c.Context(), usecase.UpdateCDNSettingsInput{
		BucketID:           bucketID,
		DefaultTTLSeconds:  req.DefaultTTLSeconds,
		MaxTTLSeconds:      req.MaxTTLSeconds,
		ForceHTTPS:         req.ForceHTTPS,
		CompressionEnabled: req.CompressionEnabled,
	})
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "CDN Settings Updated", resp)
}

func (h *CDNHandler) mapError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, cdn.ErrDomainNotFound), errors.Is(err, bucket.ErrNotFound):
		return response.NotFound(c, "not found")
	case errors.Is(err, cdn.ErrHostnameTaken):
		return response.Conflict(c, "hostname already registered")
	case errors.Is(err, cdn.ErrDomainNotPendingVerify):
		return response.Conflict(c, "only pending domains can be verified")
	case errors.Is(err, cdn.ErrInvalidStatusFlow):
		return response.Conflict(c, "invalid status transition")
	case errors.Is(err, cdn.ErrInvalidHostname),
		errors.Is(err, cdn.ErrHostnameRequired),
		errors.Is(err, cdn.ErrHostnameTooLong),
		errors.Is(err, cdn.ErrNegativeTTL),
		errors.Is(err, cdn.ErrMaxLessThanDefault):
		return response.BadRequest(c, "DOMAIN_ERROR", err.Error())
	default:
		h.log.Error("unhandled cdn error", zap.Error(err))
		return response.InternalError(c)
	}
}
