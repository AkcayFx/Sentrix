package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/config"
	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/memory"
	"github.com/yourorg/sentrix/internal/provider"
	"github.com/yourorg/sentrix/internal/sandbox"
)

// contextKey is an unexported type for context keys in this package.
type contextKey int

const (
	// ctxKeyToolActionID stores the current tool Action UUID in context.
	ctxKeyToolActionID contextKey = iota
)

// ToolActionIDFromContext returns the tool-level Action ID from context, if set.
func ToolActionIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(ctxKeyToolActionID).(uuid.UUID)
	return id, ok
}

const (
	// DefaultResultSizeLimit is the max result size before truncation.
	DefaultResultSizeLimit = 16 * 1024 // 16 KB
)

// Tool describes a callable tool implementation.
type Tool interface {
	Name() string
	Definition() provider.ToolDef
	Binary() string
	IsAvailable() bool
	Handle(ctx context.Context, rawArgs json.RawMessage) (string, error)
}

// ToolHandler is the function signature every tool must implement.
type ToolHandler func(ctx context.Context, rawArgs json.RawMessage) (string, error)
type DelegateFunc func(ctx context.Context, role string, objective string, supportingContext string) (string, error)

type handlerTool struct {
	name   string
	def    provider.ToolDef
	binary string
	handle ToolHandler
}

func (t *handlerTool) Name() string { return t.name }

func (t *handlerTool) Definition() provider.ToolDef { return t.def }

func (t *handlerTool) Binary() string { return t.binary }

func (t *handlerTool) IsAvailable() bool {
	return t.binary == "" || isBinaryAvailable(t.binary)
}

func (t *handlerTool) Handle(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	return t.handle(ctx, rawArgs)
}

type toolEntry struct {
	Tool Tool
}

// ToolRegistry implements the agent.ToolExecutor interface.
type ToolRegistry struct {
	mu          sync.RWMutex
	tools       map[string]*toolEntry
	order       []string
	findings    []ReportFindingArgs
	cfg         *config.Config
	sandbox     sandbox.Client
	containerID string // container for the current flow (set externally)
	memStore    *memory.MemoryStore
	userID      uuid.UUID
	flowID      *uuid.UUID
	taskID      *uuid.UUID
	subtaskID   *uuid.UUID
	db          *gorm.DB
	delegate    DelegateFunc
}

// NewToolRegistry creates a registry pre-loaded with all default tools.
func NewToolRegistry(
	cfg *config.Config,
	sb sandbox.Client,
	ms *memory.MemoryStore,
	userID uuid.UUID,
	flowID, taskID, subtaskID *uuid.UUID,
	db *gorm.DB,
) *ToolRegistry {
	r := &ToolRegistry{
		tools:     make(map[string]*toolEntry),
		cfg:       cfg,
		sandbox:   sb,
		memStore:  ms,
		userID:    userID,
		flowID:    flowID,
		taskID:    taskID,
		subtaskID: subtaskID,
		db:        db,
	}
	r.registerDefaults()
	return r
}

// SetContainerID sets the sandbox container for the current flow execution.
func (r *ToolRegistry) SetContainerID(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.containerID = id
}

// SetExecutionContext rebinds the registry to a new subtask without
// recreating the registry or re-registering tools. This enables safe
// reuse of one ToolRegistry across all subtasks in a task.
func (r *ToolRegistry) SetExecutionContext(flowID, taskID, subtaskID uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flowID = &flowID
	r.taskID = &taskID
	r.subtaskID = &subtaskID
}

// ResetFindings clears accumulated findings so a reused registry does
// not leak previous subtask findings into later action rows.
func (r *ToolRegistry) ResetFindings() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.findings = nil
}

// EnableDelegation registers internal delegation tools for the current registry.
func (r *ToolRegistry) EnableDelegation(delegate DelegateFunc) {
	r.mu.Lock()
	r.delegate = delegate
	r.mu.Unlock()

	r.registerDelegationTools()
}

// SandboxClient returns the sandbox client if sandbox execution is active.
func (r *ToolRegistry) SandboxClient() sandbox.Client { return r.sandbox }

// ContainerID returns the current sandbox container ID.
func (r *ToolRegistry) ContainerID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.containerID
}

// Register adds a tool to the registry from a simple handler function.
func (r *ToolRegistry) Register(name string, handler ToolHandler, def provider.ToolDef, binary string) {
	r.RegisterTool(&handlerTool{
		name:   name,
		def:    def,
		binary: binary,
		handle: handler,
	})
}

