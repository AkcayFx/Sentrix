package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/agent"
	"github.com/yourorg/sentrix/internal/auth"
	"github.com/yourorg/sentrix/internal/config"
	"github.com/yourorg/sentrix/internal/database"
)

// FlowHandler provides CRUD operations + execution control for flows.
type FlowHandler struct {
	db          *gorm.DB
	queue       *agent.Queue
	broadcaster *agent.Broadcaster
	dataDir     string // root directory for file-backed artifacts
	scraper     config.ScraperConfig
}

func NewFlowHandler(db *gorm.DB, queue *agent.Queue, broadcaster *agent.Broadcaster, dataDir string, scraper config.ScraperConfig) *FlowHandler {
	return &FlowHandler{db: db, queue: queue, broadcaster: broadcaster, dataDir: dataDir, scraper: scraper}
}

// BrowserCapabilitiesDTO describes the browser mode available in the current environment.
type BrowserCapabilitiesDTO struct {
	Mode               string `json:"mode"`                // "scraper" or "native"
	ScreenshotsEnabled bool   `json:"screenshots_enabled"` // true when scraper is configured
}

// --- Request / Response types ---

type CreateFlowRequest struct {
	Title       string `json:"title" binding:"required"`
	Description string `json:"description"`
	Target      string `json:"target"`
	Config      string `json:"config"`
}

type UpdateFlowRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Target      *string `json:"target"`
	Status      *string `json:"status"`
	Config      *string `json:"config"`
}

type FlowDTO struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Target      string `json:"target"`
	Status      string `json:"status"`
	Config      string `json:"config"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type TaskDTO struct {
	ID          string       `json:"id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Status      string       `json:"status"`
	Result      *string      `json:"result,omitempty"`
	SortOrder   int          `json:"sort_order"`
	Subtasks    []SubtaskDTO `json:"subtasks,omitempty"`
}

type SubtaskDTO struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	AgentRole   string      `json:"agent_role"`
	SortOrder   int         `json:"sort_order"`
	Status      string      `json:"status"`
	Result      *string     `json:"result,omitempty"`
	Actions     []ActionDTO `json:"actions,omitempty"`
}

type ActionDTO struct {
	ID         string  `json:"id"`
	ActionType string  `json:"action_type"`
	Status     string  `json:"status"`
	Input      string  `json:"input"`
	Output     *string `json:"output,omitempty"`
	DurationMs *int    `json:"duration_ms,omitempty"`
}

type AgentLogDTO struct {
	ID        string `json:"id"`
	AgentRole string `json:"agent_role"`
	EventType string `json:"event_type"`
	Message   string `json:"message"`
	Metadata  string `json:"metadata"`
	CreatedAt string `json:"created_at"`
}

type TerminalLogDTO struct {
	ID         string  `json:"id"`
	StreamType string  `json:"stream_type"`
	Command    *string `json:"command,omitempty"`
	Content    string  `json:"content"`
	CreatedAt  string  `json:"created_at"`
}

type SearchLogDTO struct {
	ID          string `json:"id"`
	ToolName    string `json:"tool_name"`
	Provider    string `json:"provider"`
	Query       string `json:"query"`
	Target      string `json:"target"`
	ResultCount int    `json:"result_count"`
	Summary     string `json:"summary"`
	Metadata    string `json:"metadata"`
	CreatedAt   string `json:"created_at"`
}

type VectorStoreLogDTO struct {
	ID          string `json:"id"`
	Action      string `json:"action"`
	Query       string `json:"query"`
	Content     string `json:"content"`
	ResultCount int    `json:"result_count"`
	Metadata    string `json:"metadata"`
	CreatedAt   string `json:"created_at"`
}

type ArtifactDTO struct {
	ID         string  `json:"id"`
	ActionID   string  `json:"action_id"`
	ActionType string  `json:"action_type"`
	TaskID     string  `json:"task_id"`
	TaskTitle  string  `json:"task_title"`
	SubtaskID  string  `json:"subtask_id"`
	Kind       string  `json:"kind"`
	FilePath   *string `json:"file_path,omitempty"`
	Content    *string `json:"content,omitempty"`
	Metadata   string  `json:"metadata"`
	CreatedAt  string  `json:"created_at"`
}

