// Command gateway is git2's one entry point (WBS-1.2): it routes /api to
// git2 core after the token check, Git over HTTPS to git-svc after a password
// sign-in, and serves the web apps. Contract: docs/api/gateway.md in
// git2-deploy.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/gateway"
	"github.com/git2-project/git2/internal/cache"
	"github.com/git2-project/git2/internal/database"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/redis/go-redis/v9"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := gateway.LoadConfig()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg); err != nil {
		slog.Error("gateway stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *gateway.Config) error {
	rdb, err := cache.NewRedisClient(ctx, cfg.RedisAddr, cfg.RedisPassword)
	if err != nil {
		return err
	}
	defer rdb.Close()

	git, closeGit, err := gitDeps(ctx, cfg, rdb)
	if err != nil {
		return err
	}
	defer closeGit()

	// The gateway only parses tokens; the expiries are git2 core's business.
	h, err := gateway.New(gateway.Deps{
		JWT:    authjwt.NewManager(cfg.JWTSecret, 0, 0),
		Redis:  rdb,
		Core:   cfg.CoreURL,
		WebDir: cfg.WebDir,
		Git:    git,
		HSTS:   cfg.TLS(),
	})
	if err != nil {
		return err
	}

	servers := []*listener{{srv: newServer(cfg.Port, h)}}
	if cfg.TLS() {
		servers[0].srv.TLSConfig = gateway.TLSConfig()
		servers[0].cert, servers[0].key = cfg.TLSCertFile, cfg.TLSKeyFile
		if cfg.RedirectPort != "" {
			servers = append(servers, &listener{srv: newServer(cfg.RedirectPort, gateway.RedirectToHTTPS(cfg.ExternalURL))})
		}
	}
	slog.Info("gateway listening", "port", cfg.Port, "tls", cfg.TLS(),
		"redirect_port", cfg.RedirectPort, "core", cfg.CoreURL.String())
	return serve(ctx, servers)
}

// No ReadTimeout or WriteTimeout: Git routes stream for minutes. /api
// requests are bounded per request by gateway.APITimeout instead.
func newServer(port string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + port,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

// listener is a server and, for HTTPS, its certificate files. The files are
// read at start; a renewed certificate takes a restart.
type listener struct {
	srv       *http.Server
	cert, key string
}

func (l *listener) listen() error {
	if l.cert != "" {
		return l.srv.ListenAndServeTLS(l.cert, l.key)
	}
	return l.srv.ListenAndServe()
}

// serve runs every server until one fails or ctx ends, then shuts them all
// down: a gateway that lost its HTTPS port must not keep redirecting to it.
func serve(ctx context.Context, servers []*listener) error {
	errCh := make(chan error, len(servers))
	for _, l := range servers {
		go func() { errCh <- l.listen() }()
	}
	var runErr error
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, l := range servers {
		if err := l.srv.Shutdown(shutdownCtx); err != nil && runErr == nil {
			runErr = err
		}
	}
	return runErr
}

// gitPoolSize bounds the gateway's database connections. Each Git request
// makes one short user lookup; the bcrypt check that follows, not the
// query, is what takes time.
const gitPoolSize = 4

// gitDeps connects what the Git route needs: auth-svc's users for the
// password check and git-svc for the repository lookup.
func gitDeps(ctx context.Context, cfg *gateway.Config, rdb *redis.Client) (*gateway.GitDeps, func(), error) {
	pool, err := database.NewPostgresPool(ctx, cfg.PostgresDSN, gitPoolSize)
	if err != nil {
		return nil, nil, err
	}
	creds, err := authsvc.NewCredentials(authsvc.NewPostgresStore(pool))
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	repos, conn, err := gateway.NewGitSvcRepos(cfg.GitSvcGRPC)
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	closeAll := func() {
		_ = conn.Close()
		pool.Close()
	}
	return &gateway.GitDeps{
		Creds:   creds,
		Repos:   repos,
		Limiter: gateway.NewSignInLimiter(rdb),
		GitSvc:  cfg.GitSvcHTTP,
	}, closeAll, nil
}