// RegisterTool adds a Tool implementation to the registry.
func (r *ToolRegistry) RegisterTool(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.tools[name]; !exists {
		r.order = append(r.order, name)
	}
	r.tools[name] = &toolEntry{Tool: tool}
}

// Available returns all registered tool definitions.
func (r *ToolRegistry) Available() []provider.ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()

	defs := make([]provider.ToolDef, 0, len(r.order))
	for _, name := range r.order {
		defs = append(defs, r.tools[name].Tool.Definition())
	}
	return defs
}

// Execute dispatches a tool call by name.
// It creates a per-tool Action row before invoking the tool and updates it after.
func (r *ToolRegistry) Execute(ctx context.Context, name string, args string) (string, error) {
	r.mu.RLock()
	entry, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Sprintf("Tool '%s' not found. Available tools: %s",
			name, strings.Join(r.toolNames(), ", ")), nil
	}

	if !entry.Tool.IsAvailable() {
		return fmt.Sprintf(
			"%s is unavailable in the current execution environment.\n\n"+
				"Required binary: %s\n"+
				"This Phase 6 integration is ready, but execution still depends on the host or sandbox image including that binary.",
			name, entry.Tool.Binary(),
		), nil
	}

	// Create a per-tool Action row so artifacts can be attached to exact tool calls.
	actionID := r.createToolAction(name, args)
	if actionID != uuid.Nil {
		ctx = context.WithValue(ctx, ctxKeyToolActionID, actionID)
	}

	start := time.Now()
	rawArgs := json.RawMessage(args)
	result, err := entry.Tool.Handle(ctx, rawArgs)

	if err != nil {
		r.finalizeToolAction(actionID, "failed", fmt.Sprintf("Error: %v", err), start)
		return fmt.Sprintf("Error executing tool '%s': %v", name, err), nil
	}

	result = truncateResult(result, DefaultResultSizeLimit)
	r.finalizeToolAction(actionID, "completed", truncateRunes(result, 2000), start)
	return result, nil
}

// createToolAction inserts a running Action row for the given tool call.
func (r *ToolRegistry) createToolAction(toolName, input string) uuid.UUID {
	if r.db == nil || r.subtaskID == nil {
		return uuid.Nil
	}
	action := database.Action{
		SubtaskID:  *r.subtaskID,
		ActionType: toolName,
		Status:     "running",
		Input:      input,
	}
	if err := r.db.Create(&action).Error; err != nil {
		log.WithError(err).Warn("tools: failed to create tool action row")
		return uuid.Nil
	}
	return action.ID
}

// finalizeToolAction updates a tool Action row with final status and duration.
func (r *ToolRegistry) finalizeToolAction(actionID uuid.UUID, status, output string, start time.Time) {
	if r.db == nil || actionID == uuid.Nil {
		return
	}
	durationMs := int(time.Since(start).Milliseconds())
	updates := map[string]interface{}{
		"status":      status,
		"output":      output,
		"duration_ms": durationMs,
	}
	if err := r.db.Model(&database.Action{}).Where("id = ?", actionID).Updates(updates).Error; err != nil {
		log.WithError(err).Warn("tools: failed to finalize tool action row")
	}
}

// GetFindings returns all reported security findings.
func (r *ToolRegistry) GetFindings() []ReportFindingArgs {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]ReportFindingArgs, len(r.findings))
	copy(out, r.findings)
	return out
}

// MergeFindings appends externally produced findings into the registry.
func (r *ToolRegistry) MergeFindings(findings []ReportFindingArgs) {
	if len(findings) == 0 {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.findings = append(r.findings, findings...)
}

func (r *ToolRegistry) addFinding(f ReportFindingArgs) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.findings = append(r.findings, f)
	return len(r.findings)
}

func (r *ToolRegistry) toolNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string{}, r.order...)
}

// isBinaryAvailable checks if a binary is on the system PATH.
func isBinaryAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// truncateResult trims large output with a head+tail approach.
func truncateResult(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	half := limit / 2
	return fmt.Sprintf("%s\n\n... [TRUNCATED - showing first %d and last %d bytes of %d total] ...\n\n%s",
		s[:half], half, half, len(s), s[len(s)-half:])
}

