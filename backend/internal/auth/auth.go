package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/google/uuid"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrTokenExpired       = errors.New("token has expired")
	ErrTokenInvalid       = errors.New("token is invalid")
	ErrWeakPassword       = errors.New("password must be at least 12 characters and contain uppercase, lowercase, digit, and special character")
)

// Claims represents the JWT payload for authenticated sessions.
type Claims struct {
	UserID      uuid.UUID `json:"uid"`
	Email       string    `json:"email"`
	DisplayName string    `json:"name"`
	Role        string    `json:"role"`
	jwt.RegisteredClaims
}

// TokenPair holds access and refresh tokens.
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Service provides authentication operations.
type Service struct {
	secret        []byte
	accessExpiry  time.Duration
	refreshExpiry time.Duration
}

// NewService creates an auth service with the given signing key.
func NewService(secret string, accessExpiryHours int) *Service {
	return &Service{
		secret:        []byte(secret),
		accessExpiry:  time.Duration(accessExpiryHours) * time.Hour,
		refreshExpiry: time.Duration(accessExpiryHours*7) * time.Hour,
	}
}

// HashPassword generates a bcrypt hash from the raw password.
func HashPassword(raw string) (string, error) {
	if err := ValidatePasswordStrength(raw); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(raw), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword verifies a raw password against a stored bcrypt hash.
func CheckPassword(hash, raw string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(raw)); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// ValidatePasswordStrength enforces complexity rules.
func ValidatePasswordStrength(pw string) error {
	if len(pw) < 12 {
		return ErrWeakPassword
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, ch := range pw {
		switch {
		case unicode.IsUpper(ch):
			hasUpper = true
		case unicode.IsLower(ch):
			hasLower = true
		case unicode.IsDigit(ch):
			hasDigit = true
		case unicode.IsPunct(ch) || unicode.IsSymbol(ch):
			hasSpecial = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit || !hasSpecial {
		return ErrWeakPassword
	}
	return nil
}

// GenerateTokenPair creates a signed access + refresh JWT pair.
func (s *Service) GenerateTokenPair(userID uuid.UUID, email, displayName, role string) (*TokenPair, error) {
	now := time.Now()
	expiresAt := now.Add(s.accessExpiry)

	accessClaims := &Claims{
		UserID:      userID,
		Email:       email,
		DisplayName: displayName,
		Role:        role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Issuer:    "sentrix",
		},
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("signing access token: %w", err)
	}

	refreshClaims := &Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshExpiry)),
			Issuer:    "sentrix",
		},
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString(s.secret)
	if err != nil {
		return nil, fmt.Errorf("signing refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresAt:    expiresAt,
	}, nil
}

// ValidateToken parses and validates a JWT, returning the embedded claims.
func (s *Service) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}

	return claims, nil
}

// ExtractBearerToken pulls the token string from an "Authorization: Bearer <token>" header.
func ExtractBearerToken(header string) string {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// GenerateAPIToken creates a cryptographically random API token and returns
// both the raw token (to display once) and its SHA-256 hash (to store).
func GenerateAPIToken() (raw string, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generating random bytes: %w", err)
	}
	raw = "stx_" + hex.EncodeToString(b)
	h := sha256.Sum256([]byte(raw))
	hash = hex.EncodeToString(h[:])
	return raw, hash, nil
}

// HashAPIToken hashes a raw API token for lookup.
func HashAPIToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
