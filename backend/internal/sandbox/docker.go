package sandbox

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"github.com/yourorg/sentrix/internal/config"
)

const (
	labelManagedBy = "sentrix.managed-by"
	labelFlowID    = "sentrix.flow-id"
	managedValue   = "sentrix"
)

// DockerClient implements Client using the Docker Engine API.
type DockerClient struct {
	docker  *dockerclient.Client
	cfg     config.DockerConfig
	mu      sync.Mutex
	pool    map[uuid.UUID]string // flowID -> containerID
	network string               // cached network ID
}

// NewDockerClient creates a Docker-backed sandbox client.
func NewDockerClient(cfg config.DockerConfig) (*DockerClient, error) {
	opts := []dockerclient.Opt{
		dockerclient.WithAPIVersionNegotiation(),
	}
	if cfg.SocketPath != "" {
		opts = append(opts, dockerclient.WithHost("unix://"+cfg.SocketPath))
	} else {
		opts = append(opts, dockerclient.FromEnv)
	}

	cli, err := dockerclient.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	// Verify connectivity.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		return nil, fmt.Errorf("docker ping failed: %w", err)
	}

	dc := &DockerClient{
		docker: cli,
		cfg:    cfg,
		pool:   make(map[uuid.UUID]string),
	}

	// Ensure the sandbox network exists.
	if err := dc.ensureNetwork(ctx); err != nil {
		cli.Close()
		return nil, err
	}

	return dc, nil
}

// ensureNetwork creates the sandbox Docker network if it doesn't exist.
func (dc *DockerClient) ensureNetwork(ctx context.Context) error {
	name := dc.cfg.Network

	networks, err := dc.docker.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", name)),
	})
	if err != nil {
		return fmt.Errorf("list networks: %w", err)
	}
	for _, n := range networks {
		if n.Name == name {
			dc.network = n.ID
			return nil
		}
	}

	resp, err := dc.docker.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{labelManagedBy: managedValue},
	})
	if err != nil {
		return fmt.Errorf("create network %s: %w", name, err)
	}
	dc.network = resp.ID
	log.Infof("sandbox: created network %s (%s)", name, resp.ID[:12])
	return nil
}

// EnsureContainer returns the container for the given flow, creating one if needed.
func (dc *DockerClient) EnsureContainer(ctx context.Context, flowID uuid.UUID) (string, error) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if id, ok := dc.pool[flowID]; ok {
		return id, nil
	}

	id, err := dc.createContainer(ctx, flowID)
	if err != nil {
		return "", err
	}
	dc.pool[flowID] = id
	return id, nil
}

// createContainer builds and starts a new sandboxed container.
func (dc *DockerClient) createContainer(ctx context.Context, flowID uuid.UUID) (string, error) {
	// Pull image if not present.
	if err := dc.ensureImage(ctx); err != nil {
		return "", err
	}

	name := fmt.Sprintf("sentrix-flow-%s", flowID.String()[:8])

	cpuQuota := int64(dc.cfg.CPULimit * 100000)
	memLimit := int64(dc.cfg.MemoryLimitMB) * 1024 * 1024
	pidsLimit := int64(256)

	containerCfg := &container.Config{
		Image:      dc.cfg.DefaultImage,
		Hostname:   name,
		WorkingDir: "/work",
		User:       "root",
		Labels: map[string]string{
			labelManagedBy: managedValue,
			labelFlowID:    flowID.String(),
		},
		Tty: false,
		// Keep the container alive with a long sleep so we can exec into it.
		Cmd: []string{"sleep", "infinity"},
	}

	hostCfg := &container.HostConfig{
		Resources: container.Resources{
			CPUQuota:  cpuQuota,
			CPUPeriod: 100000,
			Memory:    memLimit,
			PidsLimit: &pidsLimit,
		},
		CapDrop: []string{"ALL"},
		CapAdd:  []string{"NET_RAW", "NET_ADMIN", "DAC_OVERRIDE"}, // NET_RAW/NET_ADMIN for nmap SYN scan, tcpdump; DAC_OVERRIDE for root to write to operator-owned /work
		// NOTE: no-new-privileges is intentionally omitted. It prevents
		// execve from honoring file capabilities, which breaks nmap,
		// masscan, tcpdump, and other Kali tools that ship with
		// cap_net_raw+ep on their binaries. Security is maintained by
		// dropping ALL capabilities and adding back only NET_RAW and
		// NET_ADMIN, plus resource limits and pids cap.
		LogConfig: container.LogConfig{
			Type: "json-file",
			Config: map[string]string{
				"max-size": "10m",
				"max-file": "3",
			},
		},
	}

	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			dc.cfg.Network: {},
		},
	}

	resp, err := dc.docker.ContainerCreate(ctx, containerCfg, hostCfg, netCfg, nil, name)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	if err := dc.docker.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Clean up the created container.
		dc.docker.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("start container: %w", err)
	}

	log.Infof("sandbox: started container %s for flow %s", resp.ID[:12], flowID.String()[:8])
	return resp.ID, nil
}