func (r *ToolRegistry) registerDefaults() {
	r.Register("terminal_exec", r.handleTerminal, terminalDef(), binaryForTerminal())
	r.Register("file_read", r.handleFileRead, fileReadDef(), "")
	r.Register("file_write", r.handleFileWrite, fileWriteDef(), "")
	r.Register("report_finding", r.handleReportFinding, reportFindingDef(), "")
	r.Register("done", r.handleDone, doneDef(), "")

	r.registerSecurityTool(newNmapTool())
	r.registerSecurityTool(newMasscanTool())
	r.registerSecurityTool(newSubfinderTool())
	r.registerSecurityTool(newAmassTool())
	r.registerSecurityTool(newNiktoTool())
	r.registerSecurityTool(newWapitiTool())
	r.registerSecurityTool(newSQLMapTool())
	r.registerSecurityTool(newXSStrikeTool())
	r.registerSecurityTool(newMetasploitTool())
	r.registerSecurityTool(newSearchsploitTool())
	r.registerSecurityTool(newHydraTool())
	r.registerSecurityTool(newJohnTool())
	r.registerSecurityTool(newHashcatTool())
	r.registerSecurityTool(newTSharkTool())
	r.registerSecurityTool(newTCPDumpTool())
	r.registerSecurityTool(newTheHarvesterTool())
	r.registerSecurityTool(newReconNGTool())
	r.registerSecurityTool(newFFUFTool())
	r.registerSecurityTool(newGobusterTool())
	r.registerSecurityTool(newWFuzzTool())
	r.registerSearchTools()
	r.registerMemoryTools()

	log.Infof("tools: registered %d tools", len(r.order))
}

type delegationTool struct {
	name        string
	description string
	role        string
	registry    *ToolRegistry
}

func (t *delegationTool) Name() string { return t.name }

func (t *delegationTool) Definition() provider.ToolDef {
	return toolSchema(t.name,
		t.description,
		map[string]interface{}{
			"objective": prop("string", "Concrete objective for the delegated specialist"),
			"context":   prop("string", "Optional supporting context, evidence, or constraints"),
			"message":   prop("string", "Brief reason for the delegation"),
		},
		[]string{"objective", "message"},
	)
}

func (t *delegationTool) Binary() string { return "" }

func (t *delegationTool) IsAvailable() bool {
	return t.registry != nil && t.registry.delegate != nil
}

func (t *delegationTool) Handle(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	if t.registry == nil || t.registry.delegate == nil {
		return "", fmt.Errorf("delegation is unavailable for this agent")
	}

	var args DelegationArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("parse %s args: %w", t.name, err)
	}
	if strings.TrimSpace(args.Objective) == "" {
		return "", fmt.Errorf("%s objective is required", t.name)
	}

	result, err := t.registry.delegate(ctx, t.role, args.Objective, args.Context)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"# Delegated %s Result\n\nObjective: %s\n\n%s",
		strings.Title(strings.ReplaceAll(t.role, "_", " ")),
		args.Objective,
		strings.TrimSpace(result),
	), nil
}

func (r *ToolRegistry) registerDelegationTools() {
	r.RegisterTool(&delegationTool{
		name:        "ask_searcher",
		description: "Delegate focused reconnaissance, OSINT, and web research to the searcher specialist.",
		role:        "searcher",
		registry:    r,
	})
	r.RegisterTool(&delegationTool{
		name:        "ask_pentester",
		description: "Delegate targeted vulnerability validation or exploitation work to the pentester specialist.",
		role:        "pentester",
		registry:    r,
	})
	r.RegisterTool(&delegationTool{
		name:        "ask_coder",
		description: "Delegate script creation, parsing, or automation work to the coder specialist.",
		role:        "coder",
		registry:    r,
	})
	r.RegisterTool(&delegationTool{
		name:        "ask_installer",
		description: "Delegate package installation, environment setup, or tooling preparation to the installer specialist.",
		role:        "installer",
		registry:    r,
	})
	r.RegisterTool(&delegationTool{
		name:        "ask_enricher",
		description: "Delegate evidence synthesis and context enrichment to the enricher specialist.",
		role:        "enricher",
		registry:    r,
	})
	r.RegisterTool(&delegationTool{
		name:        "ask_adviser",
		description: "Request strategy guidance from the adviser specialist when execution is stuck or unclear.",
		role:        "adviser",
		registry:    r,
	})
}

