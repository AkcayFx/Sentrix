package sandbox

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ExecResult holds the output of a command executed inside a container.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
	TimedOut bool
}

// Client defines operations for sandboxed container execution.
type Client interface {
	// EnsureContainer returns an existing container for the flow or creates one.
	EnsureContainer(ctx context.Context, flowID uuid.UUID) (containerID string, err error)

	// Exec runs a command inside a container and returns the combined result.
	Exec(ctx context.Context, containerID string, cmd []string, timeout time.Duration) (ExecResult, error)

	// ReleaseContainer stops and removes the container for a flow.
	ReleaseContainer(ctx context.Context, flowID uuid.UUID) error

	// CleanupOrphans removes any sentrix containers left over from a previous run.
	CleanupOrphans(ctx context.Context) error

	// Close releases the underlying Docker client.
	Close() error
}
