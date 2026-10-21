package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/amantyagi23/cdn-network/internal/config"
	"github.com/amantyagi23/cdn-network/internal/health"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/database"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/logger"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/nats"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/redis"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/storage"
	"github.com/amantyagi23/cdn-network/internal/routes"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	// Load .env when running locally.
	// In Docker, environment variables are provided by the container.
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	cfg := config.Load()

	// --------------------------------------------------
	// Logger
	// --------------------------------------------------

	log, syncLog, err := logger.New(
		cfg.AppName,
		cfg.LogLevel,
		cfg.Production,
	)
	if err != nil {
		panic(err)
	}

	defer syncLog()

	log.Info(
		"CDN Network Starting",
		zap.String("app", cfg.AppName),
		zap.String("port", cfg.Port),
		zap.Bool("production", cfg.Production),
	)

	// --------------------------------------------------
	// Context
	// --------------------------------------------------

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --------------------------------------------------
	// Production startup validation
	// --------------------------------------------------

	if cfg.Production {
		if cfg.PSQLUser == "" || cfg.PSQLPassword == "" || cfg.PSQLDatabase == "" {
			log.Fatal("PostgreSQL credentials are required in production")
		}
		if cfg.StoragePublicBaseURL == "" || cfg.StorageSigningSecret == "" {
			log.Fatal("Storage credentials are required in production")
		}
	}

	// --------------------------------------------------
	// PostgreSQL
	// --------------------------------------------------

	databaseURL := "postgresql://" +
		cfg.PSQLUser + ":" +
		cfg.PSQLPassword + "@" +
		cfg.PSQLHost + ":" +
		cfg.PSQLPort + "/" +
		cfg.PSQLDatabase

	psqlDBCfg := database.DefaultConfig(databaseURL)

	psqlClient, err := database.Connect(
		ctx,
		psqlDBCfg,
		log,
	)
	if err != nil {
		log.Fatal(
			"PostgreSQL connection failed",
			zap.Error(err),
		)
	}

	defer psqlClient.Close()

	// --------------------------------------------------
	// Redis
	// --------------------------------------------------

	redisClient, err := redis.NewRedisClient(
		context.Background(),
		redis.Config{
			Host:     cfg.RedisHost,
			Port:     cfg.RedisPort,
			Password: cfg.RedisPassword,
			DB:       cfg.RedisDB,
		},
		log,
	)
	if err != nil {
		log.Fatal("Redis connection failed", zap.Error(err))
	}
	defer redisClient.Close()

	// --------------------------------------------------
	// NATS
	// --------------------------------------------------

	natsClient, err := nats.NewClient(
		nats.Config{
			URL:  cfg.NATSURL,
			Name: cfg.AppName,
		},
		log,
	)
	if err != nil {
		log.Fatal("NATS connection failed", zap.Error(err))
	}
	defer natsClient.Close()

	storageClient, err := storage.NewClient(storage.Config{
		BasePath:      "document", // auto-created if missing
		SigningSecret: cfg.StorageSigningSecret,
		PublicBaseURL: cfg.StoragePublicBaseURL, // e.g. "https://cdn.example.com/files"
	}, log)
	if err != nil {
		log.Fatal("storage init failed", zap.Error(err))
	}
	defer storageClient.Close()

	// --------------------------------------------------
	// Fiber
	// --------------------------------------------------

	// trustedProxies is the list of upstream proxy CIDRs/IPs that are
	// allowed to set X-Forwarded-* and X-Real-IP headers.  Override via
	// TRUSTED_PROXIES (comma-separated).  An empty value means no proxy
	// is trusted and Fiber will use the direct peer address.
	var trustedProxies []string
	if raw := os.Getenv("TRUSTED_PROXIES"); raw != "" {
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p != "" {
				trustedProxies = append(trustedProxies, p)
			}
		}
	}

	app := fiber.New(fiber.Config{
		AppName:      cfg.AppName,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		// 10 MiB global body limit; individual upload routes enforce their own.
		BodyLimit:             10 * 1024 * 1024,
		DisableStartupMessage: true,

		// Trusted proxy configuration.  When trustedProxies is empty Fiber
		// treats every connection as direct and never reads X-Forwarded-* or
		// X-Real-IP.
		EnableTrustedProxyCheck: len(trustedProxies) > 0,
		TrustedProxies:          trustedProxies,
		// Only read X-Forwarded-For when a trusted proxy list is configured.
		ProxyHeader: func() string {
			if len(trustedProxies) > 0 {
				return fiber.HeaderXForwardedFor
			}
			return ""
		}(),

		ErrorHandler: func(c *fiber.Ctx, err error) error {
			log.Error(
				"unhandled fiber error",
				zap.Error(err),
			)

			code := fiber.StatusInternalServerError
			msg := "an unexpected error occurred"

			if fiberErr, ok := err.(*fiber.Error); ok {
				code = fiberErr.Code
				// Only expose the message for client errors (4xx).
				// Never leak internal detail for 5xx.
				if code < 500 {
					msg = fiberErr.Message
				}
			}

			return c.Status(code).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "FIBER_ERROR",
					"message": msg,
				},
			})
		},
	})

	// --------------------------------------------------
	// Middleware
	// --------------------------------------------------

	// Recovery must be first so it catches panics from all other middleware.
	app.Use(recover.New(recover.Config{
		// Stack traces go to the logger; never expose them in the response.
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, e interface{}) {
			log.Error("panic recovered",
				zap.String("method", c.Method()),
				zap.String("path", c.Path()),
				zap.Any("panic", e),
			)
		},
	}))

	app.Use(requestid.New())

	// Single structured logger — the redundant fiber fiberlogger has been removed.
	app.Use(logger.Middleware(log))

	// Security headers on every response.
	app.Use(func(c *fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Set("X-Permitted-Cross-Domain-Policies", "none")
		return c.Next()
	})

	// CORS: allowlist is read from CORS_ALLOWED_ORIGINS (comma-separated).
	// Falls back to local dev origins when not in production.
	corsOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
	allowCredentials := false
	if corsOrigins == "" {
		if cfg.Production {
			// No frontend origin configured in production — log a warning
			// rather than silently allowing nothing or everything.
			log.Warn("CORS_ALLOWED_ORIGINS is not set in production; no cross-origin requests will be allowed")
			corsOrigins = ""
		} else {
			corsOrigins = "http://localhost:5173,http://localhost:3000,http://localhost:4500"
		}
	}
	// AllowCredentials is only safe when origins are explicitly enumerated
	// (not a wildcard).  Compute this after corsOrigins is finalised.
	if corsOrigins != "" && corsOrigins != "*" {
		allowCredentials = true
	}

	app.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowCredentials: allowCredentials,
	}))

	app.Use(compress.New())

	// --------------------------------------------------
	// Static files
	// --------------------------------------------------

	app.Static("/", "./public", fiber.Static{
		Index:         "index.html",
		Browse:        false,
		CacheDuration: 0,
	})

	// --------------------------------------------------
	// Routes
	// --------------------------------------------------
	healthService := health.NewService(
		cfg.AppName,
		redisClient,
		natsClient,
		log,
	)

	healthHandler := health.NewHandler(
		healthService,
		cfg.BackendURL,
		"1.0.0",
	)

	routes.Register(app, routes.Dependencies{
		HealthHandler: healthHandler,
		Redis:         redisClient,
		Nats:          natsClient,
		DB:            psqlClient,
		Logger:        log,
		Storage:       storageClient,
		Ctx:           ctx,
		Cancel:        cancel,
	}, cfg)

	// --------------------------------------------------
	// Start Server
	// --------------------------------------------------

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- app.Listen(
			cfg.Host + ":" + cfg.Port,
		)
	}()

	log.Info(
		"CDN Network ready",
		zap.String("host", cfg.Host),
		zap.String("port", cfg.Port),
	)

	// --------------------------------------------------
	// Shutdown signals
	// --------------------------------------------------

	quit := make(chan os.Signal, 1)

	signal.Notify(
		quit,
		syscall.SIGINT,
		syscall.SIGTERM,
	)

	select {
	case sig := <-quit:
		log.Info(
			"shutdown signal received",
			zap.String("signal", sig.String()),
		)

	case err := <-serverErr:
		log.Error(
			"server stopped unexpectedly",
			zap.Error(err),
		)
	}

	// --------------------------------------------------
	// Graceful shutdown
	// --------------------------------------------------

	shutdownCtx, shutdownCancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer shutdownCancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Error(
			"graceful shutdown failed",
			zap.Error(err),
		)
	}

	log.Info("CDN Network stopped")
}
