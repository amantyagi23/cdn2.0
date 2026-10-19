package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrAPIKeyNotFound = errors.New("api key not found")
	ErrAPIKeyRevoked  = errors.New("api key is revoked")
	ErrAPIKeyExpired  = errors.New("api key has expired")
	ErrNameRequired   = errors.New("api key name is required")
	ErrNameTooLong    = errors.New("api key name exceeds 120 characters")
)

const (
	rawKeyPrefix = "ak_"
	rawKeyBytes  = 32 // 32 bytes → ~43 base64url chars → ~46 with prefix
	displayLen   = 12
)

type Status string

const (
	StatusActive  Status = "active"
	StatusRevoked Status = "revoked"
	StatusExpired Status = "expired"
)

func (s Status) IsValid() bool {
	switch s {
	case StatusActive, StatusRevoked, StatusExpired:
		return true
	}
	return false
}

// APIKey is the credential aggregate. The plaintext key is returned exactly
// once at creation time; only its SHA-256 hash is stored.
type APIKey struct {
	ID          uuid.UUID
	Name        string
	KeyPrefix   string
	KeyHash     string
	Scopes      []string
	Status      Status
	ExpiresAt   *time.Time
	LastUsedAt  *time.Time
	CreatedAt   time.Time
	RevokedAt   *time.Time
}

// NewAPIKey generates a fresh key. Returns the aggregate plus the raw
// plaintext key which MUST be handed to the caller exactly once.
func NewAPIKey(name string, scopes []string, expiresAt *time.Time) (*APIKey, string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return nil, "", ErrNameRequired
	case len(name) > 120:
		return nil, "", ErrNameTooLong
	}

	raw, prefix, hash, err := generateRawKey()
	if err != nil {
		return nil, "", err
	}

	clean := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s != "" {
			clean = append(clean, s)
		}
	}

	return &APIKey{
		ID:        uuid.New(),
		Name:      name,
		KeyPrefix: prefix,
		KeyHash:   hash,
		Scopes:    clean,
		Status:    StatusActive,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
	}, raw, nil
}

func (k *APIKey) IsExpired() bool {
	return k.ExpiresAt != nil && time.Now().UTC().After(*k.ExpiresAt)
}

func (k *APIKey) IsActive() bool {
	return k.Status == StatusActive && k.RevokedAt == nil && !k.IsExpired()
}

func (k *APIKey) HasScope(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (k *APIKey) Revoke() error {
	if k.Status == StatusRevoked {
		return ErrAPIKeyRevoked
	}
	now := time.Now().UTC()
	k.Status = StatusRevoked
	k.RevokedAt = &now
	return nil
}

func (k *APIKey) Sanitized(rawKey string) APIKeyResponse {
	return APIKeyResponse{
		ID:         k.ID,
		Name:       k.Name,
		KeyPrefix:  k.KeyPrefix,
		Scopes:     k.Scopes,
		Status:     k.Status,
		ExpiresAt:  k.ExpiresAt,
		LastUsedAt: k.LastUsedAt,
		CreatedAt:  k.CreatedAt,
		RevokedAt:  k.RevokedAt,
		RawKey:     rawKey, // only ever set on creation
	}
}

// HashRawKey deterministically hashes a plaintext key for lookup.
func HashRawKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func generateRawKey() (raw, prefix, hash string, err error) {
	b := make([]byte, rawKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", "", "", err
	}
	raw = rawKeyPrefix + base64.RawURLEncoding.EncodeToString(b)
	prefix = raw[:displayLen]
	hash = HashRawKey(raw)
	return raw, prefix, hash, nil
}

type APIKeyResponse struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"keyPrefix"`
	Scopes     []string   `json:"scopes"`
	Status     Status     `json:"status"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	RawKey     string     `json:"rawKey,omitempty"` // present only on creation
}
