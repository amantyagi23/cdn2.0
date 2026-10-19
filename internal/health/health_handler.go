package health

import (
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	service     *Service
	environment string
	version     string
}

func NewHandler(
	service *Service,
	environment string,
	version string,
) *Handler {
	return &Handler{
		service:     service,
		environment: environment,
		version:     version,
	}
}

// GET /health
func (h *Handler) Health(c *fiber.Ctx) error {
	result := h.service.Check(
		c.Context(),
		h.environment,
		h.version,
	)

	statusCode := http.StatusOK

	if result.Status == "degraded" {
		statusCode = http.StatusServiceUnavailable
	}

	return c.Status(statusCode).JSON(result)
}

// GET /health/live
func (h *Handler) Live(c *fiber.Ctx) error {
	return c.Status(http.StatusOK).JSON(fiber.Map{
		"status":    "alive",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}

// GET /health/ready
func (h *Handler) Ready(c *fiber.Ctx) error {
	result := h.service.Check(
		c.Context(),
		h.environment,
		h.version,
	)

	if result.Status != "healthy" {
		return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "not_ready",
		})
	}

	return c.Status(http.StatusOK).JSON(fiber.Map{
		"status": "ready",
	})
}
