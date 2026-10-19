package handlers

import (
	"errors"
	"strconv"

	"github.com/amantyagi23/cdn-network/internal/domain/bucket"
	"github.com/amantyagi23/cdn-network/internal/domain/file"
	natsclient "github.com/amantyagi23/cdn-network/internal/infrastructure/nats"
	"github.com/amantyagi23/cdn-network/internal/usecase"
	"github.com/amantyagi23/cdn-network/internal/workers"
	"github.com/amantyagi23/cdn-network/pkg/response"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type FileHandler struct {
	uc       usecase.FileUsecase
	nats     *natsclient.NATSClient
	validate *validator.Validate
	log      *zap.Logger
}

func NewFileHandler(uc usecase.FileUsecase, _ interface{}, nats *natsclient.NATSClient, log *zap.Logger) *FileHandler {
	return &FileHandler{uc: uc, nats: nats, validate: validator.New(), log: log}
}

func (h *FileHandler) RegisterRoutes(router fiber.Router) {
	g := router.Group("/buckets/:bucketId/files")
	g.Post("/", h.Register)
	g.Post("/upload", h.Upload)        // direct multipart upload
	g.Post("/upload-url", h.RequestUploadURL)
	g.Post("/:fileId/complete", h.CompleteUpload)
	g.Get("/", h.List)
	g.Get("/:fileId", h.GetByID)
	g.Get("/:fileId/url", h.GetURL)   // viewable / download URL
	g.Delete("/:fileId", h.Delete)
}

type registerFileRequest struct {
	OriginalFilename string  `json:"originalFilename" validate:"required,min=1,max=512"`
	ContentType      string  `json:"contentType"      validate:"required,max=255"`
	SizeBytes        int64   `json:"sizeBytes"        validate:"min=0"`
	ChecksumSHA256   *string `json:"checksumSha256,omitempty" validate:"omitempty,len=64"`
	CacheControl     *string `json:"cacheControl,omitempty"  validate:"omitempty,max=512"`
}

func (h *FileHandler) Register(c *fiber.Ctx) error {
	bucketID, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	var req registerFileRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "PARSE_ERROR", "invalid request body")
	}
	if err := h.validate.Struct(&req); err != nil {
		return response.BadRequest(c, "VALIDATION_ERROR", err.Error())
	}
	resp, err := h.uc.RegisterFile(c.Context(), usecase.RegisterFileInput{
		BucketID:         bucketID,
		OriginalFilename: req.OriginalFilename,
		ContentType:      req.ContentType,
		SizeBytes:        req.SizeBytes,
		ChecksumSHA256:   req.ChecksumSHA256,
		CacheControl:     req.CacheControl,
	})
	if err != nil {
		return h.mapError(c, err)
	}
	return response.Created(c, "File Registered", resp)
}

func (h *FileHandler) List(c *fiber.Ctx) error {
	bucketID, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	out, err := h.uc.ListFiles(c.Context(), bucketID, limit, offset)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", out)
}

func (h *FileHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("fileId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid file id")
	}
	f, err := h.uc.GetFile(c.Context(), id)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", f.Sanitized())
}

func (h *FileHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("fileId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid file id")
	}
	f, err := h.uc.GetFile(c.Context(), id)
	if err != nil {
		return h.mapError(c, err)
	}
	if err := h.uc.MarkFileDeleted(c.Context(), id); err != nil {
		return h.mapError(c, err)
	}
	// Publish async deletion event — non-fatal if NATS is unavailable.
	if h.nats != nil {
		if err := workers.PublishFileDelete(c.Context(), h.nats, id.String(), f.ObjectKey); err != nil {
			h.log.Warn("FileHandler.Delete: publish event failed", zap.Error(err))
		}
	}
	return response.OK(c, "File marked for deletion", nil)
}

