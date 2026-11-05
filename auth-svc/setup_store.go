package authsvc

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const adminExistsSQL = `SELECT EXISTS (SELECT 1 FROM auth_svc.users WHERE role = 'admin')`

func (s *PostgresStore) AdminExists(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, adminExistsSQL).Scan(&exists); err != nil {
		return false, fmt.Errorf("admin exists: %w", err)
	}
	return exists, nil
}

func (s *PostgresStore) PutSetupToken(ctx context.Context, tokenSHA256 string) (bool, error) {
	// One statement, so "no admin yet" and the write cannot drift apart.
	const q = `
		INSERT INTO auth_svc.setup_token (token_sha256)
		SELECT $1 WHERE NOT (` + adminExistsSQL + `)
		ON CONFLICT (singleton) DO UPDATE
			SET token_sha256 = EXCLUDED.token_sha256, created_at = now()`
	tag, err := s.db.Exec(ctx, q, tokenSHA256)
	if err != nil {
		return false, fmt.Errorf("put setup token: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

func (s *PostgresStore) CreateFirstAdmin(ctx context.Context, tokenSHA256 string, u User) (User, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// FOR UPDATE makes two requests with the right token take turns: the
	// second one finds the row gone and gets ErrSetupDone.
	var stored string
	err = tx.QueryRow(ctx, `SELECT token_sha256 FROM auth_svc.setup_token WHERE singleton FOR UPDATE`).Scan(&stored)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		var exists bool
		if err := tx.QueryRow(ctx, adminExistsSQL).Scan(&exists); err != nil {
			return User{}, fmt.Errorf("admin exists: %w", err)
		}
		if exists {
			return User{}, ErrSetupDone
		}
		// No admin and no token: nothing to check against until a restart
		// writes one.
		return User{}, ErrBadSetupToken
	case err != nil:
		return User{}, fmt.Errorf("read setup token: %w", err)
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(tokenSHA256)) != 1 {
		return User{}, ErrBadSetupToken
	}

	var exists bool
	if err := tx.QueryRow(ctx, adminExistsSQL).Scan(&exists); err != nil {
		return User{}, fmt.Errorf("admin exists: %w", err)
	}
	if !exists {
		u.Role = RoleAdmin
		created, err := insertUser(ctx, tx, u)
		if err != nil {
			return User{}, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM auth_svc.setup_token`); err != nil {
			return User{}, fmt.Errorf("burn setup token: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return User{}, fmt.Errorf("commit: %w", err)
		}
		return created, nil
	}

	// An admin was made some other way; the token is stale. Burn it.
	if _, err := tx.Exec(ctx, `DELETE FROM auth_svc.setup_token`); err != nil {
		return User{}, fmt.Errorf("burn stale setup token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit: %w", err)
	}
	return User{}, ErrSetupDone
}
