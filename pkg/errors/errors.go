// Package errors defines sentinel domain errors shared across services.
// Usecases return these typed errors; handlers map them to HTTP status codes.
package errors

import "errors"

// Sentinel errors — compare with errors.Is().
var (
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrInternal      = errors.New("internal error")
)

// ValidationError wraps field-level validation failures.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// Is allows errors.Is to match against the base sentinel.
func (e *ValidationError) Is(target error) bool {
	return target == ErrInvalidInput
}
