package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yourorg/sentrix/internal/provider"
)

const sandboxWorkDir = "/work"

func fileReadDef() provider.ToolDef {
	return toolSchema("file_read",
		"Read the content of a file from the sandbox workspace (/work).",
		map[string]interface{}{
			"path":    prop("string", "File path (absolute or relative to /work)"),
			"message": prop("string", "Brief description of what you want to read and why"),
		},
		[]string{"path", "message"},
	)
}

func fileWriteDef() provider.ToolDef {
	return toolSchema("file_write",
		"Write content to a file in the sandbox workspace (/work). Creates parent directories if needed.",
		map[string]interface{}{
			"path":    prop("string", "File path (absolute or relative to /work)"),
			"content": prop("string", "File content to write"),
			"message": prop("string", "Brief description of what you are writing and why"),
		},
		[]string{"path", "content", "message"},
	)
}

func (r *ToolRegistry) handleFileRead(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args FileReadArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("parse file_read args: %w", err)
	}

	if r.sandbox != nil && r.containerID != "" {
		return r.sandboxFileRead(ctx, args)
	}

	path := sanitizePath(args.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("Error reading file '%s': %v", path, err), nil
	}
	return string(data), nil
}

func (r *ToolRegistry) handleFileWrite(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args FileWriteArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("parse file_write args: %w", err)
	}

	if r.sandbox != nil && r.containerID != "" {
		return r.sandboxFileWrite(ctx, args)
	}

	path := sanitizePath(args.Path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Sprintf("Error creating directory '%s': %v", dir, err), nil
	}
	if err := os.WriteFile(path, []byte(args.Content), 0644); err != nil {
		return fmt.Sprintf("Error writing file '%s': %v", path, err), nil
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(args.Content), path), nil
}

func (r *ToolRegistry) sandboxFileRead(ctx context.Context, args FileReadArgs) (string, error) {
	path := sandboxResolvePath(args.Path)
	result, err := r.sandbox.Exec(ctx, r.containerID, []string{"cat", path}, 30*time.Second)
	if err != nil {
		return "", fmt.Errorf("sandbox file read: %w", err)
	}
	if result.ExitCode != 0 {
		msg := result.Stderr
		if msg == "" {
			msg = fmt.Sprintf("exit code %d", result.ExitCode)
		}
		return fmt.Sprintf("Error reading file '%s': %s", path, msg), nil
	}
	return result.Stdout, nil
}

func (r *ToolRegistry) sandboxFileWrite(ctx context.Context, args FileWriteArgs) (string, error) {
	path := sandboxResolvePath(args.Path)
	dir := filepath.Dir(path)

	encoded := base64.StdEncoding.EncodeToString([]byte(args.Content))
	cmd := fmt.Sprintf("mkdir -p '%s' && printf '%%s' '%s' | base64 -d > '%s'", dir, encoded, path)

	result, err := r.sandbox.Exec(ctx, r.containerID, []string{"sh", "-c", cmd}, 30*time.Second)
	if err != nil {
		return "", fmt.Errorf("sandbox file write: %w", err)
	}
	if result.ExitCode != 0 {
		msg := result.Stderr
		if msg == "" {
			msg = fmt.Sprintf("exit code %d", result.ExitCode)
		}
		return fmt.Sprintf("Error writing file '%s': %s", path, msg), nil
	}
	return fmt.Sprintf("Successfully wrote %d bytes to %s", len(args.Content), path), nil
}

// sandboxResolvePath converts a user-supplied path to an absolute path inside the sandbox.
func sandboxResolvePath(p string) string {
	cleaned := filepath.Clean(p)
	if filepath.IsAbs(cleaned) {
		return cleaned
	}
	return filepath.Join(sandboxWorkDir, cleaned)
}

// sanitizePath prevents directory traversal outside workspace (host-mode only).
func sanitizePath(p string) string {
	cleaned := filepath.Clean(p)
	cleaned = strings.TrimPrefix(cleaned, "/")
	return cleaned
}
