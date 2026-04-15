package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/yourorg/sentrix/internal/provider"
)

const (
	defaultTerminalTimeout = 60 * time.Second
	maxTerminalTimeout     = 20 * time.Minute
)

func terminalDef() provider.ToolDef {
	return toolSchema("terminal_exec",
		"Execute a shell command in blocking mode with configurable timeout. "+
			"Only one command can be executed at a time. Use for running security tools, scripts, and system commands.",
		map[string]interface{}{
			"command":     prop("string", "The shell command to execute"),
			"working_dir": prop("string", "Working directory for the command (optional)"),
			"timeout":     prop("integer", "Execution timeout in seconds (default 60, max 1200)"),
			"detach":      prop("boolean", "If true, run command in background and return immediately"),
			"message":     prop("string", "Brief description of what this command does"),
		},
		[]string{"command", "message"},
	)
}

func (r *ToolRegistry) handleTerminal(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args TerminalArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("parse terminal args: %w", err)
	}

	r.logTerminal("stdin", args.Command, args.Command)

	timeout := defaultTerminalTimeout
	if args.Timeout.Int() > 0 {
		timeout = time.Duration(args.Timeout.Int()) * time.Second
		if timeout > maxTerminalTimeout {
			timeout = maxTerminalTimeout
		}
	}

	// Route through sandbox if available.
	if r.sandbox != nil && r.containerID != "" {
		cmd := []string{"sh", "-c", args.Command}
		execResult, err := r.sandbox.Exec(ctx, r.containerID, cmd, timeout)
		if err != nil {
			r.logTerminal("stderr", args.Command, fmt.Sprintf("sandbox terminal error: %v", err))
			return "", fmt.Errorf("sandbox terminal: %w", err)
		}
		if execResult.Stdout != "" {
			r.logTerminal("stdout", args.Command, execResult.Stdout)
		}
		if execResult.Stderr != "" {
			r.logTerminal("stderr", args.Command, execResult.Stderr)
		}

		var result strings.Builder
		if execResult.Stdout != "" {
			result.WriteString(execResult.Stdout)
		}
		if execResult.Stderr != "" {
			if result.Len() > 0 {
				result.WriteString("\n--- STDERR ---\n")
			}
			result.WriteString(execResult.Stderr)
		}
		if execResult.TimedOut {
			result.WriteString(fmt.Sprintf("\n\nCommand timed out after %v", timeout))
		} else if execResult.ExitCode != 0 {
			result.WriteString(fmt.Sprintf("\n\nExit code: %d", execResult.ExitCode))
		}
		if result.Len() == 0 {
			r.logTerminal("status", args.Command, "Command completed successfully with exit code 0. No output produced.")
			return "Command completed successfully with exit code 0. No output produced.", nil
		}
		return result.String(), nil
	}

	// Direct host execution fallback.
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", args.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", args.Command)
	}

	if args.WorkingDir != "" {
		cmd.Dir = args.WorkingDir
	}

	if args.Detach.Bool() {
		if err := cmd.Start(); err != nil {
			r.logTerminal("stderr", args.Command, fmt.Sprintf("start command error: %v", err))
			return "", fmt.Errorf("start command: %w", err)
		}
		r.logTerminal("status", args.Command, fmt.Sprintf("Command started in background (PID %d): %s", cmd.Process.Pid, args.Command))
		return fmt.Sprintf("Command started in background (PID %d): %s", cmd.Process.Pid, args.Command), nil
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(execCtx, "cmd", "/C", args.Command)
	} else {
		cmd = exec.CommandContext(execCtx, "sh", "-c", args.Command)
	}
	if args.WorkingDir != "" {
		cmd.Dir = args.WorkingDir
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	var result strings.Builder
	if stdout.Len() > 0 {
		result.WriteString(stdout.String())
	}
	if stderr.Len() > 0 {
		r.logTerminal("stderr", args.Command, stderr.String())
		if result.Len() > 0 {
			result.WriteString("\n--- STDERR ---\n")
		}
		result.WriteString(stderr.String())
	}
	if stdout.Len() > 0 {
		r.logTerminal("stdout", args.Command, stdout.String())
	}

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			r.logTerminal("status", args.Command, fmt.Sprintf("Command timed out after %v", timeout))
			result.WriteString(fmt.Sprintf("\n\nCommand timed out after %v", timeout))
		} else {
			r.logTerminal("status", args.Command, fmt.Sprintf("Exit error: %v", err))
			result.WriteString(fmt.Sprintf("\n\nExit error: %v", err))
		}
	}

	if result.Len() == 0 {
		r.logTerminal("status", args.Command, "Command completed successfully with exit code 0. No output produced.")
		return "Command completed successfully with exit code 0. No output produced.", nil
	}

	return result.String(), nil
}
