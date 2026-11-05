package authsvc

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// An admin counts only while active. Disable never lets the last active
// admin go, so this turns false only if the database was edited by hand;
// setup then works again as the way back in.
const adminExistsSQL = `SELECT EXISTS (SELECT 1 FROM auth_svc.users WHERE role = 'admin' AND is_active)`

func (s *PostgresStore) AdminExists(ctx context.Context) (bool, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, adminExistsSQL).Scan(&exists); err != nil {
		return false, fmt.Errorf("admin exists: %w", err)
	}
	return exists, nil
}

func (s *PostgresStore) PutSetupToken(ctx context.Context, tokenSHA256 string, replace bool) (PutSetupResult, error) {
	// One statement, so "no active admin" and the write cannot drift apart.
	// The outer SELECT sees the table as it was before the INSERT (a CTE's
	// writes are not visible to it), which is what "kept" needs.
	const q = `
		WITH put AS (
			INSERT INTO auth_svc.setup_token (token_sha256)
			SELECT $1 WHERE NOT (` + adminExistsSQL + `)
			ON CONFLICT (singleton) DO UPDATE
				SET token_sha256 = EXCLUDED.token_sha256, created_at = now()
				WHERE $2 OR auth_svc.setup_token.created_at <= now() - make_interval(secs => $3)
			RETURNING created_at
		)
		SELECT (SELECT created_at FROM put),
		       (` + adminExistsSQL + `),
		       (SELECT created_at FROM auth_svc.setup_token)`
	var stored, existing *time.Time
	var admin bool
	if err := s.db.QueryRow(ctx, q, tokenSHA256, replace, SetupTokenTTL.Seconds()).Scan(&stored, &admin, &existing); err != nil {
		return PutSetupResult{}, fmt.Errorf("put setup token: %w", err)
	}
	switch {
	case stored != nil:
		return PutSetupResult{Stored: true, ExpiresAt: stored.Add(SetupTokenTTL)}, nil
	case admin:
		return PutSetupResult{AdminExists: true}, nil
	case existing != nil:
		return PutSetupResult{ExpiresAt: existing.Add(SetupTokenTTL)}, nil
	}
	return PutSetupResult{}, errors.New("put setup token: neither stored nor kept")
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
	var fresh bool
	err = tx.QueryRow(ctx, `
		SELECT token_sha256, created_at > now() - make_interval(secs => $1)
		FROM auth_svc.setup_token WHERE singleton FOR UPDATE`, SetupTokenTTL.Seconds()).Scan(&stored, &fresh)
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
	// Only after the match, so a wrong token learns nothing about expiry.
	if !fresh {
		return User{}, ErrSetupTokenExpired
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
