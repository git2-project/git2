package authsvc

import (
	"time"

	"github.com/google/uuid"
)

type RegisterRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (r *RegisterRequest) Normalize() {
	r.Email = normalizeEmail(r.Email)
	r.Username = normalizeUsername(r.Username)
}

func (r *RegisterRequest) Validate() error {
	if err := validateEmail(r.Email); err != nil {
		return err
	}
	if err := validateUsername(r.Username); err != nil {
		return err
	}
	return validatePassword(r.Password)
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Normalize is all login does to the address: no shape check. Login only looks
// a row up, so a malformed address already ends in the same 401 as an unknown
// one. Validation belongs on the paths that write a row.
func (r *LoginRequest) Normalize() {
	r.Email = normalizeEmail(r.Email)
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token,omitempty"`
}

// UserResponse is the public wire shape of a user. The password hash never
// leaves the service.
type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

func toUserResponse(u User) UserResponse {
	return UserResponse{ID: u.ID, Email: u.Email, Username: u.Username, CreatedAt: u.CreatedAt}
}

type messageResponse struct {
	Message string `json:"message"`
}

// LoginResponse is the body of POST /api/auth/login. The tokens themselves
// travel in response headers, see writeTokenHeaders.
type LoginResponse struct {
	Message string       `json:"message"`
	User    UserResponse `json:"user"`
}
