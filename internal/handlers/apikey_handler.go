package handlers

import (
	"errors"
	"time"

	"github.com/amantyagi23/cdn-network/internal/domain/apikey"
	"github.com/amantyagi23/cdn-network/internal/usecase"
	"github.com/amantyagi23/cdn-network/pkg/response"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type APIKeyHandler struct {
	uc       usecase.APIKeyUsecase
	validate *validator.Validate
	log      *zap.Logger
}

func NewAPIKeyHandler(uc usecase.APIKeyUsecase, log *zap.Logger) *APIKeyHandler {
	return &APIKeyHandler{uc: uc, validate: validator.New(), log: log}
}

func (h *APIKeyHandler) RegisterRoutes(router fiber.Router) {
	g := router.Group("/api-keys")
	g.Post("/", h.Create)
	g.Get("/", h.List)
	g.Delete("/:keyId", h.Revoke)
}

type createAPIKeyRequest struct {
	Name      string     `json:"name"      validate:"required,min=2,max=120"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (h *APIKeyHandler) Create(c *fiber.Ctx) error {
	var req createAPIKeyRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "PARSE_ERROR", "invalid request body")
	}
	if err := h.validate.Struct(&req); err != nil {
		return response.BadRequest(c, "VALIDATION_ERROR", err.Error())
	}
	resp, err := h.uc.CreateAPIKey(c.Context(), usecase.CreateAPIKeyInput{
		Name:      req.Name,
		Scopes:    req.Scopes,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		return h.mapError(c, err)
	}
	return response.Created(c, "API Key Created", resp)
}

func (h *APIKeyHandler) List(c *fiber.Ctx) error {
	out, err := h.uc.ListAPIKeys(c.Context())
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", out)
}

func (h *APIKeyHandler) Revoke(c *fiber.Ctx) error {
	keyID, err := uuid.Parse(c.Params("keyId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid api key id")
	}
	if err := h.uc.RevokeAPIKey(c.Context(), keyID); err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "API Key Revoked", nil)
}

func (h *APIKeyHandler) mapError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, apikey.ErrAPIKeyNotFound):
		return response.NotFound(c, "api key not found")
	case errors.Is(err, apikey.ErrAPIKeyRevoked):
		return response.Conflict(c, "api key is already revoked")
	case errors.Is(err, apikey.ErrAPIKeyExpired):
		return response.BadRequest(c, "API_KEY_EXPIRED", "api key has expired")
	case errors.Is(err, apikey.ErrNameRequired), errors.Is(err, apikey.ErrNameTooLong):
		return response.BadRequest(c, "DOMAIN_ERROR", err.Error())
	default:
		h.log.Error("unhandled apikey error", zap.Error(err))
		return response.InternalError(c)
	}
}
