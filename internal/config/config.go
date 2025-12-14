package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port         string
	DatabaseURL  string
	Environment  string
	LogLevel     string
	MaxBodySize  int64
	CleanupInterval int
	CSRFEnabled  bool
	AllowedOrigins []string
}

var AppConfig *Config

func Load() {
	csrfEnabled := getEnv("CSRF_ENABLED", "true") == "true"
	allowedOriginsStr := getEnv("ALLOWED_ORIGINS", "")
	var allowedOrigins []string
	if allowedOriginsStr != "" {
		allowedOrigins = strings.Split(allowedOriginsStr, ",")
		for i := range allowedOrigins {
			allowedOrigins[i] = strings.TrimSpace(allowedOrigins[i])
		}
	}

	AppConfig = &Config{
		Port:         getEnv("PORT", "8080"),
		DatabaseURL:  getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/flowhook_dev?sslmode=disable"),
		Environment:  getEnv("ENVIRONMENT", "development"),
		LogLevel:     getEnv("LOG_LEVEL", "info"),
		MaxBodySize:  int64(getEnvInt("MAX_BODY_SIZE", 10*1024*1024)), // 10MB default
		CleanupInterval: getEnvInt("CLEANUP_INTERVAL", 60), // 60 minutes default
		CSRFEnabled:  csrfEnabled,
		AllowedOrigins: allowedOrigins,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

