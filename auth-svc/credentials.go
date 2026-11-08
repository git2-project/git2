package authsvc

import (
	"context"
	"errors"
	"strings"

	"github.com/git2-project/git2/internal/password"
	"github.com/google/uuid"
)

// ErrBadCredentials means the username is unknown, the password is wrong or
// the account is disabled. Callers answer all three the same way, so a
// caller cannot tell which accounts exist.
var ErrBadCredentials = errors.New("invalid credentials")

// UsernameFinder looks a user up by lowercase username. PostgresStore is one.
type UsernameFinder interface {
	FindByUsername(ctx context.Context, username string) (User, error)
}

// Credentials checks a username and password, for clients that sign in on
// every request instead of holding a token: Git over HTTPS through the
// gateway (git2-deploy docs/api/gateway.md, "Git over HTTPS"). It follows
// Login's rules, so both sign-in paths accept and refuse the same accounts.
type Credentials struct {
	users UsernameFinder
	// dummyHash plays the role it has in Handler: an unknown username costs
	// the same bcrypt time as a wrong password.
	dummyHash string
}

func NewCredentials(users UsernameFinder) (*Credentials, error) {
	dummy, err := password.Hash(uuid.NewString())
	if err != nil {
		return nil, err
	}
	return &Credentials{users: users, dummyHash: dummy}, nil
}

// Check returns the user, ErrBadCredentials, or another error when the
// store cannot answer. That error must not read as a refusal: callers
// answer it with 503, never 401 (NFR-22).
func (c *Credentials) Check(ctx context.Context, username, plain string) (User, error) {
	u, err := c.users.FindByUsername(ctx, strings.ToLower(username))
	if errors.Is(err, ErrNotFound) {
		password.Verify(c.dummyHash, plain)
		return User{}, ErrBadCredentials
	}
	if err != nil {
		return User{}, err
	}
	// The password is checked before the account state, as in Login, so a
	// wrong password never learns that an account is disabled.
	if !password.Verify(u.PasswordHash, plain) || !u.IsActive {
		return User{}, ErrBadCredentials
	}
	return u, nil
}
