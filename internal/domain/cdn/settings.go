package cdn

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrSettingsNotFound    = errors.New("cdn settings not found")
	ErrNegativeTTL         = errors.New("ttl must be >= 0")
	ErrMaxLessThanDefault  = errors.New("max ttl must be >= default ttl")
	ErrBucketIDReqSettings = errors.New("bucket id is required")
)

type Settings struct {
	BucketID           uuid.UUID
	DefaultTTLSeconds  int32
	MaxTTLSeconds      int32
	ForceHTTPS         bool
	CompressionEnabled bool
	UpdatedAt          time.Time
}

func NewSettings(bucketID uuid.UUID) (*Settings, error) {
	if bucketID == uuid.Nil {
		return nil, ErrBucketIDReqSettings
	}
	return &Settings{
		BucketID:           bucketID,
		DefaultTTLSeconds:  3600,
		MaxTTLSeconds:      86400,
		ForceHTTPS:         true,
		CompressionEnabled: true,
		UpdatedAt:          time.Now().UTC(),
	}, nil
}

func (s *Settings) Update(defaultTTL, maxTTL int32, forceHTTPS, compression bool) error {
	if defaultTTL < 0 || maxTTL < 0 {
		return ErrNegativeTTL
	}
	if maxTTL < defaultTTL {
		return ErrMaxLessThanDefault
	}
	s.DefaultTTLSeconds = defaultTTL
	s.MaxTTLSeconds = maxTTL
	s.ForceHTTPS = forceHTTPS
	s.CompressionEnabled = compression
	s.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Settings) Sanitized() SettingsResponse {
	return SettingsResponse{
		BucketID:           s.BucketID,
		DefaultTTLSeconds:  s.DefaultTTLSeconds,
		MaxTTLSeconds:      s.MaxTTLSeconds,
		ForceHTTPS:         s.ForceHTTPS,
		CompressionEnabled: s.CompressionEnabled,
		UpdatedAt:          s.UpdatedAt,
	}
}

type SettingsResponse struct {
	BucketID           uuid.UUID `json:"bucketId"`
	DefaultTTLSeconds  int32     `json:"defaultTtlSeconds"`
	MaxTTLSeconds      int32     `json:"maxTtlSeconds"`
	ForceHTTPS         bool      `json:"forceHttps"`
	CompressionEnabled bool      `json:"compressionEnabled"`
	UpdatedAt          time.Time `json:"updatedAt"`
}
