package gateway

import (
	"fmt"
	"net/url"
	"os"
)

// Config is the gateway's environment. Names follow git2 core's where they
// mean the same thing (REDIS_ADDR, REDIS_PASSWORD, JWT_SECRET).
type Config struct {
	Port          string   // GATEWAY_PORT
	CoreURL       *url.URL // GIT2_CORE_URL: git2 core, every /api route
	WebDir        string   // GATEWAY_WEB_DIR: holds dash/ and admin/ (the built web apps)
	RedisAddr     string
	RedisPassword string
	JWTSecret     string
}

// LoadConfig reads the gateway's configuration from the environment. Unlike
// internal/config it needs no database, so it does not use that loader.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Port:          getenv("GATEWAY_PORT", "8000"),
		WebDir:        getenv("GATEWAY_WEB_DIR", "/srv/web"),
		RedisAddr:     getenv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
	}
	core, err := url.Parse(getenv("GIT2_CORE_URL", "http://localhost:8080"))
	if err != nil || (core.Scheme != "http" && core.Scheme != "https") || core.Host == "" {
		return nil, fmt.Errorf("GIT2_CORE_URL must be an http(s) URL with a host")
	}
	cfg.CoreURL = core
	// The same secret git2 core signs with, and the same minimum it enforces.
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
