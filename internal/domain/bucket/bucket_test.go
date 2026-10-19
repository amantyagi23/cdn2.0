package bucket_test

import (
	"testing"

	"github.com/amantyagi23/cdn-network/internal/domain/bucket"
	"github.com/google/uuid"
)

func TestNewBucket_Valid(t *testing.T) {
	b, err := bucket.NewBucket(
		uuid.New(),
		"my-assets", "apikey-my-assets",
		bucket.VisibilityPrivate, 52428800,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.ID == uuid.Nil {
		t.Error("bucket ID should be set")
	}
	if b.Visibility != bucket.VisibilityPrivate {
		t.Errorf("visibility mismatch")
	}
}

func TestNewBucket_InvalidName(t *testing.T) {
	// NewBucket lowercases the name, so only test inputs that remain invalid.
	tests := []string{
		"ab",             // too short
		"has space",      // space
		"has_underscore", // underscore
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := bucket.NewBucket(
				uuid.New(),
				name, "minio-bucket",
				bucket.VisibilityPrivate, 52428800,
			)
			if err == nil {
				t.Errorf("expected error for bucket name %q", name)
			}
		})
	}
}

func TestNewBucket_APIKeyIDRequired(t *testing.T) {
	_, err := bucket.NewBucket(uuid.Nil, "my-bucket", "minio", bucket.VisibilityPrivate, 1024)
	if err == nil {
		t.Fatal("expected error for nil api key ID")
	}
}

func TestNewBucket_MaxSizeMustBePositive(t *testing.T) {
	_, err := bucket.NewBucket(uuid.New(), "my-bucket", "minio", bucket.VisibilityPrivate, 0)
	if err == nil {
		t.Fatal("expected error for zero max file size")
	}
}

func TestBucket_SetVisibility(t *testing.T) {
	b, _ := bucket.NewBucket(uuid.New(), "my-assets", "minio", bucket.VisibilityPrivate, 1024)
	if err := b.SetVisibility(bucket.VisibilityPublic); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Visibility != bucket.VisibilityPublic {
		t.Error("visibility should be public")
	}
}

func TestBucket_SetVisibility_Invalid(t *testing.T) {
	b, _ := bucket.NewBucket(uuid.New(), "my-assets", "minio", bucket.VisibilityPrivate, 1024)
	if err := b.SetVisibility("unknown"); err == nil {
		t.Fatal("expected error for invalid visibility")
	}
}

func TestBucket_SetMaxFileSize(t *testing.T) {
	b, _ := bucket.NewBucket(uuid.New(), "my-assets", "minio", bucket.VisibilityPrivate, 1024)
	if err := b.SetMaxFileSize(2048); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.MaxFileSizeBytes != 2048 {
		t.Errorf("max file size not updated")
	}
}

func TestBucket_MarkDeleted_FreesName(t *testing.T) {
	b, _ := bucket.NewBucket(uuid.New(), "my-assets", "minio", bucket.VisibilityPrivate, 1024)
	originalName := b.Name
	b.MarkDeleted()
	if b.Name == originalName {
		t.Error("bucket name should be altered after deletion to free the slot")
	}
	if b.DeletedAt == nil {
		t.Error("DeletedAt should be set after deletion")
	}
}
