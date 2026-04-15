package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/database"
)

const (
	ContextKeyUserID      = "userID"
	ContextKeyEmail       = "userEmail"
	ContextKeyDisplayName = "userDisplayName"
	ContextKeyRole        = "userRole"
)

// RequireAuth is Gin middleware that validates JWT or API token in every request.
func RequireAuth(authSvc *Service, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		raw := ExtractBearerToken(header)

		// Fallback: check httpOnly cookie
		if raw == "" {
			if cookie, err := c.Cookie("sentrix_token"); err == nil {
				raw = cookie
			}
		}

		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}

		// Try API token first (starts with prefix)
		if strings.HasPrefix(raw, "stx_") {
			if failed := handleAPIToken(c, db, raw); failed {
				return
			}
			c.Next()
			return
		}

		// Try JWT
		claims, err := authSvc.ValidateToken(raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		c.Set(ContextKeyUserID, claims.UserID)
		c.Set(ContextKeyEmail, claims.Email)
		c.Set(ContextKeyDisplayName, claims.DisplayName)
		c.Set(ContextKeyRole, claims.Role)
		c.Next()
	}
}

// handleAPIToken validates an API bearer token, updates last_used_at, and sets context.
// Returns true if auth failed (and the handler was aborted).
func handleAPIToken(c *gin.Context, db *gorm.DB, raw string) bool {
	hash := HashAPIToken(raw)
	var tok database.APIToken
	if err := db.Where("token_hash = ?", hash).First(&tok).Error; err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid api token"})
		return true
	}
	if tok.ExpiresAt != nil && tok.ExpiresAt.Before(time.Now()) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "api token expired"})
		return true
	}

	// Update last_used_at in background
	now := time.Now()
	go func() {
		db.Model(&tok).Update("last_used_at", now)
	}()

	// Load user
	var user database.User
	if err := db.First(&user, "id = ?", tok.UserID).Error; err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return true
	}

	c.Set(ContextKeyUserID, user.ID)
	c.Set(ContextKeyEmail, user.Email)
	c.Set(ContextKeyDisplayName, user.DisplayName)
	c.Set(ContextKeyRole, user.Role)
	return false
}

// GetUserID extracts the authenticated user's UUID from context.
func GetUserID(c *gin.Context) uuid.UUID {
	val, exists := c.Get(ContextKeyUserID)
	if !exists {
		return uuid.Nil
	}
	id, ok := val.(uuid.UUID)
	if !ok {
		return uuid.Nil
	}
	return id
}

// RequireRole returns middleware that checks whether the user has one of the allowed roles.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		role, _ := c.Get(ContextKeyRole)
		rs, _ := role.(string)
		if !allowed[rs] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}
		c.Next()
	}
}
