package file

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound          = errors.New("file not found")
	ErrObjectKeyTaken    = errors.New("object key already exists in this bucket")
	ErrBucketIDReq       = errors.New("bucket id is required")
	ErrObjectKeyRequired = errors.New("object key is required")
	ErrFilenameRequired  = errors.New("original filename is required")
	ErrContentTypeReq    = errors.New("content type is required")
	ErrInvalidSize       = errors.New("size must be >= 0")
	ErrInvalidStatus     = errors.New("invalid file status")
	ErrFileTooLarge      = errors.New("file exceeds bucket max size")
	ErrInvalidStatusFlow = errors.New("invalid file status transition")
)

type Status string

const (
	StatusActive   Status = "active"
	StatusDeleting Status = "deleting"
	StatusDeleted  Status = "deleted"
)

func (s Status) IsValid() bool {
	switch s {
	case StatusActive, StatusDeleting, StatusDeleted:
		return true
	}
	return false
}

type File struct {
	ID               uuid.UUID
	BucketID         uuid.UUID
	ObjectKey        string
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	ChecksumSHA256   *string
	ETag             *string
	CacheControl     *string
	Status           Status
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

func NewFile(
	bucketID uuid.UUID,
	objectKey, originalFilename, contentType string,
	sizeBytes int64,
	maxFileSize int64,
) (*File, error) {
	objectKey = strings.TrimSpace(objectKey)
	originalFilename = strings.TrimSpace(originalFilename)
	contentType = strings.TrimSpace(contentType)

	switch {
	case bucketID == uuid.Nil:
		return nil, ErrBucketIDReq
	case objectKey == "":
		return nil, ErrObjectKeyRequired
	case originalFilename == "":
		return nil, ErrFilenameRequired
	case contentType == "":
		return nil, ErrContentTypeReq
	case sizeBytes < 0:
		return nil, ErrInvalidSize
	case maxFileSize > 0 && sizeBytes > maxFileSize:
		return nil, ErrFileTooLarge
	}

	now := time.Now().UTC()
	return &File{
		ID:               uuid.New(),
		BucketID:         bucketID,
		ObjectKey:        objectKey,
		OriginalFilename: originalFilename,
		ContentType:      contentType,
		SizeBytes:        sizeBytes,
		Status:           StatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (f *File) SetChecksum(hex string)    { f.ChecksumSHA256 = &hex; f.UpdatedAt = time.Now().UTC() }
func (f *File) SetETag(tag string)        { f.ETag = &tag; f.UpdatedAt = time.Now().UTC() }
func (f *File) SetCacheControl(cc string) { f.CacheControl = &cc; f.UpdatedAt = time.Now().UTC() }

func (f *File) MarkDeleting() error {
	if f.Status != StatusActive {
		return ErrInvalidStatusFlow
	}
	f.Status = StatusDeleting
	f.UpdatedAt = time.Now().UTC()
	return nil
}

func (f *File) MarkDeleted() error {
	if f.Status != StatusDeleting && f.Status != StatusActive {
		return ErrInvalidStatusFlow
	}
	now := time.Now().UTC()
	f.Status = StatusDeleted
	f.DeletedAt = &now
	f.UpdatedAt = now
	return nil
}

func (f *File) Sanitized() FileResponse {
	return FileResponse{
		ID:               f.ID,
		BucketID:         f.BucketID,
		ObjectKey:        f.ObjectKey,
		OriginalFilename: f.OriginalFilename,
		ContentType:      f.ContentType,
		SizeBytes:        f.SizeBytes,
		ChecksumSHA256:   f.ChecksumSHA256,
		ETag:             f.ETag,
		CacheControl:     f.CacheControl,
		Status:           string(f.Status),
		CreatedAt:        f.CreatedAt,
		UpdatedAt:        f.UpdatedAt,
	}
}

type FileResponse struct {
	ID               uuid.UUID `json:"id"`
	BucketID         uuid.UUID `json:"bucketId"`
	ObjectKey        string    `json:"objectKey"`
	OriginalFilename string    `json:"originalFilename"`
	ContentType      string    `json:"contentType"`
	SizeBytes        int64     `json:"sizeBytes"`
	ChecksumSHA256   *string   `json:"checksumSha256,omitempty"`
	ETag             *string   `json:"etag,omitempty"`
	CacheControl     *string   `json:"cacheControl,omitempty"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
