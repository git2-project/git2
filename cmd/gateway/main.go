// Command gateway is git2's one entry point (WBS-1.2): it routes /api to
// git2 core after the token check and serves the web apps. Contract:
// docs/api/gateway.md in git2-deploy.
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

	"github.com/git2-project/git2/gateway"
	"github.com/git2-project/git2/internal/cache"
	authjwt "github.com/git2-project/git2/internal/jwt"
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

	// The gateway only parses tokens; the expiries are git2 core's business.
	h, err := gateway.New(gateway.Deps{
		JWT:    authjwt.NewManager(cfg.JWTSecret, 0, 0),
		Redis:  rdb,
		Core:   cfg.CoreURL,
		WebDir: cfg.WebDir,
	})
	if err != nil {
		return err
	}

	// No ReadTimeout or WriteTimeout: Git routes (next) stream for minutes.
	// /api requests are bounded per request by gateway.APITimeout instead.
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("gateway listening", "port", cfg.Port, "core", cfg.CoreURL.String())
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
