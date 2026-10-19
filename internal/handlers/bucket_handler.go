package handlers

import (
	"errors"

	"github.com/amantyagi23/cdn-network/internal/domain/bucket"
	"github.com/amantyagi23/cdn-network/internal/middleware"
	"github.com/amantyagi23/cdn-network/internal/usecase"
	"github.com/amantyagi23/cdn-network/pkg/response"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type BucketHandler struct {
	uc       usecase.BucketUsecase
	validate *validator.Validate
	log      *zap.Logger
}

func NewBucketHandler(uc usecase.BucketUsecase, log *zap.Logger) *BucketHandler {
	return &BucketHandler{uc: uc, validate: validator.New(), log: log}
}

func (h *BucketHandler) RegisterRoutes(router fiber.Router) {
	g := router.Group("/buckets")
	g.Post("/", h.Create)
	g.Get("/", h.List)
	g.Get("/:bucketId", h.GetByID)
	g.Patch("/:bucketId", h.Update)
	g.Delete("/:bucketId", h.Delete)
}

type createBucketRequest struct {
	Name             string `json:"name"             validate:"required,min=3,max=63"`
	Visibility       string `json:"visibility"       validate:"required,oneof=private public"`
	MaxFileSizeBytes int64  `json:"maxFileSizeBytes" validate:"omitempty,min=1"`
}

func (h *BucketHandler) Create(c *fiber.Ctx) error {
	apiKeyID, ok := middleware.APIKeyIDFromContext(c)
	if !ok {
		return response.InternalError(c)
	}
	var req createBucketRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "PARSE_ERROR", "invalid request body")
	}
	if err := h.validate.Struct(&req); err != nil {
		return response.BadRequest(c, "VALIDATION_ERROR", err.Error())
	}
	resp, err := h.uc.CreateBucket(c.Context(), usecase.CreateBucketInput{
		APIKeyID:         apiKeyID,
		Name:             req.Name,
		Visibility:       req.Visibility,
		MaxFileSizeBytes: req.MaxFileSizeBytes,
	})
	if err != nil {
		return h.mapError(c, err)
	}
	return response.Created(c, "Bucket Created", resp)
}

func (h *BucketHandler) List(c *fiber.Ctx) error {
	apiKeyID, ok := middleware.APIKeyIDFromContext(c)
	if !ok {
		return response.InternalError(c)
	}
	out, err := h.uc.ListBuckets(c.Context(), apiKeyID)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", out)
}

func (h *BucketHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	b, err := h.uc.GetBucket(c.Context(), id)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", b.Sanitized())
}

type updateBucketRequest struct {
	Visibility       string `json:"visibility"       validate:"omitempty,oneof=private public"`
	MaxFileSizeBytes int64  `json:"maxFileSizeBytes" validate:"omitempty,min=1"`
}

func (h *BucketHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	var req updateBucketRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "PARSE_ERROR", "invalid request body")
	}
	if err := h.validate.Struct(&req); err != nil {
		return response.BadRequest(c, "VALIDATION_ERROR", err.Error())
	}
	resp, err := h.uc.UpdateBucket(c.Context(), usecase.UpdateBucketInput{
		BucketID:         id,
		Visibility:       req.Visibility,
		MaxFileSizeBytes: req.MaxFileSizeBytes,
	})
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "Bucket Updated", resp)
}

func (h *BucketHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	if err := h.uc.DeleteBucket(c.Context(), id); err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "Bucket Deleted", nil)
}

func (h *BucketHandler) mapError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, bucket.ErrNotFound):
		return response.NotFound(c, "bucket not found")
	case errors.Is(err, bucket.ErrBucketNameTaken):
		return response.Conflict(c, "bucket name already in use")
	case errors.Is(err, bucket.ErrInvalidName),
		errors.Is(err, bucket.ErrInvalidVisibility),
		errors.Is(err, bucket.ErrInvalidMaxSize):
		return response.BadRequest(c, "DOMAIN_ERROR", err.Error())
	default:
		h.log.Error("unhandled bucket error", zap.Error(err))
		return response.InternalError(c)
	}
}
