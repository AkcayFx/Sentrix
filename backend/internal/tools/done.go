package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yourorg/sentrix/internal/provider"
)

func doneDef() provider.ToolDef {
	return toolSchema("done",
		"Mark the current agent task as complete and summarize the final outcome.",
		map[string]interface{}{
			"success": prop("boolean", "Whether the task completed successfully"),
			"result":  prop("string", "Final task result or summary"),
			"message": prop("string", "Brief progress message for the user"),
		},
		[]string{"success", "result", "message"},
	)
}

func (r *ToolRegistry) handleDone(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args DoneArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("parse done args: %w", err)
	}

	status := "FAILED"
	if args.Success.Bool() {
		status = "SUCCESS"
	}

	return fmt.Sprintf("Task completion status: %s\n\n%s", status, args.Result), nil
}
