package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yourorg/sentrix/internal/provider"
)

func reportFindingDef() provider.ToolDef {
	return toolSchema("report_finding",
		"Report a security finding or vulnerability discovered during the assessment.",
		map[string]interface{}{
			"severity": propEnum("Severity level of the finding",
				[]string{"critical", "high", "medium", "low", "info"}),
			"title":       prop("string", "Short title for the finding"),
			"description": prop("string", "Detailed description of the vulnerability"),
			"evidence":    prop("string", "Evidence or proof of the vulnerability (command output, screenshots, etc.)"),
			"remediation": prop("string", "Recommended remediation steps"),
			"message":     prop("string", "Brief summary to display to the user"),
		},
		[]string{"severity", "title", "description", "message"},
	)
}

func (r *ToolRegistry) handleReportFinding(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args ReportFindingArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("parse report_finding args: %w", err)
	}

	id := r.addFinding(args)

	return fmt.Sprintf("✅ Finding #%d recorded: [%s] %s\n\nSeverity: %s\nDescription: %s",
		id, severityBadge(args.Severity), args.Title, args.Severity, args.Description), nil
}

func severityBadge(sev string) string {
	switch sev {
	case "critical":
		return "🔴 CRITICAL"
	case "high":
		return "🟠 HIGH"
	case "medium":
		return "🟡 MEDIUM"
	case "low":
		return "🔵 LOW"
	case "info":
		return "⚪ INFO"
	default:
		return sev
	}
}
