// Package response provides standardized HTTP response helpers shared
// across all services. This keeps API contracts consistent shadowops-wide.
package response

import "github.com/gofiber/fiber/v2"

// Envelope is the standard API response shape.
type Envelope struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   *APIError   `json:"error,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

// APIError carries a machine-readable code and a human-readable message.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Meta holds optional pagination or request metadata.
type Meta struct {
	RequestID string `json:"request_id,omitempty"`
}

// OK sends a 200 JSON response with data.
func OK(c *fiber.Ctx, message string, data interface{}) error {
	return c.Status(fiber.StatusOK).JSON(Envelope{
		Success: true,
		Message: message,
		Data:    data,
		// Meta:    &Meta{RequestID: c.Locals("requestid").(string)},
	})
}

// Created sends a 201 JSON response with data.
func Created(c *fiber.Ctx, message string, data interface{}) error {
	return c.Status(fiber.StatusCreated).JSON(Envelope{
		Success: true,
		Message: message,
		Data:    data,
		// Meta:    &Meta{RequestID: c.Locals("requestid").(string)},
	})
}

// BadRequest sends a 400 with a descriptive error.
func BadRequest(c *fiber.Ctx, code, message string) error {
	return c.Status(fiber.StatusBadRequest).JSON(Envelope{
		Success: false,
		Error:   &APIError{Code: code, Message: message},
	})
}

// Forbidden sends a 403. Use this when the caller is authenticated but not
// authorized to perform the requested action (e.g. a non-superadmin
// operator hitting an endpoint reserved for superadmins).
func Forbidden(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusForbidden).JSON(Envelope{
		Success: false,
		Error:   &APIError{Code: "FORBIDDEN", Message: message},
	})
}

// NotFound sends a 404.
func NotFound(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusNotFound).JSON(Envelope{
		Success: false,
		Error:   &APIError{Code: "NOT_FOUND", Message: message},
	})
}

// InternalError sends a 500 without leaking internal details.
func InternalError(c *fiber.Ctx) error {
	return c.Status(fiber.StatusInternalServerError).JSON(Envelope{
		Success: false,
		Error:   &APIError{Code: "INTERNAL_ERROR", Message: "an unexpected error occurred"},
	})
}

// Conflict sends a 409.
func Conflict(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusConflict).JSON(Envelope{
		Success: false,
		Error:   &APIError{Code: "CONFLICT", Message: message},
	})
}
func Unauthorized(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusUnauthorized).JSON(Envelope{
		Success: false,
		Error:   &APIError{Code: "UNAUTHORIZED", Message: message},
	})
}

// TooManyRequests sends a 429.
func TooManyRequests(c *fiber.Ctx, message string) error {
	return c.Status(fiber.StatusTooManyRequests).JSON(Envelope{
		Success: false,
		Error:   &APIError{Code: "RATE_LIMIT_EXCEEDED", Message: message},
	})
}
