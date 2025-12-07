package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Server
	Port     string
	GRPCPort string

	// Database (PostgreSQL)
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	// Cloud SQL
	CloudSQLEnabled  bool
	CloudSQLInstance string // format: project:region:instance

	// Health Check
	HealthCheckCacheTTL time.Duration

	// VPS Auth
	VPSBearerToken string

	// Timeouts
	VPSRequestTimeout time.Duration

	// gRPC-Web
	GRPCWebEnabled     bool
	GRPCWebPort        string
	AllowedOrigins     []string
}

func Load() *Config {
	return &Config{
		Port:     getEnv("PORT", "8080"),
		GRPCPort: getEnv("GRPC_PORT", "50051"),

		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBName:     getEnv("DB_NAME", "myapp"),
		DBSSLMode:  getEnv("DB_SSLMODE", "disable"),

		CloudSQLEnabled:  getBoolEnv("CLOUDSQL_ENABLED", false),
		CloudSQLInstance: getEnv("CLOUDSQL_INSTANCE", "cloudsql-sv:asia-northeast1:postgres-prod"),

		HealthCheckCacheTTL: getDurationEnv("HEALTH_CHECK_CACHE_TTL", 30*time.Second),
		VPSBearerToken:      getEnv("VPS_BEARER_TOKEN", ""),
		VPSRequestTimeout:   getDurationEnv("VPS_REQUEST_TIMEOUT", 55*time.Second),

		GRPCWebEnabled: getBoolEnv("GRPC_WEB_ENABLED", true),
		GRPCWebPort:    getEnv("GRPC_WEB_PORT", "8081"),
		AllowedOrigins: getSliceEnv("ALLOWED_ORIGINS", []string{"*"}),
	}
}

func (c *Config) DSN() string {
	return "postgres://" + c.DBUser + ":" + c.DBPassword + "@" + c.DBHost + ":" + c.DBPort + "/" + c.DBName + "?sslmode=" + c.DBSSLMode
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return defaultValue
}

func getBoolEnv(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1"
	}
	return defaultValue
}

func getSliceEnv(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
