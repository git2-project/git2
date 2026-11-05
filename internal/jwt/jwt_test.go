package jwt

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const secret = "test-secret-that-is-at-least-32-chars"

func TestAccessRoundTrip(t *testing.T) {
	m := NewManager(secret, time.Minute, time.Hour)
	uid := uuid.New()

	tok, err := m.IssueAccess(uid)
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	claims, err := m.Parse(tok, PurposeAccess)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.UserID != uid {
		t.Errorf("UserID = %v, want %v", claims.UserID, uid)
	}
	if claims.SessionID != "" {
		t.Errorf("access token carries sid %q", claims.SessionID)
	}
}

func TestRefreshCarriesSessionAndJTI(t *testing.T) {
	m := NewManager(secret, time.Minute, time.Hour)
	tok, jti, err := m.IssueRefresh(uuid.New(), "sid-1", time.Now())
	if err != nil {
		t.Fatalf("IssueRefresh: %v", err)
	}
	claims, err := m.Parse(tok, PurposeRefresh)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.SessionID != "sid-1" || claims.ID != jti {
		t.Errorf("claims sid=%q jti=%q, want sid-1 / %q", claims.SessionID, claims.ID, jti)
	}
}

func TestRefreshCarriesSessionStart(t *testing.T) {
	m := NewManager(secret, time.Minute, time.Hour)
	start := time.Now().Add(-time.Hour)
	tok, _, _ := m.IssueRefresh(uuid.New(), "sid", start)
	claims, err := m.Parse(tok, PurposeRefresh)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Times travel as fractional seconds, so parsing can land 1 ms off.
	if claims.SessionStart == nil || abs(claims.SessionStart.UnixMilli()-start.UnixMilli()) > 1 {
		t.Errorf("SessionStart = %v, want %v (±1 ms)", claims.SessionStart, start)
	}

	// A zero start (rotating a token from before the claim) leaves it out.
	tok, _, _ = m.IssueRefresh(uuid.New(), "sid", time.Time{})
	if claims, _ := m.Parse(tok, PurposeRefresh); claims.SessionStart != nil {
		t.Errorf("SessionStart = %v for a zero start, want nil", claims.SessionStart)
	}
	// Access tokens never carry it.
	access, _ := m.IssueAccess(uuid.New())
	if claims, _ := m.Parse(access, PurposeAccess); claims.SessionStart != nil {
		t.Errorf("access token carries SessionStart %v", claims.SessionStart)
	}
}

func TestParseRejects(t *testing.T) {
	m := NewManager(secret, time.Minute, time.Hour)
	uid := uuid.New()
	access, _ := m.IssueAccess(uid)
	refresh, _, _ := m.IssueRefresh(uid, "sid", time.Now())
	expired, _ := NewManager(secret, -time.Minute, time.Hour).IssueAccess(uid)
	foreign, _ := NewManager("another-secret-that-is-32-chars-long", time.Minute, time.Hour).IssueAccess(uid)
	hs384, _ := jwt.NewWithClaims(jwt.SigningMethodHS384, Claims{
		UserID: uid, Purpose: PurposeAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))},
	}).SignedString([]byte(secret))
	noExp, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{UserID: uid, Purpose: PurposeAccess}).SignedString([]byte(secret))

	tests := []struct {
		name    string
		token   string
		purpose TokenPurpose
		want    error
	}{
		{"refresh used as access", refresh, PurposeAccess, ErrWrongPurpose},
		{"access used as refresh", access, PurposeRefresh, ErrWrongPurpose},
		{"expired", expired, PurposeAccess, ErrExpired},
		{"signed with another secret", foreign, PurposeAccess, ErrInvalidToken},
		{"garbage", "not-a-jwt", PurposeAccess, ErrInvalidToken},
		{"HS384 with the right secret", hs384, PurposeAccess, ErrInvalidToken},
		{"no exp claim", noExp, PurposeAccess, ErrInvalidToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := m.Parse(tt.token, tt.purpose); !errors.Is(err, tt.want) {
				t.Errorf("Parse err = %v, want %v", err, tt.want)
			}
		})
	}
}

func abs(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
