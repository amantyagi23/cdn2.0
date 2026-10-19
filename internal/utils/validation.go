package utils

import (
	"fmt"

	"github.com/google/uuid"
)

func ValidateUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid UUID: %w", err)
	}

	return id, nil
}

func CheckEquality(value1 string, value2 string) bool {
	return value1 == value2
}
