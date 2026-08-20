package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL                   string
	Port                          string
	ScholaroscopeWebhookSecret    string
	ScholaroscopeAllowedTimestamp string
	PortalPublicURL               string
	ScholaroscopeWebhookURL       string
	PortalSessionDuration         time.Duration
	PortalCookieSecure            bool
	CORSAllowedOrigins            []string
}

func Load() (*Config, error) {
	dbURL := os.Getenv("TEMPORAL_DATABASE_URL")
	if dbURL == "" {
		// fallback for local dev
		dbURL = fmt.Sprintf(
			"postgres://%s:%s@%s:%s/%s?sslmode=disable",
			getEnv("DB_USER", "temporal_user"),
			getEnv("DB_PASSWORD", "root"),
			getEnv("DB_HOST", "localhost"),
			getEnv("DB_PORT", "5432"),
			getEnv("DB_NAME", "temporal_service"),
		)
	}
	portalSessionMinutes := getEnvInt("TEMPORAL_PORTAL_SESSION_MINUTES", 480)
	if portalSessionMinutes <= 0 {
		return nil, fmt.Errorf("TEMPORAL_PORTAL_SESSION_MINUTES must be positive")
	}
	return &Config{
		DatabaseURL:                   dbURL,
		Port:                          getEnv("PORT", "8081"),
		ScholaroscopeWebhookSecret:    os.Getenv("TEMPORAL_SCHOLAROSCOPE_WEBHOOK_SECRET"),
		ScholaroscopeAllowedTimestamp: getEnv("TEMPORAL_WEBHOOK_ALLOWED_SKEW_SECONDS", "300"),
		PortalPublicURL:               getEnv("TEMPORAL_PORTAL_PUBLIC_URL", "http://localhost:3000"),
		ScholaroscopeWebhookURL:       os.Getenv("SCHOLAROSCOPE_TIMETABLE_WEBHOOK_URL"),
		PortalSessionDuration:         time.Duration(portalSessionMinutes) * time.Minute,
		PortalCookieSecure:            getEnvBool("TEMPORAL_PORTAL_COOKIE_SECURE", !strings.HasPrefix(getEnv("TEMPORAL_PORTAL_PUBLIC_URL", "http://localhost:3000"), "http://")),
		CORSAllowedOrigins:            splitCSV(getEnv("TEMPORAL_CORS_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:3001,http://127.0.0.1:3000")),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.Atoi(v)
		if err == nil {
			return parsed
		}
		return -1
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