// ensureImage pulls the configured image if it isn't already present locally.
func (dc *DockerClient) ensureImage(ctx context.Context) error {
	_, _, err := dc.docker.ImageInspectWithRaw(ctx, dc.cfg.DefaultImage)
	if err == nil {
		return nil // image exists
	}

	log.Infof("sandbox: pulling image %s ...", dc.cfg.DefaultImage)
	reader, err := dc.docker.ImagePull(ctx, dc.cfg.DefaultImage, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", dc.cfg.DefaultImage, err)
	}
	defer reader.Close()
	// Consume the pull output to completion.
	io.Copy(io.Discard, reader)
	log.Infof("sandbox: image %s ready", dc.cfg.DefaultImage)
	return nil
}

// Exec runs a command inside a running container.
func (dc *DockerClient) Exec(ctx context.Context, containerID string, cmd []string, timeout time.Duration) (ExecResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	execCfg := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
		User:         "root",
	}

	execResp, err := dc.docker.ContainerExecCreate(execCtx, containerID, execCfg)
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec create: %w", err)
	}

	attachResp, err := dc.docker.ContainerExecAttach(execCtx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return ExecResult{}, fmt.Errorf("exec attach: %w", err)
	}
	defer attachResp.Close()

	start := time.Now()

	var stdoutBuf, stderrBuf strings.Builder

	// stdcopy.StdCopy blocks on the reader and does not respect context
	// cancellation. Run it in a goroutine and close the attach stream on
	// timeout to unblock it.
	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, attachResp.Reader)
		copyDone <- copyErr
	}()

	select {
	case err = <-copyDone:
		// Copy finished normally.
	case <-execCtx.Done():
		// Timeout — close the stream to unblock StdCopy, then wait for
		// the goroutine to finish so we can read partial output.
		attachResp.Close()
		<-copyDone
		duration := time.Since(start)
		return ExecResult{
			Stdout:   strings.TrimSpace(stdoutBuf.String()),
			Stderr:   strings.TrimSpace(stderrBuf.String()),
			Duration: duration,
			TimedOut: true,
			ExitCode: -1,
		}, nil
	}

	duration := time.Since(start)

	result := ExecResult{
		Stdout:   strings.TrimSpace(stdoutBuf.String()),
		Stderr:   strings.TrimSpace(stderrBuf.String()),
		Duration: duration,
	}

	if err != nil {
		return result, fmt.Errorf("read exec output: %w", err)
	}

	// Get exit code.
	inspectResp, err := dc.docker.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return result, fmt.Errorf("exec inspect: %w", err)
	}
	result.ExitCode = inspectResp.ExitCode

	return result, nil
}

// ReleaseContainer stops and removes the container for a flow.
func (dc *DockerClient) ReleaseContainer(ctx context.Context, flowID uuid.UUID) error {
	dc.mu.Lock()
	id, ok := dc.pool[flowID]
	if ok {
		delete(dc.pool, flowID)
	}
	dc.mu.Unlock()

	if !ok {
		return nil
	}

	return dc.removeContainer(ctx, id)
}

func (dc *DockerClient) removeContainer(ctx context.Context, id string) error {
	timeout := 10
	dc.docker.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
	if err := dc.docker.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove container %s: %w", id[:12], err)
	}
	log.Infof("sandbox: removed container %s", id[:12])
	return nil
}

// CleanupOrphans removes any sentrix-managed containers from a previous run.
func (dc *DockerClient) CleanupOrphans(ctx context.Context) error {
	containers, err := dc.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", labelManagedBy+"="+managedValue)),
	})
	if err != nil {
		return fmt.Errorf("list orphan containers: %w", err)
	}

	for _, c := range containers {
		log.Infof("sandbox: cleaning up orphan container %s (%s)", c.ID[:12], c.Names)
		dc.removeContainer(ctx, c.ID)
	}

	if len(containers) > 0 {
		log.Infof("sandbox: cleaned up %d orphan containers", len(containers))
	}
	return nil
}

// Close releases the Docker client connection.
func (dc *DockerClient) Close() error {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	// Release all active containers.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for flowID, id := range dc.pool {
		dc.removeContainer(ctx, id)
		delete(dc.pool, flowID)
	}

	return dc.docker.Close()
}
