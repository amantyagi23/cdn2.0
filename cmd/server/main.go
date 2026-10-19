package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/amantyagi23/cdn-network/internal/config"
	"github.com/amantyagi23/cdn-network/internal/health"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/database"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/logger"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/minio"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/nats"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/redis"
	"github.com/amantyagi23/cdn-network/internal/routes"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
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
		if cfg.MinIOAccessKey == "" || cfg.MinIOSecretKey == "" {
			log.Fatal("MinIO credentials are required in production")
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

	minioClient, err := minio.NewClient(minio.Config{
		Endpoint:  cfg.MinIOEndpoint,  // "minio:9000" in compose, "localhost:9000" locally
		AccessKey: cfg.MinIOAccessKey, // fall back to MINIO_ROOT_USER in dev
		SecretKey: cfg.MinIOSecretKey, // fall back to MINIO_ROOT_PASSWORD in dev
		UseSSL:    cfg.MinIOUseSSL,    // false in compose
		Region:    cfg.MinIORegion,    // "" (let server decide) or "us-east-1"
	}, log)
	if err != nil {
		log.Fatal("minio init failed", zap.Error(err))
	}
	defer minioClient.Close()

	// --------------------------------------------------
	// Fiber
	// --------------------------------------------------

	app := fiber.New(fiber.Config{
		AppName:               cfg.AppName,
		ReadTimeout:           10 * time.Second,
		WriteTimeout:          10 * time.Second,
		IdleTimeout:           30 * time.Second,
		DisableStartupMessage: false,

		ErrorHandler: func(c *fiber.Ctx, err error) error {
			log.Error(
				"unhandled fiber error",
				zap.Error(err),
			)

			code := fiber.StatusInternalServerError

			if fiberErr, ok := err.(*fiber.Error); ok {
				code = fiberErr.Code
			}

			return c.Status(code).JSON(fiber.Map{
				"success": false,
				"error": fiber.Map{
					"code":    "FIBER_ERROR",
					"message": err.Error(),
				},
			})
		},
	})

	// --------------------------------------------------
	// Middleware
	// --------------------------------------------------

	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
	}))

	app.Use(compress.New())

	app.Use(requestid.New())

	app.Use(logger.Middleware(log))

	// CORS: allowlist is read from CORS_ALLOWED_ORIGINS (comma-separated).
	// Falls back to local dev origins when not in production.
	corsOrigins := os.Getenv("CORS_ALLOWED_ORIGINS")
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

	app.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
		AllowCredentials: true,
	}))

	app.Use(fiberlogger.New(fiberlogger.Config{
		Format: "[${time}] ${status} - ${method} ${path} | ${latency} | rid=${locals:requestid}\n",
	}))

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
		Minio:         minioClient,
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
