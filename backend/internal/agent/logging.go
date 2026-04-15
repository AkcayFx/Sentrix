package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/yourorg/sentrix/internal/database"
)

func marshalMetadata(metadata map[string]interface{}) string {
	metaJSON := "{}"
	if metadata != nil {
		if raw, err := json.Marshal(metadata); err == nil {
			metaJSON = string(raw)
		}
	}
	return metaJSON
}

func persistAgentLog(
	ctx context.Context,
	db *gorm.DB,
	flowID uuid.UUID,
	taskID, subtaskID *uuid.UUID,
	agentRole AgentType,
	eventType string,
	message string,
	metadata map[string]interface{},
) {
	if db == nil || strings.TrimSpace(message) == "" {
		return
	}

	record := database.AgentLog{
		FlowID:    flowID,
		TaskID:    taskID,
		SubtaskID: subtaskID,
		AgentRole: string(agentRole),
		EventType: eventType,
		Message:   truncate(message, 4000),
		Metadata:  marshalMetadata(metadata),
	}

	if err := db.WithContext(ctx).Create(&record).Error; err != nil {
		log.WithError(err).Warn("agent: failed to persist agent log")
	}
}

// internalChainTypes are chain types produced by internal planning, recovery,
// and delegation agents. Their system/user scaffolding is noise in the
// default trace; only the assistant response is worth keeping.
var internalChainTypes = map[string]bool{
	"task_generation":      true,
	"task_refinement":      true,
	"task_reporting":       true,
	"recovery_advice":      true,
	"recovery_reflection":  true,
}

func persistMessageChain(
	ctx context.Context,
	db *gorm.DB,
	flowID uuid.UUID,
	taskID, subtaskID *uuid.UUID,
	agentRole AgentType,
	chainType string,
	role string,
	content string,
	tokenCount int,
	metadata map[string]interface{},
) {
	if db == nil || strings.TrimSpace(content) == "" {
		return
	}

	// Never persist system prompts — they are large, deterministic, and
	// reconstructible from the prompt template + context.
	if role == "system" {
		return
	}

	// Skip synthetic user scaffolding for internal planning/recovery agents.
	if role == "user" && internalChainTypes[chainType] {
		return
	}

	record := database.MessageChain{
		FlowID:     &flowID,
		TaskID:     taskID,
		SubtaskID:  subtaskID,
		Role:       role,
		AgentRole:  string(agentRole),
		ChainType:  chainType,
		Content:    truncate(content, 16000),
		TokenCount: tokenCount,
		Metadata:   marshalMetadata(metadata),
	}

	if err := db.WithContext(ctx).Create(&record).Error; err != nil {
		log.WithError(err).Warn("agent: failed to persist message chain")
	}
}

func recordAgentLog(
	ctx context.Context,
	agent *Agent,
	eventType string,
	message string,
	metadata map[string]interface{},
) {
	if agent == nil || agent.DB == nil || strings.TrimSpace(message) == "" {
		return
	}

	persistAgentLog(
		ctx,
		agent.DB,
		agent.FlowID,
		optionalUUID(agent.TaskID),
		optionalUUID(agent.SubtaskID),
		agent.Type,
		eventType,
		message,
		metadata,
	)
}

func recordMessageChain(
	ctx context.Context,
	agent *Agent,
	role string,
	content string,
	tokenCount int,
	metadata map[string]interface{},
) {
	if agent == nil || agent.DB == nil || strings.TrimSpace(content) == "" {
		return
	}

	persistMessageChain(
		ctx,
		agent.DB,
		agent.FlowID,
		optionalUUID(agent.TaskID),
		optionalUUID(agent.SubtaskID),
		agent.Type,
		"subtask_execution",
		role,
		content,
		tokenCount,
		metadata,
	)
}

func optionalUUID(value uuid.UUID) *uuid.UUID {
	if value == uuid.Nil {
		return nil
	}
	v := value
	return &v
}
