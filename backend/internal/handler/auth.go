package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/auth"
	"github.com/yourorg/sentrix/internal/database"
)

// AuthHandler handles registration, login, logout, and token refresh.
type AuthHandler struct {
	db      *gorm.DB
	authSvc *auth.Service
}

func NewAuthHandler(db *gorm.DB, authSvc *auth.Service) *AuthHandler {
	return &AuthHandler{db: db, authSvc: authSvc}
}

// --- Request / Response types ---

type RegisterRequest struct {
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required"`
	DisplayName string `json:"display_name"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type AuthResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	User         UserDTO   `json:"user"`
}

type UserDTO struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}

// Register creates a new user account.
func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	// Check duplicate
	var count int64
	h.db.Model(&database.User{}).Where("email = ?", req.Email).Count(&count)
	if count > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = strings.Split(req.Email, "@")[0]
	}

	user := database.User{
		Email:        req.Email,
		PasswordHash: hash,
		DisplayName:  displayName,
		Role:         "user",
	}
	if err := h.db.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}

	tokens, err := h.authSvc.GenerateTokenPair(user.ID, user.Email, user.DisplayName, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate tokens"})
		return
	}

	setAuthCookie(c, tokens.AccessToken, tokens.ExpiresAt)

	c.JSON(http.StatusCreated, AuthResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    tokens.ExpiresAt,
		User: UserDTO{
			ID:          user.ID.String(),
			Email:       user.Email,
			DisplayName: user.DisplayName,
			Role:        user.Role,
		},
	})
}

// Login authenticates a user by email+password.
func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	var user database.User
	if err := h.db.Where("email = ?", req.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	if err := auth.CheckPassword(user.PasswordHash, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	tokens, err := h.authSvc.GenerateTokenPair(user.ID, user.Email, user.DisplayName, user.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate tokens"})
		return
	}

	setAuthCookie(c, tokens.AccessToken, tokens.ExpiresAt)

	c.JSON(http.StatusOK, AuthResponse{
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresAt:    tokens.ExpiresAt,
		User: UserDTO{
			ID:          user.ID.String(),
			Email:       user.Email,
			DisplayName: user.DisplayName,
			Role:        user.Role,
		},
	})
}

// Logout clears the auth cookie.
func (h *AuthHandler) Logout(c *gin.Context) {
	c.SetCookie("sentrix_token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// Me returns the currently authenticated user.
func (h *AuthHandler) Me(c *gin.Context) {
	userID := auth.GetUserID(c)
	var user database.User
	if err := h.db.First(&user, "id = ?", userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, UserDTO{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Role:        user.Role,
	})
}

func setAuthCookie(c *gin.Context, token string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	c.SetCookie("sentrix_token", token, maxAge, "/", "", false, true)
}
