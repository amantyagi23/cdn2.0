package usecase

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/amantyagi23/cdn-network/internal/domain/bucket"
	"github.com/amantyagi23/cdn-network/internal/domain/file"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/minio"
	"github.com/amantyagi23/cdn-network/internal/repos"
	"github.com/amantyagi23/cdn-network/pkg/cache"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type RegisterFileInput struct {
	BucketID         uuid.UUID
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	ChecksumSHA256   *string
	CacheControl     *string
}

// UploadURLResponse is returned by RequestUploadURL.
type UploadURLResponse struct {
	File      file.FileResponse `json:"file"`
	UploadURL string            `json:"uploadUrl"`
	ObjectKey string            `json:"objectKey"`
	ExpiresIn int               `json:"expiresInSeconds"`
}

// UploadFileInput carries the data needed for a direct server-side upload.
type UploadFileInput struct {
	BucketID         uuid.UUID
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	Reader           io.Reader
	ChecksumSHA256   *string
	CacheControl     *string
}

// FileURLResponse is returned by GetFileURL.
type FileURLResponse struct {
	URL       string `json:"url"`
	ExpiresIn *int   `json:"expiresInSeconds,omitempty"` // nil for permanent public URLs
}

type FileUsecase interface {
	RegisterFile(ctx context.Context, in RegisterFileInput) (*file.FileResponse, error)
	RequestUploadURL(ctx context.Context, in RegisterFileInput) (*UploadURLResponse, error)
	CompleteUpload(ctx context.Context, fileID uuid.UUID) (*file.FileResponse, error)
	// UploadFile performs a direct server-side upload to MinIO in one call.
	UploadFile(ctx context.Context, in UploadFileInput) (*file.FileResponse, error)
	GetFile(ctx context.Context, fileID uuid.UUID) (*file.File, error)
	ListFiles(ctx context.Context, bucketID uuid.UUID, limit, offset int) ([]file.FileResponse, error)
	MarkFileDeleted(ctx context.Context, fileID uuid.UUID) error
	// GetFileURL returns a viewable URL for the file.
	// Public buckets → permanent CDN delivery URL.
	// Private buckets → presigned MinIO GET URL (15 min TTL).
	GetFileURL(ctx context.Context, fileID uuid.UUID) (*FileURLResponse, error)
}

type fileUsecase struct {
	fileRepo      *repos.FileRepository
	bucketRepo    *repos.BucketRepository
	minio         *minio.MinIOClient
	cache         cache.Store
	publicBaseURL string // e.g. "https://cdn.example.com" – no trailing slash
	log           *zap.Logger
}

func NewFileUsecase(
	fileRepo *repos.FileRepository,
	bucketRepo *repos.BucketRepository,
	minio *minio.MinIOClient,
	redisCache cache.Store,
	publicBaseURL string,
	log *zap.Logger,
) FileUsecase {
	return &fileUsecase{
		fileRepo:      fileRepo,
		bucketRepo:    bucketRepo,
		minio:         minio,
		cache:         redisCache,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		log:           log,
	}
}

// objectKey generates a date-partitioned, collision-free object key.
func objectKey(original string) string {
	now := time.Now().UTC()
	clean := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(original)), " ", "-")
	clean = filepath.Base(clean)
	return fmt.Sprintf("%04d/%02d/%s-%s", now.Year(), int(now.Month()), uuid.NewString(), clean)
}

func (uc *fileUsecase) RegisterFile(ctx context.Context, in RegisterFileInput) (*file.FileResponse, error) {
	b, err := uc.bucketRepo.GetByID(ctx, in.BucketID)
	if err != nil {
		return nil, err
	}

	key := objectKey(in.OriginalFilename)
	f, err := file.NewFile(in.BucketID, key, in.OriginalFilename, in.ContentType, in.SizeBytes, b.MaxFileSizeBytes)
	if err != nil {
		return nil, err
	}
	if in.ChecksumSHA256 != nil {
		f.SetChecksum(*in.ChecksumSHA256)
	}
	if in.CacheControl != nil {
		f.SetCacheControl(*in.CacheControl)
	}

	if err := uc.fileRepo.Create(ctx, f); err != nil {
		return nil, fmt.Errorf("RegisterFile: %w", err)
	}
	resp := f.Sanitized()
	return &resp, nil
}

