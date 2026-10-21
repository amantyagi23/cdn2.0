package routes

import (
	"context"

	"github.com/amantyagi23/cdn-network/internal/config"
	"github.com/amantyagi23/cdn-network/internal/handlers"
	"github.com/amantyagi23/cdn-network/internal/health"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/nats"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/redis"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/storage"
	"github.com/amantyagi23/cdn-network/internal/middleware"
	postgres "github.com/amantyagi23/cdn-network/internal/repos"
	"github.com/amantyagi23/cdn-network/internal/security/ratelimit"
	"github.com/amantyagi23/cdn-network/internal/usecase"
	"github.com/amantyagi23/cdn-network/internal/workers"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// Dependencies carries everything Register needs to wire up route groups.
type Dependencies struct {
	Ctx           context.Context
	Cancel        context.CancelFunc
	HealthHandler *health.Handler
	DB            *pgxpool.Pool
	Redis         *redis.RedisClient
	Nats          *nats.NATSClient
	Storage       *storage.StorageClient
	Logger        *zap.Logger
}

func Register(fiberApp *fiber.App, deps Dependencies, cfg *config.Config) {
	// Build the rate limiter backed by Redis.
	rlStore := ratelimit.NewRedisStore(deps.Redis.Client())
	limiter := ratelimit.NewLimiter(rlStore, ratelimit.DefaultConfig(), deps.Logger)

	// Repositories
	apikeyRepo := postgres.NewAPIKeyRepository(deps.DB, deps.Logger)
	bucketRepo := postgres.NewBucketRepository(deps.DB, deps.Logger)
	fileRepo := postgres.NewFileRepository(deps.DB, deps.Logger)
	cdnRepo := postgres.NewCDNRepository(deps.DB, deps.Logger)
	usageRepo := postgres.NewUsageRepository(deps.DB, deps.Logger)

	// Usecases
	apikeyUC := usecase.NewAPIKeyUsecase(apikeyRepo, deps.Logger)
	bucketUC := usecase.NewBucketUsecase(bucketRepo, deps.Storage, deps.Logger)
	fileUC := usecase.NewFileUsecase(fileRepo, bucketRepo, deps.Storage, deps.Redis, cfg.PublicBaseURL, deps.Logger)
	cdnUC := usecase.NewCDNUsecase(cdnRepo, bucketRepo, deps.Logger)
	cdnDeliveryUC := usecase.NewCDNDeliveryUsecase(cdnRepo, bucketRepo, deps.Storage, deps.Redis, deps.Logger)
	usageUC := usecase.NewUsageUsecase(usageRepo, deps.Logger)

	// API key management routes — no auth required to create/list/revoke keys
	// (in production you would protect these differently, e.g. an admin secret).
	api := fiberApp.Group(cfg.APIPrefix)

	// Global IP-based rate limit on all API routes.
	api.Use(middleware.RateLimitMiddleware(limiter, ratelimit.KindAPI))
	authenticated := api.Group("", middleware.EnsureAPIKey(apikeyRepo, deps.Logger))

	// API key CRUD — intentionally unauthenticated so clients can bootstrap.
	handlers.NewAPIKeyHandler(apikeyUC, deps.Logger).RegisterRoutes(authenticated)

	// All remaining routes require a valid API key.

	handlers.NewBucketHandler(bucketUC, deps.Logger).RegisterRoutes(authenticated)
	handlers.NewFileHandler(fileUC, deps.Nats, deps.Logger).RegisterRoutes(authenticated)
	handlers.NewCDNHandler(cdnUC, deps.Logger).RegisterRoutes(authenticated)
	handlers.NewUsageHandler(usageUC, deps.Logger).RegisterRoutes(authenticated)

	// CDN delivery — root-mounted so custom hostnames resolve directly.
	// Apply a separate (higher) delivery rate limit before the handler.
	// cfg.CDNLocalDomain is read from CDN_LOCAL_DOMAIN; empty disables the fallback.
	cdnGroup := fiberApp.Group("/cdn")
	cdnGroup.Use(middleware.RateLimitMiddleware(limiter, ratelimit.KindDelivery))
	handlers.NewCDNDeliveryHandler(cdnDeliveryUC, deps.Logger, handlers.WithLocalDomain(cfg.CDNLocalDomain)).RegisterRoutes(fiberApp)

	registerHealthRoutes(authenticated, deps)

	// Background workers.
	fileWorker := workers.NewFileCleanupWorker(
		deps.Nats, deps.Storage, fileRepo, bucketRepo, deps.Logger,
	)
	go func() {
		if err := fileWorker.Start(deps.Ctx); err != nil {
			deps.Logger.Error("FileCleanupWorker exited", zap.Error(err))
		}
	}()
}

func registerHealthRoutes(api fiber.Router, deps Dependencies) {
	healthRoutes := api.Group("/health")
	healthRoutes.Get("/", deps.HealthHandler.Health)
	healthRoutes.Get("/live", deps.HealthHandler.Live)
	healthRoutes.Get("/ready", deps.HealthHandler.Ready)
}
