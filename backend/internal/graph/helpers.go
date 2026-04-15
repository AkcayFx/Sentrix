package graph

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/sentrix/internal/agent"
	"github.com/yourorg/sentrix/internal/auth"
	"github.com/yourorg/sentrix/internal/config"
	"github.com/yourorg/sentrix/internal/database"
	"github.com/yourorg/sentrix/internal/graph/model"
)

// ginCtx extracts the *gin.Context stored in the GraphQL context.
func ginCtx(ctx context.Context) *gin.Context {
	gc, _ := ctx.Value("GinContextKey").(*gin.Context)
	return gc
}

// currentUserID reads the authenticated user's UUID from the Gin context.
func currentUserID(ctx context.Context) uuid.UUID {
	gc := ginCtx(ctx)
	if gc == nil {
		return uuid.Nil
	}
	return auth.GetUserID(gc)
}

func toGQLFlow(f database.Flow, scraper config.ScraperConfig) *model.Flow {
	mode := "native"
	screenshotsEnabled := false
	if scraper.PublicURL != "" || scraper.PrivateURL != "" {
		mode = "scraper"
		screenshotsEnabled = true
	}
	return &model.Flow{
		ID:          f.ID,
		Title:       f.Title,
		Description: f.Description,
		Status:      f.Status,
		Config:      f.Config,
		CreatedAt:   f.CreatedAt,
		UpdatedAt:   f.UpdatedAt,
		BrowserCapabilities: model.BrowserCapabilities{
			Mode:               mode,
			ScreenshotsEnabled: screenshotsEnabled,
		},
	}
}

func toGQLProvider(p database.ProviderConfig) *model.ProviderConfig {
	hasKey := p.APIKeyEncrypted != nil && *p.APIKeyEncrypted != ""
	return &model.ProviderConfig{
		ID:           p.ID,
		ProviderType: p.ProviderType,
		ModelName:    p.ModelName,
		BaseURL:      p.BaseURL,
		IsDefault:    p.IsDefault,
		HasAPIKey:    hasKey,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}

func toGQLToken(t database.APIToken) *model.APIToken {
	return &model.APIToken{
		ID:         t.ID,
		Label:      t.Label,
		LastUsedAt: t.LastUsedAt,
		ExpiresAt:  t.ExpiresAt,
		CreatedAt:  t.CreatedAt,
	}
}

// agentEventToFlowEvent converts a broadcaster Event to a GraphQL FlowEvent.
func agentEventToFlowEvent(evt agent.Event) *model.FlowEvent {
	flowUUID, _ := uuid.Parse(evt.FlowID)
	ts := evt.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	fe := &model.FlowEvent{
		FlowID:    flowUUID,
		Status:    evt.Type,
		Timestamp: ts,
	}

	data, _ := evt.Data.(map[string]interface{})
	if data != nil {
		if taskID, ok := data["task_id"].(string); ok {
			tid, err := uuid.Parse(taskID)
			if err == nil {
				fe.TaskID = &tid
			}
		}
		fe.Message = eventDataMessage(evt.Type, data)
	} else {
		fe.Message = evt.Type
	}

	return fe
}

// agentEventToActionEvent converts a broadcaster Event to a GraphQL ActionEvent.
func agentEventToActionEvent(evt agent.Event) *model.ActionEvent {
	ts := evt.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}

	ae := &model.ActionEvent{
		ActionType: evt.Type,
		Timestamp:  ts,
	}

	data, _ := evt.Data.(map[string]interface{})
	if data != nil {
		if sid, ok := data["subtask"].(string); ok {
			ae.SubtaskID, _ = uuid.Parse(sid)
		}
		if evt.Type == agent.EventToolExecuted {
			tool, _ := data["tool"].(string)
			args, _ := data["args"].(string)
			ae.Output = fmt.Sprintf("[%s] %s", tool, args)
			ae.ActionType = tool
		} else {
			content, _ := data["content"].(string)
			ae.Output = content
		}
	}

	return ae
}

// eventDataMessage builds a human-readable message from event data.
func eventDataMessage(eventType string, data map[string]interface{}) string {
	switch eventType {
	case agent.EventFlowStarted:
		title, _ := data["title"].(string)
		return fmt.Sprintf("Flow started: %s", title)
	case agent.EventTaskCreated:
		title, _ := data["title"].(string)
		role, _ := data["agent_role"].(string)
		return fmt.Sprintf("Task created: %s [%s]", title, role)
	case agent.EventSubtaskStarted:
		role, _ := data["agent_role"].(string)
		return fmt.Sprintf("Subtask started [%s]", role)
	case agent.EventActionCompleted:
		agentName, _ := data["agent"].(string)
		return fmt.Sprintf("Agent %s completed action", agentName)
	case agent.EventToolExecuted:
		tool, _ := data["tool"].(string)
		return fmt.Sprintf("Tool executed: %s", tool)
	case agent.EventSubtaskCompleted:
		role, _ := data["agent_role"].(string)
		status, _ := data["status"].(string)
		return fmt.Sprintf("Subtask [%s] %s", role, status)
	case agent.EventTaskCompleted:
		title, _ := data["title"].(string)
		status, _ := data["status"].(string)
		return fmt.Sprintf("Task completed: %s (%s)", title, status)
	case agent.EventFlowCompleted:
		return "Flow completed"
	case agent.EventFlowFailed:
		errMsg, _ := data["error"].(string)
		return fmt.Sprintf("Flow failed: %s", errMsg)
	case agent.EventFlowStopped:
		return "Flow stopped"
	default:
		return eventType
	}
}