func (uc *fileUsecase) GetFile(ctx context.Context, fileID uuid.UUID) (*file.File, error) {
	cacheKey := cache.FileKey(fileID.String())

	// Try Redis first.
	if cached, err := cache.Get[file.File](ctx, uc.cache, cacheKey); err == nil {
		return &cached, nil
	}

	f, err := uc.fileRepo.GetByID(ctx, fileID)
	if err != nil {
		return nil, err
	}

	if err := cache.Set(ctx, uc.cache, cacheKey, *f, cache.TTLFile); err != nil {
		uc.log.Warn("GetFile: cache set failed", zap.Error(err))
	}
	return f, nil
}

func (uc *fileUsecase) ListFiles(ctx context.Context, bucketID uuid.UUID, limit, offset int) ([]file.FileResponse, error) {
	_, err := uc.bucketRepo.GetByID(ctx, bucketID)
	if err != nil {
		return nil, err
	}

	if limit <= 0 || limit > 200 {
		limit = 50
	}

	cacheKey := cache.FileListKey(bucketID.String(), limit, offset)
	if cached, err := cache.Get[[]file.FileResponse](ctx, uc.cache, cacheKey); err == nil {
		return cached, nil
	}

	fs, err := uc.fileRepo.ListByBucket(ctx, bucketID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]file.FileResponse, len(fs))
	for i, f := range fs {
		out[i] = f.Sanitized()
	}

	if err := cache.Set(ctx, uc.cache, cacheKey, out, cache.TTLFile); err != nil {
		uc.log.Warn("ListFiles: cache set failed", zap.Error(err))
	}
	return out, nil
}

// RequestUploadURL creates the file metadata record and returns a short-lived
// presigned MinIO PUT URL the client uploads to directly.
func (uc *fileUsecase) RequestUploadURL(ctx context.Context, in RegisterFileInput) (*UploadURLResponse, error) {
	b, err := uc.bucketRepo.GetByID(ctx, in.BucketID)
	if err != nil {
		return nil, err
	}

	key := objectKey(in.OriginalFilename)
	f, err := file.NewFile(in.BucketID, key, in.OriginalFilename, in.ContentType, in.SizeBytes, b.MaxFileSizeBytes)
	if err != nil {
		return nil, err
	}
	if in.ChecksumSHA256 != nil {
		f.SetChecksum(*in.ChecksumSHA256)
	}
	if in.CacheControl != nil {
		f.SetCacheControl(*in.CacheControl)
	}

	if err := uc.fileRepo.Create(ctx, f); err != nil {
		return nil, fmt.Errorf("RequestUploadURL: %w", err)
	}

	const uploadExpiry = 15 * time.Minute
	presignURL, err := uc.minio.PresignedPutObject(ctx, b.MinioBucketName, key, uploadExpiry)
	if err != nil {
		return nil, fmt.Errorf("RequestUploadURL: presign: %w", err)
	}

	resp := f.Sanitized()
	return &UploadURLResponse{
		File:      resp,
		UploadURL: presignURL.String(),
		ObjectKey: key,
		ExpiresIn: int(uploadExpiry.Seconds()),
	}, nil
}

// CompleteUpload verifies the object exists in MinIO and updates the ETag.
func (uc *fileUsecase) CompleteUpload(ctx context.Context, fileID uuid.UUID) (*file.FileResponse, error) {
	f, err := uc.fileRepo.GetByID(ctx, fileID)
	if err != nil {
		return nil, err
	}

	b, err := uc.bucketRepo.GetByID(ctx, f.BucketID)
	if err != nil {
		return nil, err
	}

	info, err := uc.minio.StatObject(ctx, b.MinioBucketName, f.ObjectKey)
	if err != nil {
		if minio.IsNotFound(err) {
			return nil, fmt.Errorf("CompleteUpload: object not yet uploaded")
		}
		return nil, fmt.Errorf("CompleteUpload: stat: %w", err)
	}

	f.SetETag(info.ETag)
	if err := uc.fileRepo.Update(ctx, f); err != nil {
		return nil, fmt.Errorf("CompleteUpload: %w", err)
	}

	// Bust cache so next GetFile reflects the updated ETag.
	cache.Invalidate(ctx, uc.cache, cache.FileKey(fileID.String()))

	resp := f.Sanitized()
	return &resp, nil
}

