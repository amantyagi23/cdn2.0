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
	"github.com/amantyagi23/cdn-network/internal/infrastructure/storage"
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
	storage       *storage.StorageClient
	cache         cache.Store
	publicBaseURL string // e.g. "https://cdn.example.com" – no trailing slash
	log           *zap.Logger
}

func NewFileUsecase(
	fileRepo *repos.FileRepository,
	bucketRepo *repos.BucketRepository,
	storage *storage.StorageClient,
	redisCache cache.Store,
	publicBaseURL string,
	log *zap.Logger,
) FileUsecase {
	return &fileUsecase{
		fileRepo:      fileRepo,
		bucketRepo:    bucketRepo,
		storage:       storage,
		cache:         redisCache,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
		log:           log,
	}
}

// objectKey generates a date-partitioned, collision-free object key.
// The client-supplied filename is used only as a human-readable suffix;
// a UUID prefix guarantees uniqueness and prevents collisions.  All
// path-traversal sequences and unsafe characters are stripped so the
// resulting key is safe to use as a storage path component.
func objectKey(bucketName, original string) string {
	now := time.Now().UTC()

	// Normalize and validate the bucket name.
	bucket := strings.ToLower(strings.TrimSpace(bucketName))
	bucket = strings.ReplaceAll(bucket, " ", "-")

	var bucketBuilder strings.Builder
	for _, r := range bucket {
		if (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' {
			bucketBuilder.WriteRune(r)
		}
	}

	bucket = bucketBuilder.String()
	if bucket == "" {
		bucket = "default"
	}

	// Sanitize the original filename.
	base := filepath.Base(strings.TrimSpace(original))
	if base == "." || base == ".." || base == "/" || base == "" {
		base = "file"
	}

	base = strings.ReplaceAll(strings.ToLower(base), " ", "-")

	var sb strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}

	safe := strings.Trim(sb.String(), ".-")
	if safe == "" {
		safe = "file"
	}

	return fmt.Sprintf(
		"%s/%04d/%02d/%s-%s",
		bucket,
		now.Year(),
		int(now.Month()),
		uuid.NewString(),
		safe,
	)
}

func (uc *fileUsecase) RegisterFile(ctx context.Context, in RegisterFileInput) (*file.FileResponse, error) {
	b, err := uc.bucketRepo.GetByID(ctx, in.BucketID)
	if err != nil {
		return nil, err
	}

	key := objectKey(b.Name, in.OriginalFilename)
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

	key := objectKey(b.Name, in.OriginalFilename)
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
	presignURL, err := uc.storage.PresignedPutObject(ctx, b.StorageName, key, uploadExpiry)
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

	info, err := uc.storage.StatObject(ctx, b.StorageName, f.ObjectKey)
	if err != nil {
		if storage.IsNotFound(err) {
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

	key := objectKey(b.Name, in.OriginalFilename)
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
	opts := storage.PutObjectOptions{
		ContentType: in.ContentType,
	}
	if in.CacheControl != nil {
		opts.CacheControl = *in.CacheControl
	}
	opts.UserMetadata = map[string]string{"original-filename": in.OriginalFilename}

	var etag string
	if in.SizeBytes > 0 {
		info, err := uc.storage.PutObject(ctx, b.StorageName, key, in.Reader, in.SizeBytes, opts)
		if err != nil {
			return nil, fmt.Errorf("UploadFile: storage put: %w", err)
		}
		etag = info.ETag
	} else {
		info, err := uc.storage.PutObjectStreaming(ctx, b.StorageName, key, in.Reader, opts)
		if err != nil {
			return nil, fmt.Errorf("UploadFile: storage put streaming: %w", err)
		}
		etag = info.ETag
	}

	f.SetETag(etag)
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

// GetFileURL returns a URL that can be used to view or download the file.
//
//   - Public bucket  → permanent CDN delivery URL: <publicBaseURL>/cdn/<objectKey>
//   - Private bucket → presigned MinIO GET URL valid for 15 minutes
func (uc *fileUsecase) GetFileURL(
	ctx context.Context,
	fileID uuid.UUID,
) (*FileURLResponse, error) {
	f, err := uc.GetFile(ctx, fileID)
	if err != nil {
		return nil, err
	}

	b, err := uc.bucketRepo.GetByID(ctx, f.BucketID)
	if err != nil {
		return nil, err
	}

	if b.Visibility == bucket.VisibilityPublic {
		baseURL := strings.TrimRight(uc.publicBaseURL, "/")
		objectKey := strings.TrimLeft(f.ObjectKey, "/")

		url := baseURL + "/" + objectKey

		return &FileURLResponse{URL: url}, nil
	}

	const expiry = 15 * time.Minute

	presignURL, err := uc.storage.PresignedGetObject(
		ctx,
		b.StorageName,
		f.ObjectKey,
		expiry,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("GetFileURL: presign: %w", err)
	}

	secs := int(expiry.Seconds())

	return &FileURLResponse{
		URL:       presignURL.String(),
		ExpiresIn: &secs,
	}, nil
}
