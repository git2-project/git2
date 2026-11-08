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
	// Git over HTTPS. The gateway reads auth-svc's users to check Git
	// passwords (contract, "Git over HTTPS"), so it needs the database.
	PostgresDSN string   // POSTGRES_DSN
	GitSvcGRPC  string   // GIT_SVC_GRPC_ADDR: git-svc's RepositoryService
	GitSvcHTTP  *url.URL // GIT_SVC_HTTP_URL: git-svc's Smart HTTP port
	// TLS (NFR-04). Both files or neither: without them the gateway serves
	// plain HTTP, for a TLS proxy in front or a loopback-only dev stack.
	TLSCertFile string // GATEWAY_TLS_CERT_FILE: PEM chain, leaf first
	TLSKeyFile  string // GATEWAY_TLS_KEY_FILE
	// RedirectPort, with TLS, also listens for plain HTTP and redirects it to
	// ExternalURL.
	RedirectPort string   // GATEWAY_HTTP_REDIRECT_PORT
	ExternalURL  *url.URL // GIT2_EXTERNAL_URL: https://<host>[:port], the address users type
}

// TLS reports whether the gateway terminates TLS itself.
func (c *Config) TLS() bool { return c.TLSCertFile != "" }

// LoadConfig reads the gateway's configuration from the environment. It
// does not use internal/config, which loads settings the gateway has no use
// for.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Port:          getenv("GATEWAY_PORT", "8000"),
		WebDir:        getenv("GATEWAY_WEB_DIR", "/srv/web"),
		RedisAddr:     getenv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		PostgresDSN:   os.Getenv("POSTGRES_DSN"),
		GitSvcGRPC:    getenv("GIT_SVC_GRPC_ADDR", "localhost:9090"),
	}
	var err error
	if cfg.CoreURL, err = httpURL("GIT2_CORE_URL", "http://localhost:8080"); err != nil {
		return nil, err
	}
	if cfg.GitSvcHTTP, err = httpURL("GIT_SVC_HTTP_URL", "http://localhost:9080"); err != nil {
		return nil, err
	}
	if cfg.PostgresDSN == "" {
		return nil, fmt.Errorf("POSTGRES_DSN is required: Git sign-in reads auth-svc's users")
	}
	if err := cfg.loadTLS(); err != nil {
		return nil, err
	}
	// The same secret git2 core signs with, and the same minimum it enforces.
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	return cfg, nil
}

// loadTLS reads the TLS settings and refuses half of them: a certificate
// without its key, or a redirect with nowhere to send it, would otherwise
// start a gateway that quietly serves plain HTTP.
func (c *Config) loadTLS() error {
	c.TLSCertFile = os.Getenv("GATEWAY_TLS_CERT_FILE")
	c.TLSKeyFile = os.Getenv("GATEWAY_TLS_KEY_FILE")
	c.RedirectPort = os.Getenv("GATEWAY_HTTP_REDIRECT_PORT")
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("set both GATEWAY_TLS_CERT_FILE and GATEWAY_TLS_KEY_FILE, or neither")
	}
	if c.RedirectPort == "" {
		return nil
	}
	if !c.TLS() {
		return fmt.Errorf("GATEWAY_HTTP_REDIRECT_PORT needs TLS: there is no HTTPS to redirect to")
	}
	u, err := url.Parse(os.Getenv("GIT2_EXTERNAL_URL"))
	if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return fmt.Errorf("GATEWAY_HTTP_REDIRECT_PORT needs GIT2_EXTERNAL_URL as https://<host>[:port]")
	}
	u.Path = ""
	c.ExternalURL = u
	return nil
}

func httpURL(key, fallback string) (*url.URL, error) {
	u, err := url.Parse(getenv(key, fallback))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%s must be an http(s) URL with a host", key)
	}
	return u, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
