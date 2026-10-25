// Command git2 is the single binary that runs git2's control-plane services.
// Today that is auth-svc; the other services mount on the same router as they
// land.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	authsvc "github.com/git2-project/git2/auth-svc"
	"github.com/git2-project/git2/internal/cache"
	"github.com/git2-project/git2/internal/config"
	"github.com/git2-project/git2/internal/database"
	"github.com/git2-project/git2/internal/httpx"
	authjwt "github.com/git2-project/git2/internal/jwt"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	portFlag := flag.String("port", "", "override SERVER_PORT env")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}
	if *portFlag != "" {
		cfg.ServerPort = *portFlag
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg); err != nil {
		slog.Error("git2 stopped", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg *config.Config) error {
	db, err := database.NewPostgresPool(ctx, cfg.PostgresDSN, cfg.PostgresMaxConns)
	if err != nil {
		return err
	}
	defer db.Close()

	rdb, err := cache.NewRedisClient(ctx, cfg.RedisAddr, cfg.RedisPassword)
	if err != nil {
		return err
	}
	defer rdb.Close()

	auth, err := authsvc.NewHandler(authsvc.Deps{
		Users: authsvc.NewPostgresStore(db),
		JWT:   authjwt.NewManager(cfg.JWTSecret, cfg.AccessTokenExpiry, cfg.RefreshTokenExpiry),
		Redis: rdb,
	})
	if err != nil {
		return err
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Logger, middleware.Recoverer, middleware.Timeout(30*time.Second))
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Route("/api", auth.Mount)

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      35 * time.Second, // past middleware.Timeout's 30s
		IdleTimeout:       2 * time.Minute,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("git2 listening", "port", cfg.ServerPort, "env", cfg.Environment)
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
