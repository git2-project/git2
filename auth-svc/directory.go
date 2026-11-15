package authsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/git2-project/git2/internal/password"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// This file is what admin-api needs from auth-svc to manage accounts. The
// auth_svc schema belongs to auth-svc, so admin-api goes through these
// functions instead of writing SQL against it.

// InputError is a request value that fails validation. Handlers answer it
// with 400 and its message.
type InputError struct{ msg string }

func (e *InputError) Error() string { return e.msg }

func inputErr(err error) error { return &InputError{msg: err.Error()} }

// ParseInstanceRole accepts the instance roles of roles.go only.
func ParseInstanceRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleAdmin, RoleUser:
		return r, nil
	}
	return "", &InputError{msg: "role must be admin or user"}
}

// CheckEmail trims and lowercases an address and validates it, the same way
// registration does.
func CheckEmail(s string) (string, error) {
	s = normalizeEmail(s)
	if err := validateEmail(s); err != nil {
		return "", inputErr(err)
	}
	return s, nil
}

// NewUser checks an account an admin creates, with the same rules as
// registration, and hashes its password. The result is active and ready for
// Create. Invalid input is an *InputError.
func NewUser(email, username, pass string, role Role) (User, error) {
	email, err := CheckEmail(email)
	if err != nil {
		return User{}, err
	}
	username = normalizeUsername(username)
	if err := validateUsername(username); err != nil {
		return User{}, inputErr(err)
	}
	if err := validatePassword(pass); err != nil {
		return User{}, inputErr(err)
	}
	if role == "" {
		role = RoleUser
	}
	hash, err := password.Hash(pass)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}
	return User{Email: email, Username: username, PasswordHash: hash, IsActive: true, Role: role}, nil
}

// UserFilter selects a page of users, oldest first. Users are ordered by id,
// which is a UUIDv7 and so follows creation time.
type UserFilter struct {
	// Prefix matches the start of the username or the email address. It is
	// compared lowercase, the form both are stored in.
	Prefix string
	Role   Role  // "" for any role
	Active *bool // nil for active and disabled users
	// After is the id of the last user of the previous page; uuid.Nil for
	// the first page.
	After uuid.UUID
	Limit int
}

// ListUsers returns at most f.Limit users matching f.
func (s *PostgresStore) ListUsers(ctx context.Context, f UserFilter) ([]User, error) {
	const q = `
		SELECT ` + userColumns + ` FROM auth_svc.users
		WHERE id > $1
		  AND ($2 = '' OR username LIKE $2 || '%' ESCAPE '\' OR email LIKE $2 || '%' ESCAPE '\')
		  AND ($3 = '' OR role = $3)
		  AND ($4::boolean IS NULL OR is_active = $4)
		ORDER BY id
		LIMIT $5`
	rows, err := s.db.Query(ctx, q, f.After, escapeLike(strings.ToLower(f.Prefix)), string(f.Role), f.Active, f.Limit)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	users, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (User, error) { return scanUser(row) })
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

// escapeLike makes s match literally inside a LIKE pattern with ESCAPE '\'.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// UserUpdate holds the fields an admin may change; nil leaves a field as it
// is. Enabling and disabling go through EnableUser and DisableUser, which
// also end sessions.
type UserUpdate struct {
	Email *string // already checked with CheckEmail
	Role  *Role
}

// UpdateUser applies u and returns the user as stored. Demoting the last
// active admin is refused with ErrLastAdmin; the active admin rows are
// locked first, as in Disable, so two concurrent demotions cannot both
// succeed. ErrEmailExists, ErrNotFound.
func (s *PostgresStore) UpdateUser(ctx context.Context, id uuid.UUID, u UserUpdate) (User, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if u.Role != nil && *u.Role != RoleAdmin {
		rows, err := tx.Query(ctx, `SELECT id FROM auth_svc.users WHERE role = 'admin' AND is_active ORDER BY id FOR UPDATE`)
		if err != nil {
			return User{}, fmt.Errorf("lock admins: %w", err)
		}
		admins, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return User{}, fmt.Errorf("lock admins: %w", err)
		}
		if len(admins) == 1 && admins[0] == id {
			return User{}, ErrLastAdmin
		}
	}

	var role *string
	if u.Role != nil {
		r := string(*u.Role)
		role = &r
	}
	updated, err := scanUser(tx.QueryRow(ctx, `
		UPDATE auth_svc.users
		SET email = COALESCE($2, email), role = COALESCE($3, role), updated_at = now()
		WHERE id = $1
		RETURNING `+userColumns, id, u.Email, role))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return User{}, ErrNotFound
	case isUniqueViolation(err, "users_email_key"):
		return User{}, ErrEmailExists
	case err != nil:
		return User{}, fmt.Errorf("update user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit: %w", err)
	}
	return updated, nil
}

// UserTeam is one team membership of a user.
type UserTeam struct {
	TeamID   uuid.UUID
	TeamName string
	Role     Role
	AddedAt  time.Time
}

// UserTeams returns the teams the user belongs to, by team name. An unknown
// user has none; callers look the user up first.
func (s *PostgresStore) UserTeams(ctx context.Context, userID uuid.UUID) ([]UserTeam, error) {
	rows, err := s.db.Query(ctx, `
		SELECT t.id, t.name, m.role, m.added_at
		FROM auth_svc.team_members m JOIN auth_svc.teams t ON t.id = m.team_id
		WHERE m.user_id = $1
		ORDER BY t.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("user teams: %w", err)
	}
	teams, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (UserTeam, error) {
		var t UserTeam
		err := row.Scan(&t.TeamID, &t.TeamName, &t.Role, &t.AddedAt)
		return t, err
	})
	if err != nil {
		return nil, fmt.Errorf("user teams: %w", err)
	}
	return teams, nil
}
