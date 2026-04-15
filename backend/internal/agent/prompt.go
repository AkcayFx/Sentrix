package agent

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed prompts/*.tmpl
var promptFS embed.FS

// PromptContext holds the variables available inside prompt templates.
type PromptContext struct {
	FlowTitle           string
	FlowDescription     string
	FlowTarget          string
	TaskTitle           string
	TaskDescription     string
	SubtaskTitle        string
	SubtaskDescription  string
	AvailableTools      []string
	PreviousResults     []string
	AgentRole           string
	DefaultAgentRole    string
	WorkingMemory       string
	EpisodicMemory      string
	CompletedSubtasks   string
	PendingSubtasks     string
	LatestSubtaskResult string
	FindingsSummary     string
	ExecutionSummary    string
	RecoveryReason      string
	LastAgentResponse   string
	ToolUsageSummary    string
}

// promptTemplates caches parsed templates.
var promptTemplates *template.Template

func init() {
	var err error
	promptTemplates, err = template.ParseFS(promptFS, "prompts/*.tmpl")
	if err != nil {
		panic(fmt.Sprintf("agent: failed to parse prompt templates: %v", err))
	}
}

// RenderPrompt renders the system prompt for a given agent type.
func RenderPrompt(agentType AgentType, ctx PromptContext) (string, error) {
	tmplName := string(agentType) + ".tmpl"

	var buf bytes.Buffer
	if err := promptTemplates.ExecuteTemplate(&buf, tmplName, ctx); err != nil {
		return "", fmt.Errorf("render prompt %s: %w", tmplName, err)
	}

	return buf.String(), nil
}