// UploadFile performs a direct server-side upload to MinIO in a single call.
// It creates the DB record, streams the bytes to MinIO, then updates the ETag.
func (uc *fileUsecase) UploadFile(ctx context.Context, in UploadFileInput) (*file.FileResponse, error) {
	b, err := uc.bucketRepo.GetByID(ctx, in.BucketID)
	if err != nil {
		return nil, err
	}

	key := objectKey(in.OriginalFilename)
	f, err := file.NewFile(in.BucketID, key, in.OriginalFilename, in.ContentType, in.SizeBytes, b.MaxFileSizeBytes)
	if err != nil {
		return nil, err
	}
	if in.ChecksumSHA256 != nil {
		f.SetChecksum(*in.ChecksumSHA256)
	}
	if in.CacheControl != nil {
		f.SetCacheControl(*in.CacheControl)
	}

	if err := uc.fileRepo.Create(ctx, f); err != nil {
		return nil, fmt.Errorf("UploadFile: create record: %w", err)
	}

	// Stream bytes to MinIO.  When the client provided the exact size use the
	// fast path; fall back to streaming upload for unknown sizes (size == 0).
	opts := minio.PutObjectOptions{
		ContentType: in.ContentType,
	}
	if in.CacheControl != nil {
		opts.CacheControl = *in.CacheControl
	}
	opts.UserMetadata = map[string]string{"original-filename": in.OriginalFilename}

	var uploadInfo interface{ GetETag() string }
	if in.SizeBytes > 0 {
		info, err := uc.minio.PutObject(ctx, b.MinioBucketName, key, in.Reader, in.SizeBytes, opts)
		if err != nil {
			return nil, fmt.Errorf("UploadFile: minio put: %w", err)
		}
		uploadInfo = minioInfoAdapter{info.ETag}
	} else {
		info, err := uc.minio.PutObjectStreaming(ctx, b.MinioBucketName, key, in.Reader, opts)
		if err != nil {
			return nil, fmt.Errorf("UploadFile: minio put streaming: %w", err)
		}
		uploadInfo = minioInfoAdapter{info.ETag}
	}

	f.SetETag(uploadInfo.GetETag())
	if err := uc.fileRepo.Update(ctx, f); err != nil {
		return nil, fmt.Errorf("UploadFile: update etag: %w", err)
	}

	// Prime the cache with the new file.
	if err := cache.Set(ctx, uc.cache, cache.FileKey(f.ID.String()), *f, cache.TTLFile); err != nil {
		uc.log.Warn("UploadFile: cache set failed", zap.Error(err))
	}

	resp := f.Sanitized()
	return &resp, nil
}

func (uc *fileUsecase) MarkFileDeleted(ctx context.Context, fileID uuid.UUID) error {
	f, err := uc.fileRepo.GetByID(ctx, fileID)
	if err != nil {
		return err
	}
	if err := f.MarkDeleting(); err != nil {
		return err
	}
	if err := uc.fileRepo.Update(ctx, f); err != nil {
		return err
	}
	// Invalidate cache immediately so stale data isn't served.
	cache.Invalidate(ctx, uc.cache, cache.FileKey(fileID.String()))
	return nil
}

// minioInfoAdapter wraps a plain ETag string so both upload paths share the
// same updateETag call site above.
type minioInfoAdapter struct{ etag string }

func (a minioInfoAdapter) GetETag() string { return a.etag }

// GetFileURL returns a URL that can be used to view or download the file.
//
//   - Public bucket  → permanent CDN delivery URL: <publicBaseURL>/cdn/<objectKey>
//   - Private bucket → presigned MinIO GET URL valid for 15 minutes
func (uc *fileUsecase) GetFileURL(ctx context.Context, fileID uuid.UUID) (*FileURLResponse, error) {
	f, err := uc.GetFile(ctx, fileID)
	if err != nil {
		return nil, err
	}

	b, err := uc.bucketRepo.GetByID(ctx, f.BucketID)
	if err != nil {
		return nil, err
	}

	if b.Visibility == bucket.VisibilityPublic {
		url := uc.publicBaseURL + "/cdn/" + f.ObjectKey
		return &FileURLResponse{URL: url}, nil
	}

	// Private: generate a presigned GET URL.
	const expiry = 15 * time.Minute
	presignURL, err := uc.minio.PresignedGetObject(ctx, b.MinioBucketName, f.ObjectKey, expiry, nil)
	if err != nil {
		return nil, fmt.Errorf("GetFileURL: presign: %w", err)
	}
	secs := int(expiry.Seconds())
	return &FileURLResponse{URL: presignURL.String(), ExpiresIn: &secs}, nil
}
