// Package config loads process configuration from the environment. A .env at
// the repo root (or one level up, when run from a subdirectory) is read first;
// real environment variables always win over it.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort  string
	Environment string // "dev" | "prod"

	// PostgresDSN points at the Supabase project (dev) or the production
	// Postgres. Supabase's transaction pooler (port 6543) is detected and
	// handled in internal/database.
	PostgresDSN      string
	PostgresMaxConns int32

	RedisAddr     string
	RedisPassword string

	JWTSecret          string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
}

func Load() (*Config, error) {
	_ = godotenv.Load(".env", "../.env")

	cfg := &Config{
		ServerPort:    getenv("SERVER_PORT", "8080"),
		Environment:   getenv("ENVIRONMENT", "dev"),
		PostgresDSN:   os.Getenv("POSTGRES_DSN"),
		RedisAddr:     getenv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
	}

	maxConns, err := strconv.Atoi(getenv("POSTGRES_MAX_CONNS", "10"))
	if err != nil {
		return nil, fmt.Errorf("POSTGRES_MAX_CONNS: %w", err)
	}
	cfg.PostgresMaxConns = int32(maxConns)

	if cfg.AccessTokenExpiry, err = time.ParseDuration(getenv("JWT_EXPIRY", "15m")); err != nil {
		return nil, fmt.Errorf("JWT_EXPIRY: %w", err)
	}
	if cfg.RefreshTokenExpiry, err = time.ParseDuration(getenv("REFRESH_TOKEN_EXPIRY", "720h")); err != nil {
		return nil, fmt.Errorf("REFRESH_TOKEN_EXPIRY: %w", err)
	}

	if maxConns < 1 || maxConns > 1000 {
		return nil, fmt.Errorf("POSTGRES_MAX_CONNS must be between 1 and 1000")
	}
	if cfg.AccessTokenExpiry <= 0 || cfg.RefreshTokenExpiry <= 0 {
		return nil, fmt.Errorf("JWT_EXPIRY and REFRESH_TOKEN_EXPIRY must be positive")
	}
	if cfg.PostgresDSN == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
