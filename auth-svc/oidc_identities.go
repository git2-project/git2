package authsvc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// An identity is a provider's subject (the "sub" claim) linked to one git2
// user, in auth_svc.user_identities (007_auth_oidc.sql).

// ErrIdentityTaken means the user already has another identity at this
// provider, or the subject is already linked (a concurrent first sign-in).
var ErrIdentityTaken = errors.New("identity already linked")

// FindUserByIdentity returns the user linked to the provider's subject.
// ErrNotFound when there is none.
func (s *PostgresStore) FindUserByIdentity(ctx context.Context, providerID uuid.UUID, subject string) (User, error) {
	return s.findOne(ctx, `
		SELECT `+prefixed("u.", userColumns)+`
		FROM auth_svc.user_identities i JOIN auth_svc.users u ON u.id = i.user_id
		WHERE i.provider_id = $1 AND i.subject = $2`, providerID, subject)
}

// LinkIdentity links the provider's subject to an existing user.
// ErrIdentityTaken.
func (s *PostgresStore) LinkIdentity(ctx context.Context, providerID uuid.UUID, subject string, userID uuid.UUID, email string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO auth_svc.user_identities (provider_id, subject, user_id, email) VALUES ($1, $2, $3, $4)`,
		providerID, subject, userID, email)
	if isUniqueViolation(err, "user_identities_pkey") || isUniqueViolation(err, "user_identities_provider_user_key") {
		return ErrIdentityTaken
	}
	if err != nil {
		return fmt.Errorf("link identity: %w", err)
	}
	return nil
}

// CreateUserWithIdentity creates the user and links the identity to it in
// one transaction, so a first sign-in never leaves an account without its
// identity. ErrEmailExists, ErrUsernameExists, ErrIdentityTaken.
func (s *PostgresStore) CreateUserWithIdentity(ctx context.Context, u User, providerID uuid.UUID, subject, email string) (User, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := insertUser(ctx, tx, u)
	if err != nil {
		return User{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO auth_svc.user_identities (provider_id, subject, user_id, email) VALUES ($1, $2, $3, $4)`,
		providerID, subject, created.ID, email)
	if isUniqueViolation(err, "user_identities_pkey") {
		return User{}, ErrIdentityTaken
	}
	if err != nil {
		return User{}, fmt.Errorf("link identity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit: %w", err)
	}
	return created, nil
}

// TouchIdentity records a sign-in through the identity and the email the
// provider gave this time.
func (s *PostgresStore) TouchIdentity(ctx context.Context, providerID uuid.UUID, subject, email string) error {
	_, err := s.db.Exec(ctx, `
		UPDATE auth_svc.user_identities SET email = $3, last_sign_in_at = now()
		WHERE provider_id = $1 AND subject = $2`, providerID, subject, email)
	if err != nil {
		return fmt.Errorf("touch identity: %w", err)
	}
	return nil
}

// prefixed qualifies every column of a comma-separated list with prefix.
func prefixed(prefix, columns string) string {
	cols := strings.Split(columns, ",")
	for i, c := range cols {
		cols[i] = prefix + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}
