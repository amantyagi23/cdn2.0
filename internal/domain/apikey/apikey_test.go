package apikey_test

import (
	"testing"
	"time"

	"github.com/amantyagi23/cdn-network/internal/domain/apikey"
)

func TestNewAPIKey_Valid(t *testing.T) {
	k, raw, err := apikey.NewAPIKey("My Key", []string{"read", "write"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if raw == "" {
		t.Error("raw key should not be empty")
	}
	if k.KeyHash == raw {
		t.Error("stored key hash must not equal raw key")
	}
	if k.KeyHash == "" {
		t.Error("key hash should be set")
	}
	if len(k.Scopes) != 2 {
		t.Errorf("expected 2 scopes, got %d", len(k.Scopes))
	}
}

func TestNewAPIKey_NameRequired(t *testing.T) {
	_, _, err := apikey.NewAPIKey("", nil, nil)
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestAPIKey_Revoke(t *testing.T) {
	k, _, _ := apikey.NewAPIKey("Key", nil, nil)
	if err := k.Revoke(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k.Status != apikey.StatusRevoked {
		t.Errorf("expected revoked status, got %s", k.Status)
	}
}

func TestAPIKey_Revoke_AlreadyRevoked(t *testing.T) {
	k, _, _ := apikey.NewAPIKey("Key", nil, nil)
	_ = k.Revoke()
	if err := k.Revoke(); err == nil {
		t.Fatal("expected error revoking an already-revoked key")
	}
}

func TestAPIKey_IsExpired(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	k, _, _ := apikey.NewAPIKey("Key", nil, &past)
	if !k.IsExpired() {
		t.Error("key with past expiry should be expired")
	}
}

func TestAPIKey_IsActive_Expired(t *testing.T) {
	past := time.Now().Add(-1 * time.Hour)
	k, _, _ := apikey.NewAPIKey("Key", nil, &past)
	if k.IsActive() {
		t.Error("expired key should not be active")
	}
}

func TestAPIKey_IsActive_Revoked(t *testing.T) {
	k, _, _ := apikey.NewAPIKey("Key", nil, nil)
	_ = k.Revoke()
	if k.IsActive() {
		t.Error("revoked key should not be active")
	}
}

func TestAPIKey_HasScope(t *testing.T) {
	k, _, _ := apikey.NewAPIKey("Key", []string{"read", "write"}, nil)
	if !k.HasScope("read") {
		t.Error("should have scope 'read'")
	}
	if k.HasScope("delete") {
		t.Error("should not have scope 'delete'")
	}
}

func TestAPIKey_HashRawKey(t *testing.T) {
	h1 := apikey.HashRawKey("mykey")
	h2 := apikey.HashRawKey("mykey")
	if h1 != h2 {
		t.Error("same input must produce same hash")
	}
	if apikey.HashRawKey("other") == h1 {
		t.Error("different inputs must produce different hashes")
	}
}
