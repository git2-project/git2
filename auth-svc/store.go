package authsvc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserStore is what the handlers need from persistence. PostgresStore is the
// real one; handler tests use an in-memory fake.
type UserStore interface {
	Create(ctx context.Context, u User) (User, error)
	FindByEmail(ctx context.Context, email string) (User, error)
	FindByID(ctx context.Context, id uuid.UUID) (User, error)
}

// auth-svc owns the auth_svc schema (one schema per service). Not "auth":
// Supabase reserves that schema for its own Auth product.
const userColumns = `id, email, username, password_hash, is_active, role, tokens_valid_after, created_at, updated_at`

type PostgresStore struct {
	db *pgxpool.Pool
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db}
}

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.IsActive, &u.Role, &u.TokensValidAfter, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

func (s *PostgresStore) Create(ctx context.Context, u User) (User, error) {
	return insertUser(ctx, s.db, u)
}

// queryRower is a pool or a transaction.
type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func insertUser(ctx context.Context, db queryRower, u User) (User, error) {
	// v7 keeps ids time-ordered, so the primary key index doubles as a
	// signup-order index.
	id, err := uuid.NewV7()
	if err != nil {
		return User{}, fmt.Errorf("generate user id: %w", err)
	}
	if u.Role == "" {
		u.Role = RoleUser
	}
	const q = `
		INSERT INTO auth_svc.users (id, email, username, password_hash, is_active, role)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + userColumns
	created, err := scanUser(db.QueryRow(ctx, q, id, u.Email, u.Username, u.PasswordHash, u.IsActive, u.Role))
	if err != nil {
		switch {
		case isUniqueViolation(err, "users_email_key"):
			return User{}, ErrEmailExists
		case isUniqueViolation(err, "users_username_key"):
			return User{}, ErrUsernameExists
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	return created, nil
}

func (s *PostgresStore) FindByEmail(ctx context.Context, email string) (User, error) {
	return s.findOne(ctx, `SELECT `+userColumns+` FROM auth_svc.users WHERE email = $1`, email)
}

// FindByUsername takes a lowercase username, the form usernames are stored
// in (users_username_lower).
func (s *PostgresStore) FindByUsername(ctx context.Context, username string) (User, error) {
	return s.findOne(ctx, `SELECT `+userColumns+` FROM auth_svc.users WHERE username = $1`, username)
}

func (s *PostgresStore) FindByID(ctx context.Context, id uuid.UUID) (User, error) {
	return s.findOne(ctx, `SELECT `+userColumns+` FROM auth_svc.users WHERE id = $1`, id)
}

// Disable turns the account off and records the moment in
// tokens_valid_after, using the database clock. It refuses to disable the
// last active admin (ErrLastAdmin): the active admin rows are locked first,
// so two concurrent disables of the last two admins cannot both succeed.
// ErrNotFound for an unknown id. Disabling a disabled user is allowed and
// moves the moment forward.
func (s *PostgresStore) Disable(ctx context.Context, id uuid.UUID) (time.Time, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `SELECT id FROM auth_svc.users WHERE role = 'admin' AND is_active ORDER BY id FOR UPDATE`)
	if err != nil {
		return time.Time{}, fmt.Errorf("lock admins: %w", err)
	}
	admins, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return time.Time{}, fmt.Errorf("lock admins: %w", err)
	}
	if len(admins) == 1 && admins[0] == id {
		return time.Time{}, ErrLastAdmin
	}

	var at time.Time
	err = tx.QueryRow(ctx, `
		UPDATE auth_svc.users
		SET is_active = FALSE, tokens_valid_after = now(), updated_at = now()
		WHERE id = $1
		RETURNING tokens_valid_after`, id).Scan(&at)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("disable user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, fmt.Errorf("commit: %w", err)
	}
	return at, nil
}

// Enable turns the account back on. tokens_valid_after stays, so sessions
// from before the disable keep being refused. ErrNotFound for an unknown id.
func (s *PostgresStore) Enable(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, `UPDATE auth_svc.users SET is_active = TRUE, updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("enable user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) findOne(ctx context.Context, q string, arg any) (User, error) {
	u, err := scanUser(s.db.QueryRow(ctx, q, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user: %w", err)
	}
	return u, nil
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
