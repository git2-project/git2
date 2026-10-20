// Package database opens the Postgres pool shared by every service.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool creates a pgxpool connected to the given DSN. When the DSN
// points at a transaction-pooling PgBouncer (Supabase's port 6543 pooler), it
// disables server-side prepared statements so the pooler does not collide on
// statement names across shared connections.
func NewPostgresPool(ctx context.Context, dsn string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}

	cfg.MaxConns = maxConns
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	if isTransactionPooler(dsn) {
		cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
		cfg.ConnConfig.StatementCacheCapacity = 0
		cfg.ConnConfig.DescriptionCacheCapacity = 0
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// isTransactionPooler reports whether the DSN targets Supabase's transaction
// pooler (port 6543) or any host that advertises pgbouncer in the URL.
func isTransactionPooler(dsn string) bool {
	lower := strings.ToLower(dsn)
	return strings.Contains(dsn, ":6543/") ||
		strings.Contains(lower, "pgbouncer") ||
		strings.Contains(lower, "pooler.supabase")
}
