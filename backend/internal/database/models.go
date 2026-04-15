package database

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type User struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Email        string    `gorm:"uniqueIndex;size:255;not null"`
	PasswordHash string    `gorm:"size:255;not null"`
	DisplayName  string    `gorm:"size:255;not null;default:''"`
	Role         string    `gorm:"size:50;not null;default:'user'"`
	CreatedAt    time.Time `gorm:"not null;default:now()"`
	UpdatedAt    time.Time `gorm:"not null;default:now()"`

	Flows     []Flow     `gorm:"foreignKey:UserID"`
	APITokens []APIToken `gorm:"foreignKey:UserID"`
}

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

type Flow struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID      uuid.UUID `gorm:"type:uuid;not null;index"`
	Title       string    `gorm:"size:255;not null"`
	Description string    `gorm:"type:text;not null;default:''"`
	Target      string    `gorm:"type:text;not null;default:''"`
	Status      string    `gorm:"size:50;not null;default:'pending'"`
	Config      string    `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt   time.Time `gorm:"not null;default:now()"`
	UpdatedAt   time.Time `gorm:"not null;default:now()"`

	User      User       `gorm:"foreignKey:UserID"`
	Tasks     []Task     `gorm:"foreignKey:FlowID"`
	Assistant *Assistant `gorm:"foreignKey:FlowID"`
}

type Task struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FlowID      uuid.UUID `gorm:"type:uuid;not null;index"`
	Title       string    `gorm:"size:255;not null"`
	Description string    `gorm:"type:text;not null;default:''"`
	Status      string    `gorm:"size:50;not null;default:'pending'"`
	Result      *string   `gorm:"type:text"`
	SortOrder   int       `gorm:"not null;default:0"`
	CreatedAt   time.Time `gorm:"not null;default:now()"`
	UpdatedAt   time.Time `gorm:"not null;default:now()"`

	Flow     Flow      `gorm:"foreignKey:FlowID"`
	Subtasks []Subtask `gorm:"foreignKey:TaskID"`
}

type Subtask struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	TaskID      uuid.UUID `gorm:"type:uuid;not null;index"`
	Title       string    `gorm:"size:255;not null"`
	Description string    `gorm:"type:text;not null;default:''"`
	AgentRole   string    `gorm:"size:50;not null"`
	SortOrder   int       `gorm:"not null;default:0"`
	Status      string    `gorm:"size:50;not null;default:'pending'"`
	Context     string    `gorm:"type:jsonb;not null;default:'{}'"`
	Result      *string   `gorm:"type:text"`
	CreatedAt   time.Time `gorm:"not null;default:now()"`
	UpdatedAt   time.Time `gorm:"not null;default:now()"`

	Task    Task     `gorm:"foreignKey:TaskID"`
	Actions []Action `gorm:"foreignKey:SubtaskID"`
}

type Action struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	SubtaskID  uuid.UUID `gorm:"type:uuid;not null;index"`
	ActionType string    `gorm:"size:100;not null"`
	Status     string    `gorm:"size:50;not null;default:'pending'"`
	Input      string    `gorm:"type:jsonb;not null;default:'{}'"`
	Output     *string   `gorm:"type:text"`
	DurationMs *int      `gorm:"column:duration_ms"`
	CreatedAt  time.Time `gorm:"not null;default:now()"`

	Subtask   Subtask    `gorm:"foreignKey:SubtaskID"`
	Artifacts []Artifact `gorm:"foreignKey:ActionID"`
}

type AgentLog struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FlowID    uuid.UUID  `gorm:"type:uuid;not null;index"`
	TaskID    *uuid.UUID `gorm:"type:uuid;index"`
	SubtaskID *uuid.UUID `gorm:"type:uuid;index"`
	AgentRole string     `gorm:"size:50;not null"`
	EventType string     `gorm:"size:50;not null"`
	Message   string     `gorm:"type:text;not null"`
	Metadata  string     `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt time.Time  `gorm:"not null;default:now()"`
}

type TerminalLog struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FlowID     uuid.UUID  `gorm:"type:uuid;not null;index"`
	TaskID     *uuid.UUID `gorm:"type:uuid;index"`
	SubtaskID  *uuid.UUID `gorm:"type:uuid;index"`
	StreamType string     `gorm:"size:20;not null"`
	Command    *string    `gorm:"type:text"`
	Content    string     `gorm:"type:text;not null"`
	CreatedAt  time.Time  `gorm:"not null;default:now()"`
}

