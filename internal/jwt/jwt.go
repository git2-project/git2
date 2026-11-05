// Package jwt issues and parses the HS256 access and refresh tokens auth-svc
// hands out. Every token carries a purpose claim so a refresh token can never
// be used as an access token, or the other way round.
package jwt

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Token times carry milliseconds instead of whole seconds. auth-svc rejects
// access tokens issued before a user was disabled by comparing iat with the
// moment of disabling; with whole seconds, a token issued in that same second
// could be neither safely kept nor safely rejected. Fractional NumericDates
// are valid JWT (RFC 7519, section 2), and golang-jwt parses them either way.
func init() { jwt.TimePrecision = time.Millisecond }

type TokenPurpose string

const (
	PurposeAccess  TokenPurpose = "access"
	PurposeRefresh TokenPurpose = "refresh"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpired      = errors.New("token expired")
	ErrWrongPurpose = errors.New("token has wrong purpose")
)

type Claims struct {
	UserID  uuid.UUID    `json:"uid"`
	Purpose TokenPurpose `json:"purpose"`
	// SessionID ties a refresh token to its server-side session record
	// (sess:<sid> in Redis). Set on refresh tokens only and carried unchanged
	// across rotation.
	SessionID string `json:"sid,omitempty"`
	// SessionStart is when the session began (sign-in), on refresh tokens
	// only and carried unchanged across rotation like SessionID. auth-svc
	// compares it with the moment a user was disabled. Missing on refresh
	// tokens issued before it existed.
	SessionStart *jwt.NumericDate `json:"sat,omitempty"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret        []byte
	accessExpiry  time.Duration
	refreshExpiry time.Duration
}

func NewManager(secret string, access, refresh time.Duration) *Manager {
	return &Manager{secret: []byte(secret), accessExpiry: access, refreshExpiry: refresh}
}

// issue mints a signed token and returns it together with its JTI.
func (m *Manager) issue(userID uuid.UUID, purpose TokenPurpose, ttl time.Duration, sid string, sessionStart *jwt.NumericDate) (token, jti string, err error) {
	now := time.Now()
	jti = uuid.NewString()
	claims := Claims{
		UserID:       userID,
		Purpose:      purpose,
		SessionID:    sid,
		SessionStart: sessionStart,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			NotBefore: jwt.NewNumericDate(now),
			ID:        jti,
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", "", err
	}
	return signed, jti, nil
}

func (m *Manager) IssueAccess(userID uuid.UUID) (string, error) {
	token, _, err := m.issue(userID, PurposeAccess, m.accessExpiry, "", nil)
	return token, err
}

// IssueRefresh issues a refresh token bound to session id sid and returns it
// together with its JTI, so the caller can record the session's currently
// valid JTI without re-parsing.
//
// sessionStart is when the session began: time.Now() at sign-in, and the
// SessionStart of the presented token on rotation. A zero time (rotating a
// token from before the claim existed) leaves the claim out.
func (m *Manager) IssueRefresh(userID uuid.UUID, sid string, sessionStart time.Time) (token, jti string, err error) {
	var sat *jwt.NumericDate
	if !sessionStart.IsZero() {
		sat = jwt.NewNumericDate(sessionStart)
	}
	return m.issue(userID, PurposeRefresh, m.refreshExpiry, sid, sat)
}

func (m *Manager) Parse(tokenString string, expected TokenPurpose) (*Claims, error) {
	claims := &Claims{}
	// Pin the algorithm instead of accepting any HMAC variant, and refuse a
	// token without exp: every token this Manager issues carries one.
	tok, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpired
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !tok.Valid {
		return nil, ErrInvalidToken
	}
	if claims.Purpose != expected {
		return nil, ErrWrongPurpose
	}
	return claims, nil
}

func (m *Manager) AccessExpiry() time.Duration  { return m.accessExpiry }
func (m *Manager) RefreshExpiry() time.Duration { return m.refreshExpiry }
