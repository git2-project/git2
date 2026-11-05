package authsvc

import (
	"context"
	"errors"
	"fmt"

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
const userColumns = `id, email, username, password_hash, is_active, role, created_at, updated_at`

type PostgresStore struct {
	db *pgxpool.Pool
}

func NewPostgresStore(db *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: db}
}

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.IsActive, &u.Role, &u.CreatedAt, &u.UpdatedAt)
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

func (s *PostgresStore) FindByID(ctx context.Context, id uuid.UUID) (User, error) {
	return s.findOne(ctx, `SELECT `+userColumns+` FROM auth_svc.users WHERE id = $1`, id)
}

// SetActive enables or disables an account. ErrNotFound for an unknown id.
func (s *PostgresStore) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	tag, err := s.db.Exec(ctx, `UPDATE auth_svc.users SET is_active = $2, updated_at = now() WHERE id = $1`, id, active)
	if err != nil {
		return fmt.Errorf("set user active: %w", err)
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
