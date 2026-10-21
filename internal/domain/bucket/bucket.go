package bucket

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound           = errors.New("bucket not found")
	ErrBucketNameTaken    = errors.New("bucket name already in use for this API key")
	ErrInvalidName        = errors.New("bucket name must be 3-63 chars, lowercase alphanumeric with hyphens")
	ErrInvalidStorageName = errors.New("invalid storage bucket name")
	ErrInvalidMaxSize     = errors.New("max file size must be greater than zero")
	ErrAPIKeyIDRequired   = errors.New("api key id is required")
	ErrInvalidVisibility  = errors.New("invalid bucket visibility")
)

var bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

type Visibility string

const (
	VisibilityPrivate Visibility = "private"
	VisibilityPublic  Visibility = "public"
)

func (v Visibility) IsValid() bool {
	return v == VisibilityPrivate || v == VisibilityPublic
}

func ParseVisibility(s string) (Visibility, error) {
	v := Visibility(s)
	if !v.IsValid() {
		return "", ErrInvalidVisibility
	}
	return v, nil
}

type Bucket struct {
	ID               uuid.UUID
	APIKeyID         uuid.UUID
	Name             string
	StorageName      string
	Visibility       Visibility
	MaxFileSizeBytes int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

func NewBucket(
	apiKeyID uuid.UUID,
	name, storageName string,
	visibility Visibility,
	maxFileSize int64,
) (*Bucket, error) {
	name = strings.ToLower(strings.TrimSpace(name))

	switch {
	case apiKeyID == uuid.Nil:
		return nil, ErrAPIKeyIDRequired
	case !bucketNamePattern.MatchString(name):
		return nil, ErrInvalidName
	case storageName == "":
		return nil, ErrInvalidStorageName
	case !visibility.IsValid():
		return nil, ErrInvalidVisibility
	case maxFileSize <= 0:
		return nil, ErrInvalidMaxSize
	}

	now := time.Now().UTC()
	return &Bucket{
		ID:               uuid.New(),
		APIKeyID:         apiKeyID,
		Name:             name,
		StorageName:      storageName,
		Visibility:       visibility,
		MaxFileSizeBytes: maxFileSize,
		CreatedAt:        now,
		UpdatedAt:        now,
	}, nil
}

func (b *Bucket) SetVisibility(v Visibility) error {
	if !v.IsValid() {
		return ErrInvalidVisibility
	}
	b.Visibility = v
	b.UpdatedAt = time.Now().UTC()
	return nil
}

func (b *Bucket) SetMaxFileSize(n int64) error {
	if n <= 0 {
		return ErrInvalidMaxSize
	}
	b.MaxFileSizeBytes = n
	b.UpdatedAt = time.Now().UTC()
	return nil
}

// MarkDeleted soft-deletes and frees the per-key unique name slot.
func (b *Bucket) MarkDeleted() {
	now := time.Now().UTC()
	b.DeletedAt = &now
	b.Name = b.Name + "__deleted__" + now.Format("20060102150405")
}

func (b *Bucket) Sanitized() BucketResponse {
	return BucketResponse{
		ID:               b.ID,
		Name:             b.Name,
		Visibility:       string(b.Visibility),
		MaxFileSizeBytes: b.MaxFileSizeBytes,
		CreatedAt:        b.CreatedAt,
		UpdatedAt:        b.UpdatedAt,
	}
}

type BucketResponse struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	Visibility       string    `json:"visibility"`
	MaxFileSizeBytes int64     `json:"maxFileSizeBytes"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}
