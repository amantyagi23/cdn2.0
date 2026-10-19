package logger

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

func Middleware(log *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		duration := time.Since(start)

		log.Info(
			"HTTP request",
			zap.String("method", c.Method()),
			zap.String("path", c.Path()),
			zap.String("route", c.Route().Path),
			zap.Int("status", c.Response().StatusCode()),
			zap.Duration("duration", duration),
			zap.String("ip", c.IP()),
			zap.String("user_agent", c.Get("User-Agent")),
		)

		if err != nil {
			log.Error(
				"HTTP request failed",
				zap.String("method", c.Method()),
				zap.String("path", c.Path()),
				zap.Int("status", c.Response().StatusCode()),
				zap.Error(err),
			)
		}

		return err
	}
}
