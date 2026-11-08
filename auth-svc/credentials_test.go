package authsvc

import (
	"context"
	"errors"
	"testing"

	"github.com/git2-project/git2/internal/password"
	"github.com/google/uuid"
)

type usernames struct {
	users map[string]User
	err   error
}

func (u usernames) FindByUsername(_ context.Context, name string) (User, error) {
	if u.err != nil {
		return User{}, u.err
	}
	if user, ok := u.users[name]; ok {
		return user, nil
	}
	return User{}, ErrNotFound
}

func TestCredentialsCheck(t *testing.T) {
	hash, err := password.Hash("right-password")
	if err != nil {
		t.Fatal(err)
	}
	active := User{ID: uuid.New(), Username: "deniz", PasswordHash: hash, IsActive: true}
	disabled := User{ID: uuid.New(), Username: "murat", PasswordHash: hash, IsActive: false}
	creds, err := NewCredentials(usernames{users: map[string]User{"deniz": active, "murat": disabled}})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, username, password string
		wantErr                  error
	}{
		{"right password", "deniz", "right-password", nil},
		{"username in another case", "Deniz", "right-password", nil},
		{"wrong password", "deniz", "wrong-password", ErrBadCredentials},
		{"unknown user", "nobody", "right-password", ErrBadCredentials},
		{"disabled account", "murat", "right-password", ErrBadCredentials},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, err := creds.Check(context.Background(), c.username, c.password)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("err = %v, want %v", err, c.wantErr)
			}
			if c.wantErr == nil && u.ID != active.ID {
				t.Fatalf("user = %v, want %v", u.ID, active.ID)
			}
		})
	}
}

// A store fault is not a refusal: the caller must answer 503, not 401.
func TestCredentialsStoreFaultIsNotBadCredentials(t *testing.T) {
	fault := errors.New("connection refused")
	creds, err := NewCredentials(usernames{err: fault})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creds.Check(context.Background(), "deniz", "x"); !errors.Is(err, fault) || errors.Is(err, ErrBadCredentials) {
		t.Fatalf("err = %v, want the store fault", err)
	}
}