type FindingDTO struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
	TaskTitle   string `json:"task_title"`
	CreatedAt   string `json:"created_at"`
}

type MessageChainDTO struct {
	ID           string `json:"id"`
	FlowID       string `json:"flow_id"`
	TaskID       string `json:"task_id"`
	TaskTitle    string `json:"task_title"`
	SubtaskID    string `json:"subtask_id"`
	SubtaskTitle string `json:"subtask_title"`
	Role         string `json:"role"`
	AgentRole    string `json:"agent_role"`
	ChainType    string `json:"chain_type"`
	Content      string `json:"content"`
	TokenCount   int    `json:"token_count"`
	Metadata     string `json:"metadata"`
	CreatedAt    string `json:"created_at"`
}

type TraceEntryDTO struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	Kind      string `json:"kind"`
	Debug     bool   `json:"debug"`
	AgentRole string `json:"agent_role,omitempty"`
	TaskLabel string `json:"task_label,omitempty"`
	Summary   string `json:"summary"`
	Content   string `json:"content,omitempty"`

	// agent_event fields
	EventType string `json:"event_type,omitempty"`

	// transcript fields
	Role      string `json:"role,omitempty"`
	ChainType string `json:"chain_type,omitempty"`

	// terminal fields
	Command string `json:"command,omitempty"`
	Stdout  string `json:"stdout,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
}

func toFlowDTO(f database.Flow) FlowDTO {
	return FlowDTO{
		ID:          f.ID.String(),
		Title:       f.Title,
		Description: f.Description,
		Target:      f.Target,
		Status:      f.Status,
		Config:      f.Config,
		CreatedAt:   f.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   f.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

func loadTaskTree(db *gorm.DB, flowID uuid.UUID) ([]TaskDTO, error) {
	var tasks []database.Task
	if err := db.Where("flow_id = ?", flowID).Order("sort_order ASC").Find(&tasks).Error; err != nil {
		return nil, err
	}

	if len(tasks) == 0 {
		return []TaskDTO{}, nil
	}

	taskIDs := make([]uuid.UUID, len(tasks))
	for i, task := range tasks {
		taskIDs[i] = task.ID
	}

	var subtasks []database.Subtask
	if err := db.Where("task_id IN ?", taskIDs).Order("sort_order ASC, created_at ASC").Find(&subtasks).Error; err != nil {
		return nil, err
	}

	actionsBySubtaskID := make(map[uuid.UUID][]ActionDTO, len(subtasks))
	if len(subtasks) > 0 {
		subtaskIDs := make([]uuid.UUID, len(subtasks))
		for i, subtask := range subtasks {
			subtaskIDs[i] = subtask.ID
		}

		var actions []database.Action
		if err := db.Where("subtask_id IN ?", subtaskIDs).Order("created_at ASC").Find(&actions).Error; err != nil {
			return nil, err
		}

		for _, action := range actions {
			actionsBySubtaskID[action.SubtaskID] = append(actionsBySubtaskID[action.SubtaskID], ActionDTO{
				ID:         action.ID.String(),
				ActionType: action.ActionType,
				Status:     action.Status,
				Input:      action.Input,
				Output:     action.Output,
				DurationMs: action.DurationMs,
			})
		}
	}

	subtasksByTaskID := make(map[uuid.UUID][]SubtaskDTO, len(tasks))
	for _, subtask := range subtasks {
		subtasksByTaskID[subtask.TaskID] = append(subtasksByTaskID[subtask.TaskID], SubtaskDTO{
			ID:          subtask.ID.String(),
			Title:       subtask.Title,
			Description: subtask.Description,
			AgentRole:   subtask.AgentRole,
			SortOrder:   subtask.SortOrder,
			Status:      subtask.Status,
			Result:      subtask.Result,
			Actions:     actionsBySubtaskID[subtask.ID],
		})
	}

	taskDTOs := make([]TaskDTO, len(tasks))
	for i, task := range tasks {
		taskDTOs[i] = TaskDTO{
			ID:          task.ID.String(),
			Title:       task.Title,
			Description: task.Description,
			Status:      task.Status,
			Result:      task.Result,
			SortOrder:   task.SortOrder,
			Subtasks:    subtasksByTaskID[task.ID],
		}
	}

	return taskDTOs, nil
}

// List returns all flows belonging to the authenticated user.
func (h *FlowHandler) List(c *gin.Context) {
	userID := auth.GetUserID(c)
	var flows []database.Flow
	if err := h.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&flows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch flows"})
		return
	}

	dtos := make([]FlowDTO, len(flows))
	for i, f := range flows {
		dtos[i] = toFlowDTO(f)
	}
	c.JSON(http.StatusOK, dtos)
}

// --- Trace projection sets ------------------------------------------------

var defaultAgentEventTypes = map[string]bool{
	"tool_call":     true,
	"error":         true,
	"fallback":      true,
	"limit_reached": true,
}

var debugChainTypes = map[string]bool{
	"assistant_session":     true,
	"assistant_delegation":  true,
	"task_generation":       true,
	"task_refinement":       true,
	"task_reporting":        true,
	"recovery_advice":       true,
	"recovery_reflection":   true,
}

func traceEntrySummary(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// buildTraceEntries merges agent_logs, terminal_logs, and message_chains into
// a single sorted array with a debug flag on each entry. When includeDebug is
// false, debug-only entries are omitted.
func buildTraceEntries(db *gorm.DB, flowID uuid.UUID, includeDebug bool) []TraceEntryDTO {
	var entries []TraceEntryDTO

	// --- Agent events ---
	var agentRecords []database.AgentLog
	db.Where("flow_id = ?", flowID).Order("created_at ASC").Find(&agentRecords)
	for _, r := range agentRecords {
		isDebug := !defaultAgentEventTypes[r.EventType]
		if !includeDebug && isDebug {
			continue
		}
		entries = append(entries, TraceEntryDTO{
			ID:        r.ID.String(),
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
			Kind:      "agent_event",
			Debug:     isDebug,
			AgentRole: r.AgentRole,
			EventType: r.EventType,
			Summary:   traceEntrySummary(r.Message, 200),
			Content:   r.Message,
		})
	}

	// --- Terminal logs (grouped by command) ---
	var termRecords []database.TerminalLog
	db.Where("flow_id = ?", flowID).Order("created_at ASC").Find(&termRecords)

	type termGroup struct {
		command   string
		stdin     strings.Builder
		stdout    strings.Builder
		stderr    strings.Builder
		status    string
		firstTime time.Time
		firstID   string
	}
	flush := func(g *termGroup) {
		if g == nil {
			return
		}
		summary := g.command
		if summary == "" {
			summary = "(terminal output)"
		}
		entries = append(entries, TraceEntryDTO{
			ID:        g.firstID,
			CreatedAt: g.firstTime.Format(time.RFC3339),
			Kind:      "terminal",
			Debug:     false,
			Summary:   summary,
			Command:   g.command,
			Stdout:    strings.TrimSpace(g.stdout.String()),
			Stderr:    strings.TrimSpace(g.stderr.String()),
		})
	}

	var cur *termGroup
	for _, r := range termRecords {
		cmd := ""
		if r.Command != nil {
			cmd = *r.Command
		}
		// Start a new group when the command changes or the first record.
		if cur == nil || cmd != cur.command {
			flush(cur)
			cur = &termGroup{
				command:   cmd,
				firstTime: r.CreatedAt,
				firstID:   "terminal-" + r.ID.String(),
			}
		}
		switch r.StreamType {
		case "stdin":
			cur.stdin.WriteString(r.Content)
			cur.stdin.WriteByte('\n')
		case "stdout":
			cur.stdout.WriteString(r.Content)
			cur.stdout.WriteByte('\n')
		case "stderr":
			cur.stderr.WriteString(r.Content)
			cur.stderr.WriteByte('\n')
		case "status":
			cur.status = strings.TrimSpace(r.Content)
		}
	}
	flush(cur)

	// --- Message chain transcripts ---
	chainRecords := loadMessageChains(db, flowID)
	for _, r := range chainRecords {
		isDebug := false
		if debugChainTypes[r.ChainType] {
			isDebug = true
		}
		if r.Role == "system" {
			isDebug = true
		}
		// In subtask_execution, only assistant and tool roles are default-visible.
		if r.ChainType == "subtask_execution" && r.Role != "assistant" && r.Role != "tool" {
			isDebug = true
		}

		if !includeDebug && isDebug {
			continue
		}

		summary := traceEntrySummary(r.Content, 200)
		taskLabel := r.SubtaskTitle
		if taskLabel == "" {
			taskLabel = r.TaskTitle
		}
		if taskLabel == "" {
			taskLabel = "flow scope"
		}

		entries = append(entries, TraceEntryDTO{
			ID:        "transcript-" + r.ID,
			CreatedAt: r.CreatedAt,
			Kind:      "transcript",
			Debug:     isDebug,
			AgentRole: r.AgentRole,
			TaskLabel: taskLabel,
			Role:      r.Role,
			ChainType: r.ChainType,
			Summary:   summary,
			Content:   r.Content,
		})
	}

	// Sort all entries by timestamp.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt < entries[j].CreatedAt
	})

	return entries
}

// Get returns a single flow by ID with its tasks tree.
func (h *FlowHandler) Get(c *gin.Context) {
	userID := auth.GetUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid flow id"})
		return
	}

	var flow database.Flow
	if err := h.db.Where("id = ? AND user_id = ?", id, userID).First(&flow).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "flow not found"})
		return
	}

	taskDTOs, err := loadTaskTree(h.db, flow.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch flow tasks"})
		return
	}

	includeDebug := c.Query("include_debug_trace") == "true"

	mode := "native"
	screenshotsEnabled := false
	if h.scraper.PublicURL != "" || h.scraper.PrivateURL != "" {
		mode = "scraper"
		screenshotsEnabled = true
	}

	c.JSON(http.StatusOK, gin.H{
		"flow":              toFlowDTO(flow),
		"tasks":             taskDTOs,
		"trace_entries":     buildTraceEntries(h.db, flow.ID, includeDebug),
		"agent_logs":        loadAgentLogs(h.db, flow.ID),
		"terminal_logs":     loadTerminalLogs(h.db, flow.ID),
		"search_logs":       loadSearchLogs(h.db, flow.ID),
		"vector_store_logs": loadVectorStoreLogs(h.db, flow.ID),
		"artifacts":         loadArtifacts(h.db, flow.ID),
		"findings":          loadFindings(h.db, flow.ID),
		"message_chains":    loadMessageChains(h.db, flow.ID),
		"browser_capabilities": BrowserCapabilitiesDTO{
			Mode:               mode,
			ScreenshotsEnabled: screenshotsEnabled,
		},
	})
}

func loadAgentLogs(db *gorm.DB, flowID uuid.UUID) []AgentLogDTO {
	var records []database.AgentLog
	db.Where("flow_id = ?", flowID).Order("created_at ASC").Find(&records)
	out := make([]AgentLogDTO, len(records))
	for i, record := range records {
		out[i] = AgentLogDTO{
			ID:        record.ID.String(),
			AgentRole: record.AgentRole,
			EventType: record.EventType,
			Message:   record.Message,
			Metadata:  record.Metadata,
			CreatedAt: record.CreatedAt.Format(time.RFC3339),
		}
	}
	return out
}

func loadTerminalLogs(db *gorm.DB, flowID uuid.UUID) []TerminalLogDTO {
	var records []database.TerminalLog
	db.Where("flow_id = ?", flowID).Order("created_at ASC").Find(&records)
	out := make([]TerminalLogDTO, len(records))
	for i, record := range records {
		out[i] = TerminalLogDTO{
			ID:         record.ID.String(),
			StreamType: record.StreamType,
			Command:    record.Command,
			Content:    record.Content,
			CreatedAt:  record.CreatedAt.Format(time.RFC3339),
		}
	}
	return out
}

func loadSearchLogs(db *gorm.DB, flowID uuid.UUID) []SearchLogDTO {
	var records []database.SearchLog
	db.Where("flow_id = ?", flowID).Order("created_at ASC").Find(&records)
	out := make([]SearchLogDTO, len(records))
	for i, record := range records {
		out[i] = SearchLogDTO{
			ID:          record.ID.String(),
			ToolName:    record.ToolName,
			Provider:    record.Provider,
			Query:       record.Query,
			Target:      record.Target,
			ResultCount: record.ResultCount,
			Summary:     record.Summary,
			Metadata:    record.Metadata,
			CreatedAt:   record.CreatedAt.Format(time.RFC3339),
		}
	}
	return out
}

func loadVectorStoreLogs(db *gorm.DB, flowID uuid.UUID) []VectorStoreLogDTO {
	var records []database.VectorStoreLog
	db.Where("flow_id = ?", flowID).Order("created_at ASC").Find(&records)
	out := make([]VectorStoreLogDTO, len(records))
	for i, record := range records {
		out[i] = VectorStoreLogDTO{
			ID:          record.ID.String(),
			Action:      record.Action,
			Query:       record.Query,
			Content:     record.Content,
			ResultCount: record.ResultCount,
			Metadata:    record.Metadata,
			CreatedAt:   record.CreatedAt.Format(time.RFC3339),
		}
	}
	return out
}

type artifactRecord struct {
	ID         uuid.UUID
	ActionID   uuid.UUID
	ActionType string
	TaskID     uuid.UUID
	TaskTitle  string
	SubtaskID  uuid.UUID
	Kind       string
	FilePath   *string
	Content    *string
	Metadata   string
	CreatedAt  time.Time
}

func loadArtifacts(db *gorm.DB, flowID uuid.UUID) []ArtifactDTO {
	var records []artifactRecord
	db.Table("artifacts AS ar").
		Select(`
			ar.id,
			ar.action_id,
			a.action_type,
			s.id AS subtask_id,
			t.id AS task_id,
			t.title AS task_title,
			ar.kind,
			ar.file_path,
			ar.content,
			ar.metadata,
			ar.created_at
		`).
		Joins("JOIN actions a ON a.id = ar.action_id").
		Joins("JOIN subtasks s ON s.id = a.subtask_id").
		Joins("JOIN tasks t ON t.id = s.task_id").
		Where("t.flow_id = ?", flowID).
		Order("ar.created_at ASC").
		Scan(&records)

	out := make([]ArtifactDTO, len(records))
	for i, record := range records {
		out[i] = ArtifactDTO{
			ID:         record.ID.String(),
			ActionID:   record.ActionID.String(),
			ActionType: record.ActionType,
			TaskID:     record.TaskID.String(),
			TaskTitle:  record.TaskTitle,
			SubtaskID:  record.SubtaskID.String(),
			Kind:       record.Kind,
			FilePath:   record.FilePath,
			Content:    record.Content,
			Metadata:   record.Metadata,
			CreatedAt:  record.CreatedAt.Format(time.RFC3339),
		}
	}
	return out
}

func loadFindings(db *gorm.DB, flowID uuid.UUID) []FindingDTO {
	type findingRecord struct {
		ID        uuid.UUID
		TaskTitle string
		Metadata  string
		CreatedAt time.Time
	}
	var records []findingRecord
	db.Table("artifacts AS ar").
		Select(`ar.id, t.title AS task_title, ar.metadata, ar.created_at`).
		Joins("JOIN actions a ON a.id = ar.action_id").
		Joins("JOIN subtasks s ON s.id = a.subtask_id").
		Joins("JOIN tasks t ON t.id = s.task_id").
		Where("t.flow_id = ? AND ar.kind = ?", flowID, "finding").
		Order("ar.created_at ASC").
		Scan(&records)

	out := make([]FindingDTO, 0, len(records))
	for _, record := range records {
		var meta map[string]string
		if err := json.Unmarshal([]byte(record.Metadata), &meta); err != nil {
			continue
		}
		out = append(out, FindingDTO{
			ID:          record.ID.String(),
			Severity:    meta["severity"],
			Title:       meta["title"],
			Description: meta["description"],
			Evidence:    meta["evidence"],
			TaskTitle:   record.TaskTitle,
			CreatedAt:   record.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}

type messageChainRecord struct {
	ID           uuid.UUID
	FlowID       string
	TaskID       string
	TaskTitle    string
	SubtaskID    string
	SubtaskTitle string
	Role         string
	AgentRole    string
	ChainType    string
	Content      string
	TokenCount   int
	Metadata     string
	CreatedAt    time.Time
}

func loadMessageChains(db *gorm.DB, flowID uuid.UUID) []MessageChainDTO {
	var records []messageChainRecord
	db.Table("message_chains AS mc").
		Select(`
			mc.id,
			COALESCE(mc.flow_id::text, '') AS flow_id,
			COALESCE(COALESCE(mc.task_id, s.task_id)::text, '') AS task_id,
			COALESCE(t.title, '') AS task_title,
			COALESCE(mc.subtask_id::text, '') AS subtask_id,
			COALESCE(s.title, '') AS subtask_title,
			mc.role,
			mc.agent_role,
			mc.chain_type,
			mc.content,
			mc.token_count,
			mc.metadata,
			mc.created_at
		`).
		Joins("LEFT JOIN subtasks s ON s.id = mc.subtask_id").
		Joins("LEFT JOIN tasks t ON t.id = COALESCE(mc.task_id, s.task_id)").
		Where("mc.flow_id = ? OR t.flow_id = ?", flowID, flowID).
		Order("mc.created_at ASC").
		Scan(&records)

	out := make([]MessageChainDTO, len(records))
	for i, record := range records {
		out[i] = MessageChainDTO{
			ID:           record.ID.String(),
			FlowID:       record.FlowID,
			TaskID:       record.TaskID,
			TaskTitle:    record.TaskTitle,
			SubtaskID:    record.SubtaskID,
			SubtaskTitle: record.SubtaskTitle,
			Role:         record.Role,
			AgentRole:    record.AgentRole,
			ChainType:    record.ChainType,
			Content:      record.Content,
			TokenCount:   record.TokenCount,
			Metadata:     record.Metadata,
			CreatedAt:    record.CreatedAt.Format(time.RFC3339),
		}
	}
	return out
}

// Create creates a new flow.
func (h *FlowHandler) Create(c *gin.Context) {
	userID := auth.GetUserID(c)
	var req CreateFlowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	cfg := strings.TrimSpace(req.Config)
	if cfg == "" {
		cfg = "{}"
	}

	flow := database.Flow{
		UserID:      userID,
		Title:       strings.TrimSpace(req.Title),
		Description: strings.TrimSpace(req.Description),
		Target:      strings.TrimSpace(req.Target),
		Status:      "pending",
		Config:      cfg,
	}
	if err := h.db.Create(&flow).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create flow"})
		return
	}

	c.JSON(http.StatusCreated, toFlowDTO(flow))
}

// Update patches flow fields.
func (h *FlowHandler) Update(c *gin.Context) {
	userID := auth.GetUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid flow id"})
		return
	}

	var flow database.Flow
	if err := h.db.Where("id = ? AND user_id = ?", id, userID).First(&flow).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "flow not found"})
		return
	}

	var req UpdateFlowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	updates := make(map[string]interface{})
	if req.Title != nil {
		updates["title"] = strings.TrimSpace(*req.Title)
	}
	if req.Description != nil {
		updates["description"] = strings.TrimSpace(*req.Description)
	}
	if req.Target != nil {
		updates["target"] = strings.TrimSpace(*req.Target)
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Config != nil {
		updates["config"] = *req.Config
	}

	if len(updates) > 0 {
		if err := h.db.Model(&flow).Updates(updates).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update flow"})
			return
		}
	}

	// Reload after update
	h.db.First(&flow, "id = ?", id)
	c.JSON(http.StatusOK, toFlowDTO(flow))
}

// Delete removes a flow.
func (h *FlowHandler) Delete(c *gin.Context) {
	userID := auth.GetUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid flow id"})
		return
	}

	result := h.db.Where("id = ? AND user_id = ?", id, userID).Delete(&database.Flow{})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "flow not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "flow deleted"})
}

// Start enqueues a flow for agent execution.
func (h *FlowHandler) Start(c *gin.Context) {
	userID := auth.GetUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid flow id"})
		return
	}

	var flow database.Flow
	if err := h.db.Where("id = ? AND user_id = ?", id, userID).First(&flow).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "flow not found"})
		return
	}

	if flow.Status == "running" {
		c.JSON(http.StatusConflict, gin.H{"error": "flow is already running"})
		return
	}

	if strings.TrimSpace(flow.Target) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "flow target is empty; set the target (URL, hostname, or IP) before starting"})
		return
	}
	if strings.TrimSpace(flow.Description) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "flow scope is empty; add a scope description before starting"})
		return
	}

	// Reset status to pending and enqueue.
	h.db.Model(&flow).Update("status", "queued")
	h.queue.Enqueue(flow.ID)

	c.JSON(http.StatusOK, gin.H{"message": "flow execution started", "status": "queued"})
}

// Stop cancels a running flow.
func (h *FlowHandler) Stop(c *gin.Context) {
	userID := auth.GetUserID(c)
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid flow id"})
		return
	}

	var flow database.Flow
	if err := h.db.Where("id = ? AND user_id = ?", id, userID).First(&flow).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "flow not found"})
		return
	}

	if h.queue.StopFlow(flow.ID) {
		c.JSON(http.StatusOK, gin.H{"message": "flow execution stopped"})
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "flow is not currently running"})
	}
}

// Events streams SSE events for a flow execution.
func (h *FlowHandler) Events(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid flow id"})
		return
	}

	flowID := id.String()

	// Set SSE headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	// Clear per-request read/write deadlines so the event stream can stay open.
	rc := http.NewResponseController(c.Writer)
	_ = rc.SetReadDeadline(time.Time{})
	_ = rc.SetWriteDeadline(time.Time{})

	// Subscribe to events for this flow.
	ch := h.broadcaster.Subscribe(flowID)
	defer h.broadcaster.Unsubscribe(flowID, ch)

	// Send initial connection event.
	writeSSE(c.Writer, "connected", map[string]string{"flow_id": flowID})
	c.Writer.Flush()

	// Stream events until client disconnects.
	ctx := c.Request.Context()

	ticker := time.NewTicker(10 * time.Second) // keepalive
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(c.Writer, event.Type, event.Data)
			c.Writer.Flush()
		case <-ticker.C:
			// Send keepalive.
			_, _ = io.WriteString(c.Writer, ": keepalive\n\n")
			c.Writer.Flush()
		}
	}
}

// ArtifactFile serves a file-backed artifact with auth, ownership, and path-traversal checks.
// GET /api/v1/flows/:id/artifacts/:artifact_id/file
func (h *FlowHandler) ArtifactFile(c *gin.Context) {
	userID := auth.GetUserID(c)
	flowID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid flow id"})
		return
	}
	artifactID, err := uuid.Parse(c.Param("artifact_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid artifact id"})
		return
	}

	// Verify the authenticated user owns the flow.
	var flow database.Flow
	if err := h.db.Where("id = ? AND user_id = ?", flowID, userID).First(&flow).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "flow not found"})
		return
	}

	// Verify the artifact belongs to this flow (via action -> subtask -> task -> flow).
	var artifact database.Artifact
	result := h.db.Table("artifacts AS ar").
		Select("ar.*").
		Joins("JOIN actions a ON a.id = ar.action_id").
		Joins("JOIN subtasks s ON s.id = a.subtask_id").
		Joins("JOIN tasks t ON t.id = s.task_id").
		Where("ar.id = ? AND t.flow_id = ?", artifactID, flowID).
		First(&artifact)
	if result.Error != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "artifact not found"})
		return
	}

	if artifact.FilePath == nil || *artifact.FilePath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "artifact has no file"})
		return
	}

	// Resolve the relative path against the data root.
	relPath := filepath.Clean(*artifact.FilePath)
	absRoot, err := filepath.Abs(h.dataDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "data directory configuration error"})
		return
	}
	absPath := filepath.Join(absRoot, relPath)

	// Path traversal guard: resolved path must be under the data root.
	if !strings.HasPrefix(absPath, absRoot+string(os.PathSeparator)) && absPath != absRoot {
		c.JSON(http.StatusForbidden, gin.H{"error": "path outside allowed root"})
		return
	}

	// Check file exists.
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
		return
	}

	// Determine content type and disposition from artifact kind.
	contentType := "application/octet-stream"
	disposition := "attachment"
	if artifact.Kind == "browser_screenshot" {
		contentType = "image/png"
		disposition = "inline"
	}

	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", disposition)
	c.File(absPath)
}

func writeSSE(w io.Writer, eventType string, data any) {
	jsonData, _ := json.Marshal(data)
	_, _ = io.WriteString(w, "event: "+eventType+"\n")
	_, _ = io.WriteString(w, "data: "+string(jsonData)+"\n\n")
}
