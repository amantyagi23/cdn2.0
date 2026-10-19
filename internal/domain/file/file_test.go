package file_test

import (
	"testing"

	"github.com/amantyagi23/cdn-network/internal/domain/file"
	"github.com/google/uuid"
)

func TestNewFile_Valid(t *testing.T) {
	f, err := file.NewFile(uuid.New(), "2024/01/abc-photo.jpg", "photo.jpg", "image/jpeg", 1024, 52428800)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.ID == uuid.Nil {
		t.Error("file ID should be set")
	}
	if f.Status != file.StatusActive {
		t.Errorf("expected active status, got %s", f.Status)
	}
}

func TestNewFile_BucketRequired(t *testing.T) {
	_, err := file.NewFile(uuid.Nil, "key", "file.txt", "text/plain", 0, 0)
	if err == nil {
		t.Fatal("expected error for nil bucket ID")
	}
}

func TestNewFile_ObjectKeyRequired(t *testing.T) {
	_, err := file.NewFile(uuid.New(), "", "file.txt", "text/plain", 0, 0)
	if err == nil {
		t.Fatal("expected error for empty object key")
	}
}

func TestNewFile_FilenameRequired(t *testing.T) {
	_, err := file.NewFile(uuid.New(), "key", "", "text/plain", 0, 0)
	if err == nil {
		t.Fatal("expected error for empty filename")
	}
}

func TestNewFile_ContentTypeRequired(t *testing.T) {
	_, err := file.NewFile(uuid.New(), "key", "file.txt", "", 0, 0)
	if err == nil {
		t.Fatal("expected error for empty content type")
	}
}

func TestNewFile_NegativeSizeRejected(t *testing.T) {
	_, err := file.NewFile(uuid.New(), "key", "file.txt", "text/plain", -1, 0)
	if err == nil {
		t.Fatal("expected error for negative size")
	}
}

func TestNewFile_ExceedsMaxSize(t *testing.T) {
	_, err := file.NewFile(uuid.New(), "key", "file.txt", "text/plain", 200, 100)
	if err == nil {
		t.Fatal("expected error when file exceeds max size")
	}
}

func TestFile_StatusFlow(t *testing.T) {
	f, _ := file.NewFile(uuid.New(), "key", "file.txt", "text/plain", 100, 0)

	// active → deleting
	if err := f.MarkDeleting(); err != nil {
		t.Fatalf("MarkDeleting failed: %v", err)
	}
	if f.Status != file.StatusDeleting {
		t.Errorf("expected deleting, got %s", f.Status)
	}

	// deleting → deleted
	if err := f.MarkDeleted(); err != nil {
		t.Fatalf("MarkDeleted failed: %v", err)
	}
	if f.Status != file.StatusDeleted {
		t.Errorf("expected deleted, got %s", f.Status)
	}
}

func TestFile_MarkDeleting_AlreadyDeleting(t *testing.T) {
	f, _ := file.NewFile(uuid.New(), "key", "file.txt", "text/plain", 100, 0)
	_ = f.MarkDeleting()
	if err := f.MarkDeleting(); err == nil {
		t.Fatal("expected error marking deleting file as deleting again")
	}
}

func TestFile_SetChecksum(t *testing.T) {
	f, _ := file.NewFile(uuid.New(), "key", "file.txt", "text/plain", 100, 0)
	f.SetChecksum("abc123")
	if f.ChecksumSHA256 == nil || *f.ChecksumSHA256 != "abc123" {
		t.Error("checksum not set")
	}
}
