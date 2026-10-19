package health

import (
	"context"
	"runtime"
	"time"

	"github.com/amantyagi23/cdn-network/internal/infrastructure/nats"
	"github.com/amantyagi23/cdn-network/internal/infrastructure/redis"
	"go.uber.org/zap"
)

type Service struct {
	appName   string
	startedAt time.Time

	redis *redis.RedisClient
	nats  *nats.NATSClient
	log   *zap.Logger
}

func NewService(
	appName string,
	redisClient *redis.RedisClient,
	natsClient *nats.NATSClient,
	log *zap.Logger,
) *Service {
	return &Service{
		appName:   appName,
		startedAt: time.Now(),
		redis:     redisClient,
		nats:      natsClient,
		log:       log,
	}
}

type DependencyStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type HealthResponse struct {
	Status      string `json:"status"`
	Application string `json:"application"`
	Version     string `json:"version"`
	Environment string `json:"environment"`

	Uptime        string `json:"uptime"`
	UptimeSeconds int64  `json:"uptimeSeconds"`

	GoVersion    string `json:"goVersion"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	NumCPU       int    `json:"numCPU"`
	NumGoroutine int    `json:"numGoroutine"`

	Dependencies map[string]DependencyStatus `json:"dependencies"`

	Timestamp string `json:"timestamp"`
}

func (s *Service) Check(
	ctx context.Context,
	environment string,
	version string,
) HealthResponse {
	uptime := time.Since(s.startedAt)

	redisStatus := DependencyStatus{
		Status: "up",
	}

	if s.redis != nil {
		if err := s.redis.Ping(ctx); err != nil {
			redisStatus = DependencyStatus{
				Status:  "down",
				Message: err.Error(),
			}
		}
	} else {
		redisStatus = DependencyStatus{
			Status:  "down",
			Message: "Redis client is not initialized",
		}
	}

	natsStatus := DependencyStatus{
		Status: "up",
	}

	if s.nats != nil {
		if err := s.nats.Ping(); err != nil {
			natsStatus = DependencyStatus{
				Status:  "down",
				Message: err.Error(),
			}
		}
	} else {
		natsStatus = DependencyStatus{
			Status:  "down",
			Message: "NATS client is not initialized",
		}
	}

	status := "healthy"

	if redisStatus.Status != "up" || natsStatus.Status != "up" {
		status = "degraded"
	}

	return HealthResponse{
		Status:        status,
		Application:   s.appName,
		Version:       version,
		Environment:   environment,
		Uptime:        uptime.Round(time.Second).String(),
		UptimeSeconds: int64(uptime.Seconds()),

		GoVersion:    runtime.Version(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		NumGoroutine: runtime.NumGoroutine(),

		Dependencies: map[string]DependencyStatus{
			"redis": redisStatus,
			"nats":  natsStatus,
		},

		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}
