package handlers

import (
	"strings"

	"github.com/amantyagi23/cdn-network/internal/domain/cdn"
	"github.com/amantyagi23/cdn-network/internal/usecase"
	"github.com/amantyagi23/cdn-network/pkg/response"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// CDNDeliveryHandler serves public CDN requests.
// It is mounted at the root (not under /api/v1) so that custom hostnames can
// route directly here.  Requests arrive as:
//
//	GET /<object-key>
//
// The handler reads the Host header (or X-Forwarded-Host) to identify which
// CDN domain is being addressed, looks up the backing bucket, then proxies the
// object from MinIO.
type CDNDeliveryHandler struct {
	uc  usecase.CDNDeliveryUsecase
	log *zap.Logger
}

func NewCDNDeliveryHandler(uc usecase.CDNDeliveryUsecase, log *zap.Logger) *CDNDeliveryHandler {
	return &CDNDeliveryHandler{uc: uc, log: log}
}

// RegisterRoutes mounts the delivery endpoint on the given router.
// Call with the fiber.App root or any prefix group.
func (h *CDNDeliveryHandler) RegisterRoutes(router fiber.Router) {
	router.Get("/cdn/*", h.Serve)
}

func (h *CDNDeliveryHandler) Serve(c *fiber.Ctx) error {
	// Prefer X-Forwarded-Host (set by reverse proxies) over the raw Host header.
	hostname := c.Get("X-Forwarded-Host")
	if hostname == "" {
		hostname = c.Hostname()
	}
	// Strip port if present.
	if idx := strings.LastIndex(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	// The wildcard captures everything after /cdn/
	objectKey := c.Params("*")
	if objectKey == "" {
		return response.BadRequest(c, "MISSING_KEY", "object key is required")
	}

	domain, err := h.uc.Resolve(c.Context(), hostname)
	if err != nil {
		if strings.Contains(err.Error(), cdn.ErrDomainNotFound.Error()) {
			return response.NotFound(c, "CDN domain not found")
		}
		h.log.Warn("CDNDelivery: resolve error", zap.String("hostname", hostname), zap.Error(err))
		return response.NotFound(c, "CDN domain not found")
	}

	c.Set(fiber.HeaderContentType, "application/octet-stream") // overwritten below
	meta, err := h.uc.Stream(c.Context(), domain, objectKey, c.Response().BodyWriter())
	if err != nil {
		h.log.Warn("CDNDelivery: stream error",
			zap.String("hostname", hostname),
			zap.String("objectKey", objectKey),
			zap.Error(err),
		)
		return response.NotFound(c, "object not found")
	}

	// Set response headers from object metadata.
	if meta.ContentType != "" {
		c.Set(fiber.HeaderContentType, meta.ContentType)
	}
	if meta.ETag != "" {
		c.Set(fiber.HeaderETag, meta.ETag)
	}
	if meta.CacheControl != "" {
		c.Set(fiber.HeaderCacheControl, meta.CacheControl)
	} else {
		c.Set(fiber.HeaderCacheControl, "public, max-age=3600")
	}
	c.Set("X-CDN-Origin", "minio")

	return nil
}
