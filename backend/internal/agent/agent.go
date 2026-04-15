package agent

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/yourorg/sentrix/internal/provider"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// AgentType enumerates the specialist agent roles.
type AgentType string

const (
	AgentOrchestrator AgentType = "orchestrator"
	AgentPrimary      AgentType = "primary"
	AgentAssistant    AgentType = "assistant"
	AgentGenerator    AgentType = "generator"
	AgentRefiner      AgentType = "refiner"
	AgentReporter     AgentType = "reporter"
	AgentAdviser      AgentType = "adviser"
	AgentReflector    AgentType = "reflector"
	AgentResearcher   AgentType = "researcher"
	AgentSearcher     AgentType = "searcher"
	AgentEnricher     AgentType = "enricher"
	AgentInstaller    AgentType = "installer"
	AgentPentester    AgentType = "pentester"
	AgentCoder        AgentType = "coder"
	AgentSummarizer   AgentType = "summarizer" // internal-only, not user-configurable
)

// AgentResult holds the output from a single agent run.
type AgentResult struct {
	Content       string              `json:"content"`
	ToolCalls     []provider.ToolCall `json:"tool_calls,omitempty"`
	TokensUsed    int                 `json:"tokens_used"`
	ToolCallsUsed int                 `json:"tool_calls_used"`
	Duration      time.Duration       `json:"duration"`
}

// ToolExecutor is an interface that executes a tool call and returns the result.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args string) (string, error)
	Available() []provider.ToolDef
}

// Agent represents a specialist agent that can reason and act.
type Agent struct {
	Type      AgentType
	FlowID    uuid.UUID
	TaskID    uuid.UUID
	SubtaskID uuid.UUID
	LLM       provider.LLM
	Tools     ToolExecutor
	Monitor   *ExecutionMonitor
	Tracer    trace.Tracer
	DB        *gorm.DB
}

// PlaceholderToolExecutor is a stub tool executor that logs commands
// without executing them. Real tools (nmap, sqlmap, etc.) come in Phase 5/6.
type PlaceholderToolExecutor struct{}

func (p *PlaceholderToolExecutor) Execute(_ context.Context, name string, args string) (string, error) {
	return "[PLACEHOLDER] Tool '" + name + "' called with: " + args +
		"\n\nNote: Real execution is not yet available. " +
		"This is a simulated response for development purposes.", nil
}

func (p *PlaceholderToolExecutor) Available() []provider.ToolDef {
	return []provider.ToolDef{
		{
			Name:        "terminal_exec",
			Description: "Execute a shell command in the sandboxed environment. Use for running security tools, scripts, and system commands.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The shell command to execute",
					},
					"working_dir": map[string]interface{}{
						"type":        "string",
						"description": "Working directory for the command (optional)",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "file_write",
			Description: "Write content to a file in the sandbox workspace.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path relative to workspace",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "File content to write",
					},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "file_read",
			Description: "Read the content of a file from the sandbox workspace.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "File path relative to workspace",
					},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "web_search",
			Description: "Search the web for information about vulnerabilities, exploits, or security topics.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "report_finding",
			Description: "Report a security finding or vulnerability discovered during assessment.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"severity": map[string]interface{}{
						"type":        "string",
						"description": "Severity level: critical, high, medium, low, info",
						"enum":        []string{"critical", "high", "medium", "low", "info"},
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "Short title for the finding",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Detailed description of the vulnerability",
					},
					"evidence": map[string]interface{}{
						"type":        "string",
						"description": "Evidence or proof of the vulnerability",
					},
				},
				"required": []string{"severity", "title", "description"},
			},
		},
	}
}
