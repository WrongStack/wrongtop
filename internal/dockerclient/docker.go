// Package dockerclient wraps the Docker Engine SDK for wrongtop: one
// call to list containers with live stats, lifecycle actions and log
// streaming. A missing daemon is not fatal — callers surface a notice.
package dockerclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// UpdateMsg carries docker polling results to the UI.
type UpdateMsg struct {
	Client     *Client
	Containers []Container
	Err        error
}

// Client talks to a local Docker daemon.
type Client struct {
	cli *client.Client
}

// New connects to the daemon (DOCKER_HOST or the default socket/pipe)
// and verifies it answers. The returned client must be Closed.
func New() (*Client, error) {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("docker daemon unreachable: %w", err)
	}
	return &Client{cli: cli}, nil
}

// Close releases the connection.
func (c *Client) Close() error { return c.cli.Close() }

// Container is wrongtop's view of one container.
type Container struct {
	ID     string
	Name   string
	Image  string
	State  string
	Status string
	CPU    float64 // percent of all cores
	Mem    uint64  // bytes
	MemPct float64
	NetRx  uint64 // bytes, cumulative
	NetTx  uint64
	BlkR   uint64 // bytes, cumulative
	BlkW   uint64
}

// List returns all containers; running ones carry live stats.
func (c *Client) List(ctx context.Context) ([]Container, error) {
	res, err := c.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}
	out := make([]Container, 0, len(res.Items))
	for _, s := range res.Items {
		ct := Container{
			ID:     s.ID,
			Image:  s.Image,
			State:  string(s.State),
			Status: s.Status,
		}
		if len(s.Names) > 0 {
			ct.Name = strings.TrimPrefix(s.Names[0], "/")
		}
		if s.State == container.StateRunning {
			// stats are best-effort; zeros are fine when they fail
			_ = c.fillStats(ctx, ct.ID, &ct)
		}
		ct.ID = ct.ID[:min(12, len(ct.ID))]
		out = append(out, ct)
	}
	return out, nil
}

func (c *Client) fillStats(ctx context.Context, id string, ct *Container) error {
	res, err := c.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{
		Stream:                false,
		IncludePreviousSample: true,
	})
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()

	var st container.StatsResponse
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		return err
	}

	cpuDelta := float64(st.CPUStats.CPUUsage.TotalUsage) - float64(st.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(st.CPUStats.SystemUsage) - float64(st.PreCPUStats.SystemUsage)
	if sysDelta > 0 && cpuDelta > 0 {
		online := float64(st.CPUStats.OnlineCPUs)
		if online == 0 {
			online = float64(len(st.CPUStats.CPUUsage.PercpuUsage))
		}
		ct.CPU = cpuDelta / sysDelta * online * 100
	}

	// cgroup v2 reports page cache in inactive_file; exclude it so idle
	// containers do not show inflated memory.
	mem := st.MemoryStats.Usage
	if inact, ok := st.MemoryStats.Stats["inactive_file"]; ok && inact < mem {
		mem -= inact
	}
	ct.Mem = mem
	if st.MemoryStats.Limit > 0 {
		ct.MemPct = float64(mem) / float64(st.MemoryStats.Limit) * 100
	}

	for _, n := range st.Networks {
		ct.NetRx += n.RxBytes
		ct.NetTx += n.TxBytes
	}
	for _, b := range st.BlkioStats.IoServiceBytesRecursive {
		switch b.Op {
		case "read", "Read":
			ct.BlkR += b.Value
		case "write", "Write":
			ct.BlkW += b.Value
		}
	}
	return nil
}

// Start starts a stopped container.
func (c *Client) Start(ctx context.Context, id string) error {
	_, err := c.cli.ContainerStart(ctx, id, client.ContainerStartOptions{})
	return err
}

// Stop stops a container, waiting up to 10s before SIGKILL.
func (c *Client) Stop(ctx context.Context, id string) error {
	_, err := c.cli.ContainerStop(ctx, id, client.ContainerStopOptions{})
	return err
}

// Restart restarts a container.
func (c *Client) Restart(ctx context.Context, id string) error {
	_, err := c.cli.ContainerRestart(ctx, id, client.ContainerRestartOptions{})
	return err
}

// Logs opens the container log stream (multiplexed unless the container
// has a TTY). Cancel ctx to stop following.
func (c *Client) Logs(ctx context.Context, id string, follow bool) (io.ReadCloser, bool, error) {
	res, err := c.cli.ContainerLogs(ctx, id, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       "500",
	})
	if err != nil {
		return nil, false, err
	}
	tty := false
	if insp, err := c.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{}); err == nil && insp.Container.Config != nil {
		tty = insp.Container.Config.Tty
	}
	return res, tty, nil
}