type SearchLog struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FlowID      uuid.UUID  `gorm:"type:uuid;not null;index"`
	TaskID      *uuid.UUID `gorm:"type:uuid;index"`
	SubtaskID   *uuid.UUID `gorm:"type:uuid;index"`
	ToolName    string     `gorm:"size:50;not null"`
	Provider    string     `gorm:"size:50;not null;default:''"`
	Query       string     `gorm:"type:text;not null;default:''"`
	Target      string     `gorm:"type:text;not null;default:''"`
	ResultCount int        `gorm:"not null;default:0"`
	Summary     string     `gorm:"type:text;not null;default:''"`
	Metadata    string     `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt   time.Time  `gorm:"not null;default:now()"`
}

type VectorStoreLog struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FlowID      uuid.UUID  `gorm:"type:uuid;not null;index"`
	TaskID      *uuid.UUID `gorm:"type:uuid;index"`
	SubtaskID   *uuid.UUID `gorm:"type:uuid;index"`
	Action      string     `gorm:"size:20;not null"`
	Query       string     `gorm:"type:text;not null;default:''"`
	Content     string     `gorm:"type:text;not null;default:''"`
	ResultCount int        `gorm:"not null;default:0"`
	Metadata    string     `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt   time.Time  `gorm:"not null;default:now()"`
}

type Artifact struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	ActionID  uuid.UUID `gorm:"type:uuid;not null;index"`
	Kind      string    `gorm:"size:100;not null"`
	FilePath  *string   `gorm:"size:500"`
	Content   *string   `gorm:"type:text"`
	Metadata  string    `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt time.Time `gorm:"not null;default:now()"`

	Action Action `gorm:"foreignKey:ActionID"`
}

type Memory struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID    uuid.UUID  `gorm:"type:uuid;not null;index"`
	FlowID    *uuid.UUID `gorm:"type:uuid;index"`
	Category  string     `gorm:"size:100;not null"`
	Tier      string     `gorm:"size:20;not null;default:'long_term'"` // long_term, working, episodic
	Content   string     `gorm:"type:text;not null"`
	Embedding []float32  `gorm:"type:vector(1536)"`
	Metadata  string     `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt time.Time  `gorm:"not null;default:now()"`

	User User  `gorm:"foreignKey:UserID"`
	Flow *Flow `gorm:"foreignKey:FlowID"`
}

type APIToken struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID     uuid.UUID `gorm:"type:uuid;not null;index"`
	Label      string    `gorm:"size:255;not null"`
	TokenHash  string    `gorm:"uniqueIndex;size:255;not null"`
	LastUsedAt *time.Time
	ExpiresAt  *time.Time
	CreatedAt  time.Time `gorm:"not null;default:now()"`

	User User `gorm:"foreignKey:UserID"`
}

type ProviderConfig struct {
	ID              uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	UserID          uuid.UUID `gorm:"type:uuid;not null;index"`
	ProviderType    string    `gorm:"size:50;not null"`
	ModelName       string    `gorm:"size:255;not null;default:''"`
	APIKeyEncrypted *string   `gorm:"type:text"`
	BaseURL         *string   `gorm:"size:500"`
	IsDefault       bool      `gorm:"not null;default:false"`
	Config          string    `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt       time.Time `gorm:"not null;default:now()"`
	UpdatedAt       time.Time `gorm:"not null;default:now()"`

	User User `gorm:"foreignKey:UserID"`
}

type Assistant struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FlowID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;index"`
	Title     string    `gorm:"size:255;not null;default:''"`
	Status    string    `gorm:"size:50;not null;default:'idle'"`
	UseAgents bool      `gorm:"not null;default:true"`
	CreatedAt time.Time `gorm:"not null;default:now()"`
	UpdatedAt time.Time `gorm:"not null;default:now()"`

	Flow Flow           `gorm:"foreignKey:FlowID"`
	User User           `gorm:"foreignKey:UserID"`
	Logs []AssistantLog `gorm:"foreignKey:AssistantID"`
}

type AssistantLog struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	AssistantID uuid.UUID `gorm:"type:uuid;not null;index"`
	Role        string    `gorm:"size:50;not null"`
	AgentRole   string    `gorm:"size:50;not null;default:'assistant'"`
	Content     string    `gorm:"type:text;not null"`
	Metadata    string    `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt   time.Time `gorm:"not null;default:now()"`

	Assistant Assistant `gorm:"foreignKey:AssistantID"`
}

type MessageChain struct {
	ID         uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	FlowID     *uuid.UUID `gorm:"type:uuid;index"`
	TaskID     *uuid.UUID `gorm:"type:uuid;index"`
	SubtaskID  *uuid.UUID `gorm:"type:uuid;index"`
	Role       string     `gorm:"size:50;not null"`
	AgentRole  string     `gorm:"size:50;not null;default:''"`
	ChainType  string     `gorm:"size:50;not null;default:'subtask_execution'"`
	Content    string     `gorm:"type:text;not null"`
	TokenCount int        `gorm:"not null;default:0"`
	Metadata   string     `gorm:"type:jsonb;not null;default:'{}'"`
	CreatedAt  time.Time  `gorm:"not null;default:now()"`

	Flow    *Flow    `gorm:"foreignKey:FlowID"`
	Task    *Task    `gorm:"foreignKey:TaskID"`
	Subtask *Subtask `gorm:"foreignKey:SubtaskID"`
}
