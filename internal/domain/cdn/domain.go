package cdn

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrDomainNotFound         = errors.New("cdn domain not found")
	ErrHostnameTaken          = errors.New("hostname already registered")
	ErrHostnameRequired       = errors.New("hostname is required")
	ErrHostnameTooLong        = errors.New("hostname exceeds 253 characters")
	ErrInvalidHostname        = errors.New("invalid hostname")
	ErrInvalidStatus          = errors.New("invalid cdn domain status")
	ErrBucketIDReq            = errors.New("bucket id is required")
	ErrInvalidStatusFlow      = errors.New("invalid status transition")
	ErrDomainNotPendingVerify = errors.New("only pending domains can be verified")
)

var hostnamePattern = regexp.MustCompile(`^(?i)[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

type DomainStatus string

const (
	StatusPending  DomainStatus = "pending"
	StatusActive   DomainStatus = "active"
	StatusDisabled DomainStatus = "disabled"
	StatusFailed   DomainStatus = "failed"
)

func (s DomainStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusActive, StatusDisabled, StatusFailed:
		return true
	}
	return false
}

type Domain struct {
	ID                uuid.UUID
	BucketID          uuid.UUID
	Hostname          string
	Status            DomainStatus
	VerificationToken string
	VerifiedAt        *time.Time
	TLSEnabled        bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func NewDomain(bucketID uuid.UUID, hostname string, tls bool) (*Domain, error) {
	hostname = strings.ToLower(strings.TrimSpace(hostname))

	switch {
	case bucketID == uuid.Nil:
		return nil, ErrBucketIDReq
	case hostname == "":
		return nil, ErrHostnameRequired
	case len(hostname) > 253:
		return nil, ErrHostnameTooLong
	case !hostnamePattern.MatchString(hostname):
		return nil, ErrInvalidHostname
	}

	tok, err := randomToken(32)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	return &Domain{
		ID:                uuid.New(),
		BucketID:          bucketID,
		Hostname:          hostname,
		Status:            StatusPending,
		VerificationToken: tok,
		TLSEnabled:        tls,
		CreatedAt:         now,
		UpdatedAt:         now,
	}, nil
}

func (d *Domain) Verify() error {
	if d.Status != StatusPending {
		return ErrDomainNotPendingVerify
	}
	now := time.Now().UTC()
	d.Status = StatusActive
	d.VerifiedAt = &now
	d.UpdatedAt = now
	return nil
}

func (d *Domain) MarkFailed() error {
	if d.Status == StatusActive {
		return ErrInvalidStatusFlow
	}
	d.Status = StatusFailed
	d.UpdatedAt = time.Now().UTC()
	return nil
}

func (d *Domain) Disable() error {
	if d.Status != StatusActive {
		return ErrInvalidStatusFlow
	}
	d.Status = StatusDisabled
	d.UpdatedAt = time.Now().UTC()
	return nil
}

func (d *Domain) Enable() error {
	if d.Status != StatusDisabled {
		return ErrInvalidStatusFlow
	}
	d.Status = StatusActive
	d.UpdatedAt = time.Now().UTC()
	return nil
}

func (d *Domain) Sanitized() DomainResponse {
	return DomainResponse{
		ID:         d.ID,
		BucketID:   d.BucketID,
		Hostname:   d.Hostname,
		Status:     string(d.Status),
		VerifiedAt: d.VerifiedAt,
		TLSEnabled: d.TLSEnabled,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
	}
}

type DomainResponse struct {
	ID         uuid.UUID  `json:"id"`
	BucketID   uuid.UUID  `json:"bucketId"`
	Hostname   string     `json:"hostname"`
	Status     string     `json:"status"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
	TLSEnabled bool       `json:"tlsEnabled"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	// VerificationToken is only returned once, at creation.
	VerificationToken string `json:"verificationToken,omitempty"`
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
