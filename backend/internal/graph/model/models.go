package model

import (
	"time"

	"github.com/google/uuid"
)

// GraphQL model types used by resolvers.
// These mirror the schema.graphqls definitions for Go code generation.

type User struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	CreatedAt   time.Time `json:"createdAt"`
}

type BrowserCapabilities struct {
	Mode               string `json:"mode"`
	ScreenshotsEnabled bool   `json:"screenshotsEnabled"`
}

type Flow struct {
	ID                    uuid.UUID            `json:"id"`
	Title                 string               `json:"title"`
	Description           string               `json:"description"`
	Status                string               `json:"status"`
	Config                string               `json:"config"`
	CreatedAt             time.Time            `json:"createdAt"`
	UpdatedAt             time.Time            `json:"updatedAt"`
	Tasks                 []*Task              `json:"tasks"`
	BrowserCapabilities   BrowserCapabilities  `json:"browserCapabilities"`
}

type Task struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Result      *string    `json:"result"`
	SortOrder   int        `json:"sortOrder"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	Subtasks    []*Subtask `json:"subtasks"`
}

type Subtask struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	AgentRole string    `json:"agentRole"`
	Status    string    `json:"status"`
	Context   string    `json:"context"`
	Result    *string   `json:"result"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Actions   []*Action `json:"actions"`
}

type Action struct {
	ID         uuid.UUID `json:"id"`
	ActionType string    `json:"actionType"`
	Status     string    `json:"status"`
	Input      string    `json:"input"`
	Output     *string   `json:"output"`
	DurationMs *int      `json:"durationMs"`
	CreatedAt  time.Time `json:"createdAt"`
}

type ProviderConfig struct {
	ID           uuid.UUID `json:"id"`
	ProviderType string    `json:"providerType"`
	ModelName    string    `json:"modelName"`
	BaseURL      *string   `json:"baseUrl"`
	IsDefault    bool      `json:"isDefault"`
	HasAPIKey    bool      `json:"hasApiKey"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type APIToken struct {
	ID         uuid.UUID  `json:"id"`
	Label      string     `json:"label"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type CreateAPITokenPayload struct {
	Token string    `json:"token"`
	Data  *APIToken `json:"data"`
}

// Input types

type CreateFlowInput struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Config      *string `json:"config"`
}

type UpdateFlowInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
	Config      *string `json:"config"`
}

type CreateProviderInput struct {
	ProviderType string  `json:"providerType"`
	ModelName    *string `json:"modelName"`
	APIKey       *string `json:"apiKey"`
	BaseURL      *string `json:"baseUrl"`
	IsDefault    *bool   `json:"isDefault"`
	Config       *string `json:"config"`
}

type UpdateProviderInput struct {
	ModelName *string `json:"modelName"`
	APIKey    *string `json:"apiKey"`
	BaseURL   *string `json:"baseUrl"`
	IsDefault *bool   `json:"isDefault"`
	Config    *string `json:"config"`
}

type CreateAPITokenInput struct {
	Label string `json:"label"`
}

// Subscription event types

type FlowEvent struct {
	FlowID    uuid.UUID  `json:"flowId"`
	TaskID    *uuid.UUID `json:"taskId"`
	Status    string     `json:"status"`
	Message   string     `json:"message"`
	Timestamp time.Time  `json:"timestamp"`
}

type ActionEvent struct {
	ActionID   uuid.UUID `json:"actionId"`
	SubtaskID  uuid.UUID `json:"subtaskId"`
	ActionType string    `json:"actionType"`
	Output     string    `json:"output"`
	Timestamp  time.Time `json:"timestamp"`
}
