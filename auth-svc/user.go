package authsvc

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound       = errors.New("user not found")
	ErrEmailExists    = errors.New("email already registered")
	ErrUsernameExists = errors.New("username already taken")
)

type User struct {
	ID           uuid.UUID
	Email        string
	Username     string
	PasswordHash string
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Usernames end up in repository URLs (/<username>/<repo>), so they are
// lowercase, start with a letter or digit and use only URL-safe characters.
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,38}$`)

// bcrypt silently ignores everything past 72 bytes, so a longer password
// would verify against any string sharing its first 72 bytes.
const (
	minPasswordLen = 8
	maxPasswordLen = 72
)

func normalizeEmail(s string) string    { return strings.ToLower(strings.TrimSpace(s)) }
func normalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func validateEmail(s string) error {
	if len(s) > 254 {
		return errors.New("email is too long")
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s || !strings.Contains(s[strings.LastIndex(s, "@"):], ".") {
		return errors.New("email is invalid")
	}
	return nil
}

func validateUsername(s string) error {
	if !usernamePattern.MatchString(s) {
		return errors.New("username must be 3-39 characters of a-z, 0-9, '-' or '_', starting with a letter or digit")
	}
	return nil
}

func validatePassword(s string) error {
	if len(s) < minPasswordLen {
		return errors.New("password must be at least 8 characters")
	}
	if len(s) > maxPasswordLen {
		return errors.New("password must be at most 72 bytes")
	}
	return nil
}