func (h *FileHandler) RequestUploadURL(c *fiber.Ctx) error {
	bucketID, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}
	var req registerFileRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "PARSE_ERROR", "invalid request body")
	}
	if err := h.validate.Struct(&req); err != nil {
		return response.BadRequest(c, "VALIDATION_ERROR", err.Error())
	}
	out, err := h.uc.RequestUploadURL(c.Context(), usecase.RegisterFileInput{
		BucketID:         bucketID,
		OriginalFilename: req.OriginalFilename,
		ContentType:      req.ContentType,
		SizeBytes:        req.SizeBytes,
		ChecksumSHA256:   req.ChecksumSHA256,
		CacheControl:     req.CacheControl,
	})
	if err != nil {
		return h.mapError(c, err)
	}
	return response.Created(c, "Upload URL generated", out)
}

func (h *FileHandler) CompleteUpload(c *fiber.Ctx) error {
	fileID, err := uuid.Parse(c.Params("fileId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid file id")
	}
	resp, err := h.uc.CompleteUpload(c.Context(), fileID)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "Upload complete", resp)
}

// Upload handles direct multipart file uploads.
// The client sends multipart/form-data with a "file" part and optional fields:
//
//	cacheControl (string)
//	checksumSha256 (string, 64 hex chars)
func (h *FileHandler) Upload(c *fiber.Ctx) error {
	bucketID, err := uuid.Parse(c.Params("bucketId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid bucket id")
	}

	fh, err := c.FormFile("file")
	if err != nil {
		return response.BadRequest(c, "MISSING_FILE", "multipart field 'file' is required")
	}

	src, err := fh.Open()
	if err != nil {
		return response.InternalError(c)
	}
	defer src.Close()

	// Optional fields.
	var cacheControl *string
	if cc := c.FormValue("cacheControl"); cc != "" {
		cacheControl = &cc
	}
	var checksum *string
	if cs := c.FormValue("checksumSha256"); cs != "" {
		checksum = &cs
	}

	// Derive content type: prefer explicit form value, fall back to detected header.
	contentType := c.FormValue("contentType")
	if contentType == "" {
		contentType = fh.Header.Get("Content-Type")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	resp, err := h.uc.UploadFile(c.Context(), usecase.UploadFileInput{
		BucketID:         bucketID,
		OriginalFilename: fh.Filename,
		ContentType:      contentType,
		SizeBytes:        fh.Size,
		Reader:           src,
		ChecksumSHA256:   checksum,
		CacheControl:     cacheControl,
	})
	if err != nil {
		return h.mapError(c, err)
	}
	return response.Created(c, "File uploaded", resp)
}

// GetURL returns a viewable / downloadable URL for the file.
//   - Public bucket  → permanent CDN URL, no expiry.
//   - Private bucket → presigned MinIO GET URL, expires in 15 minutes.
func (h *FileHandler) GetURL(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("fileId"))
	if err != nil {
		return response.BadRequest(c, "INVALID_ID", "invalid file id")
	}
	out, err := h.uc.GetFileURL(c.Context(), id)
	if err != nil {
		return h.mapError(c, err)
	}
	return response.OK(c, "", out)
}

func (h *FileHandler) mapError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, file.ErrNotFound), errors.Is(err, bucket.ErrNotFound):
		return response.NotFound(c, "not found")
	case errors.Is(err, file.ErrObjectKeyTaken):
		return response.Conflict(c, "object key already exists")
	case errors.Is(err, file.ErrFileTooLarge):
		return response.BadRequest(c, "FILE_TOO_LARGE", err.Error())
	case errors.Is(err, file.ErrInvalidStatusFlow):
		return response.Conflict(c, "invalid file status transition")
	case errors.Is(err, file.ErrBucketIDReq),
		errors.Is(err, file.ErrObjectKeyRequired),
		errors.Is(err, file.ErrFilenameRequired),
		errors.Is(err, file.ErrContentTypeReq),
		errors.Is(err, file.ErrInvalidSize):
		return response.BadRequest(c, "DOMAIN_ERROR", err.Error())
	default:
		h.log.Error("unhandled file error", zap.Error(err))
		return response.InternalError(c)
	}
}
