package handlers

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/amantyagi23/cdn-network/internal/domain/usage"
	"github.com/amantyagi23/cdn-network/internal/middleware"
	"github.com/amantyagi23/cdn-network/internal/usecase"
	"github.com/amantyagi23/cdn-network/pkg/response"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type UsageHandler struct {
	uc       usecase.UsageUsecase
	validate *validator.Validate
	log      *zap.Logger
}

func NewUsageHandler(uc usecase.UsageUsecase, log *zap.Logger) *UsageHandler {
	return &UsageHandler{uc: uc, validate: validator.New(), log: log}
}

func (h *UsageHandler) RegisterRoutes(router fiber.Router) {
	g := router.Group("/usage-events")
	g.Post("/", h.Record)
	g.Get("/", h.List)
	g.Get("/summary", h.Summary)
}

type recordUsageRequest struct {
	BucketID  *uuid.UUID      `json:"bucketId,omitempty"`
	EventType string          `json:"eventType" validate:"required"`
	Quantity  int64           `json:"quantity"  validate:"gte=0"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

func (h *UsageHandler) Record(c *fiber.Ctx) error {
	apiKeyID, ok := middleware.APIKeyIDFromContext(c)
	if !ok {
		return response.InternalError(c)
	}
	var req recordUsageRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "PARSE_ERROR", "invalid request body")
	}
	if err := h.validate.Struct(&req); err != nil {
		return response.BadRequest(c, "VALIDATION_ERROR", err.Error())
	}
	if err := h.uc.Record(c.Context(), usecase.RecordUsageInput{
		APIKeyID:  &apiKeyID,
		BucketID:  req.BucketID,
		EventType: req.EventType,
		Quantity:  req.Quantity,
		Metadata:  req.Metadata,
	}); err != nil {
		return h.mapError(c, err)
	}
	return response.Created(c, "Usage Event Recorded", nil)
}

func (h *UsageHandler) List(c *fiber.Ctx) error {
	apiKeyID, ok := middleware.APIKeyIDFromContext(c)
	if !ok {
		return response.InternalError(c)
	}
	limit, _ := strconv.Atoi(c.Query("limit", "100"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	out, err := h.uc.List(c.Context(), apiKeyID, limit, offset)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", out)
}

func (h *UsageHandler) Summary(c *fiber.Ctx) error {
	apiKeyID, ok := middleware.APIKeyIDFromContext(c)
	if !ok {
		return response.InternalError(c)
	}
	var since, until time.Time
	if s := c.Query("since"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			since = t
		}
	}
	if u := c.Query("until"); u != "" {
		if t, err := time.Parse(time.RFC3339, u); err == nil {
			until = t
		}
	}
	out, err := h.uc.Summary(c.Context(), apiKeyID, since, until)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", out)
}

func (h *UsageHandler) mapError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, usage.ErrEventTypeRequired),
		errors.Is(err, usage.ErrQuantityNegative):
		return response.BadRequest(c, "DOMAIN_ERROR", err.Error())
	default:
		h.log.Error("unhandled usage error", zap.Error(err))
		return response.InternalError(c)
	}
}
