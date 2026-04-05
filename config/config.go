package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL string
	Port        string
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
	return &Config{
		DatabaseURL: dbURL,
		Port:        getEnv("PORT", "8081"),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