// registerMemoryTools adds memory_store and memory_search when a MemoryStore is available.
func (r *ToolRegistry) registerMemoryTools() {
	if r.memStore == nil || !r.memStore.Enabled() {
		log.Info("tools: memory tools skipped (embedder unavailable)")
		return
	}

	handler := memory.NewMemoryToolHandler(r.memStore, r.userID, r.flowID, r.taskID, r.subtaskID)

	r.Register(memory.ToolNameStore, handler.HandleStore, memory.StoreToolDef(), "")
	r.Register(memory.ToolNameSearch, handler.HandleSearch, memory.SearchToolDef(), "")
}

func (r *ToolRegistry) registerSecurityTool(tool Tool) {
	// Inject sandbox provider into commandTools.
	if ct, ok := tool.(*commandTool); ok {
		ct.sbp = r
	}
	r.RegisterTool(tool)
}

// persistScreenshot saves a screenshot PNG to disk and creates a browser_screenshot artifact.
// Returns the artifact ID and relative file path, or empty strings on failure.
func (r *ToolRegistry) persistScreenshot(ctx context.Context, targetURL, action string, pngData []byte) (string, string) {
	if r.db == nil || r.flowID == nil || len(pngData) == 0 {
		return "", ""
	}

	actionID, ok := ToolActionIDFromContext(ctx)
	if !ok || actionID == uuid.Nil {
		log.Warn("tools: no tool action ID in context for screenshot persistence")
		return "", ""
	}

	artifactID := uuid.New()
	relPath := filepath.Join("screenshots", r.flowID.String(), artifactID.String()+".png")

	// Resolve against DOCKER_DATA_DIR.
	dataDir := "./data/sandbox"
	if r.cfg != nil && r.cfg.Docker.DataDir != "" {
		dataDir = r.cfg.Docker.DataDir
	}
	absPath := filepath.Join(dataDir, relPath)

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		log.WithError(err).Warn("tools: failed to create screenshot directory")
		return "", ""
	}

	// Write PNG file.
	if err := os.WriteFile(absPath, pngData, 0o644); err != nil {
		log.WithError(err).Warn("tools: failed to write screenshot file")
		return "", ""
	}

	// Build metadata JSON.
	meta := map[string]interface{}{
		"tool":         "browser",
		"action":       action,
		"target_url":   targetURL,
		"content_type": "image/png",
		"source":       "scraper",
	}
	metaJSON, _ := json.Marshal(meta)

	// Create artifact row.
	relPathStr := relPath
	artifact := database.Artifact{
		ID:       artifactID,
		ActionID: actionID,
		Kind:     "browser_screenshot",
		FilePath: &relPathStr,
		Metadata: string(metaJSON),
	}
	if err := r.db.Create(&artifact).Error; err != nil {
		log.WithError(err).Warn("tools: failed to create screenshot artifact")
		// Clean up the file on DB failure.
		_ = os.Remove(absPath)
		return "", ""
	}

	return artifactID.String(), relPath
}

func (r *ToolRegistry) logTerminal(streamType, command, content string) {
	if r.db == nil || r.flowID == nil || strings.TrimSpace(content) == "" {
		return
	}

	cmd := command
	record := database.TerminalLog{
		FlowID:     *r.flowID,
		TaskID:     r.taskID,
		SubtaskID:  r.subtaskID,
		StreamType: streamType,
		Content:    content,
	}
	if strings.TrimSpace(command) != "" {
		record.Command = &cmd
	}

	if err := r.db.Create(&record).Error; err != nil {
		log.WithError(err).Warn("tools: failed to persist terminal log")
	}
}

func (r *ToolRegistry) logSearch(
	toolName, provider, query, target string,
	resultCount int,
	summary string,
	metadata map[string]interface{},
) {
	if r.db == nil || r.flowID == nil {
		return
	}

	metaJSON := "{}"
	if metadata != nil {
		if raw, err := json.Marshal(metadata); err == nil {
			metaJSON = string(raw)
		}
	}

	record := database.SearchLog{
		FlowID:      *r.flowID,
		TaskID:      r.taskID,
		SubtaskID:   r.subtaskID,
		ToolName:    toolName,
		Provider:    provider,
		Query:       query,
		Target:      target,
		ResultCount: resultCount,
		Summary:     summary,
		Metadata:    metaJSON,
	}

	if err := r.db.Create(&record).Error; err != nil {
		log.WithError(err).Warn("tools: failed to persist search log")
	}
}

// binaryForTerminal returns the shell binary based on OS.
func binaryForTerminal() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "sh"
}
