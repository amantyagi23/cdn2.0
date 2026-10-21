package config

import (
	"os"
	"strconv"
)

type Config struct {
	Production bool

	AppName   string
	Host      string
	Port      string
	APIPrefix string

	LogLevel string

	JWTSecret   string
	TokenHeader string

	// Redis
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// Postgres
	PSQLUser     string
	PSQLPassword string
	PSQLDatabase string
	PSQLHost     string
	PSQLPort     string

	// NATS
	NATSURL         string
	NATSMonitorPort string

	StorageSigningSecret string
	StoragePublicBaseURL string
	// External
	BackendURL    string
	PublicBaseURL string // base URL for public CDN file links, e.g. "https://cdn.example.com"

	// CDNLocalDomain is the domain used as a fallback when the request
	// host is localhost or a bare IP (dev / smoke-test mode).
	// Leave empty to disable the fallback.
	CDNLocalDomain string

	// SMTP
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
}

func Load() *Config {
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))

	return &Config{
		Production: getEnv("APP_ENV", "development") == "production",

		AppName:   getEnv("APP_NAME", "API Platform"),
		Host:      getEnv("APP_HOST", "0.0.0.0"),
		Port:      getEnv("APP_PORT", "4500"),
		APIPrefix: getEnv("API_PREFIX", "/api/v1"),

		LogLevel: getEnv("LOG_LEVEL", "debug"),

		JWTSecret:   getEnv("JWT_SECRET", ""),
		TokenHeader: getEnv("TOKEN_HEADER", "Authorization"),

		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       redisDB,

		PSQLUser:     getEnv("POSTGRES_USER", ""),
		PSQLPassword: getEnv("POSTGRES_PASSWORD", ""),
		PSQLDatabase: getEnv("POSTGRES_DB", ""),
		PSQLHost:     getEnv("POSTGRES_HOST", "localhost"),
		PSQLPort:     getEnv("POSTGRES_PORT", "5432"),

		NATSURL:         getEnv("NATS_URL", "nats://nats:4222"),
		NATSMonitorPort: getEnv("NATS_MONITOR_PORT", "8222"),

		StorageSigningSecret: getEnv("STORAGE_SIGNING_SECRET", ""),
		StoragePublicBaseURL: getEnv("STORAGE_PUBLIC_BASE_URL", ""),

		BackendURL:     getEnv("BACKEND_URL", "http://localhost:4500/api/v1"),
		PublicBaseURL:  getEnv("PUBLIC_BASE_URL", "http://localhost:4500"),
		CDNLocalDomain: getEnv("CDN_LOCAL_DOMAIN", ""),

		SMTPHost:     getEnv("SMTP_HOST", ""),
		SMTPPort:     getEnv("SMTP_PORT", "587"),
		SMTPUsername: getEnv("SMTP_USERNAME", ""),
		SMTPPassword: getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:     getEnv("SMTP_FROM", ""),
	}
}

func getEnv(key string, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
