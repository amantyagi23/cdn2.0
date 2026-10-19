package usage

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrEventTypeRequired = errors.New("event type is required")
	ErrQuantityNegative  = errors.New("quantity must be >= 0")
	ErrNotFound          = errors.New("usage event not found")
)

// EventType is a controlled vocabulary.
type EventType string

const (
	EventStorageBytes   EventType = "storage_bytes"
	EventBandwidthBytes EventType = "bandwidth_bytes"
	EventRequests       EventType = "requests"
	EventEgressBytes    EventType = "egress_bytes"
)

func (t EventType) IsValid() bool {
	switch t {
	case EventStorageBytes, EventBandwidthBytes, EventRequests, EventEgressBytes:
		return true
	}
	return false
}

type Event struct {
	ID         int64
	APIKeyID   *uuid.UUID
	BucketID   *uuid.UUID
	EventType  EventType
	Quantity   int64
	OccurredAt time.Time
	Metadata   json.RawMessage
}

func NewEvent(
	apiKeyID *uuid.UUID,
	bucketID *uuid.UUID,
	eventType EventType,
	quantity int64,
	metadata json.RawMessage,
) (*Event, error) {
	if !eventType.IsValid() {
		return nil, ErrEventTypeRequired
	}
	if quantity < 0 {
		return nil, ErrQuantityNegative
	}
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	return &Event{
		APIKeyID:   apiKeyID,
		BucketID:   bucketID,
		EventType:  eventType,
		Quantity:   quantity,
		OccurredAt: time.Now().UTC(),
		Metadata:   metadata,
	}, nil
}

// Summary is an aggregated view of usage events over a window.
type Summary struct {
	EventType string    `json:"eventType"`
	Total     int64     `json:"total"`
	Since     time.Time `json:"since"`
	Until     time.Time `json:"until"`
}

// ParseEventType validates from a raw string.
func ParseEventType(s string) (EventType, error) {
	t := EventType(strings.TrimSpace(s))
	if !t.IsValid() {
		return "", ErrEventTypeRequired
	}
	return t, nil
}
