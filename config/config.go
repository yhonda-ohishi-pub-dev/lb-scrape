package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Server
	Port string

	// Database (PostgreSQL)
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	// Health Check
	HealthCheckCacheTTL time.Duration

	// VPS Auth
	VPSBearerToken string

	// Timeouts
	VPSRequestTimeout time.Duration
}

func Load() *Config {
	return &Config{
		Port: getEnv("PORT", "8080"),

		DBHost:     getEnv("DB_HOST", "localhost"),
		DBPort:     getEnv("DB_PORT", "5432"),
		DBUser:     getEnv("DB_USER", "postgres"),
		DBPassword: getEnv("DB_PASSWORD", ""),
		DBName:     getEnv("DB_NAME", "scraper"),
		DBSSLMode:  getEnv("DB_SSLMODE", "disable"),

		HealthCheckCacheTTL: getDurationEnv("HEALTH_CHECK_CACHE_TTL", 30*time.Second),
		VPSBearerToken:      getEnv("VPS_BEARER_TOKEN", ""),
		VPSRequestTimeout:   getDurationEnv("VPS_REQUEST_TIMEOUT", 55*time.Second),
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
