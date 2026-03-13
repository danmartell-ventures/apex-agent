package container

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Info represents a running container.
type Info struct {
	ID      string
	Name    string
	Status  string
	Running bool
	Health  string // healthy, unhealthy, starting, none
	CPU     float64
	MemMB   float64
}

// Docker wraps docker CLI commands.
type Docker struct {
	prefix string
}

func NewDocker(prefix string) *Docker {
	return &Docker{prefix: prefix}
}

// ListContainers returns containers matching the prefix.
func (d *Docker) ListContainers(ctx context.Context) ([]Info, error) {
	out, err := d.run(ctx, "ps", "-a", "--filter", "name=^"+d.prefix,
		"--format", `{"id":"{{.ID}}","name":"{{.Names}}","status":"{{.Status}}","state":"{{.State}}"}`)
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}

	if strings.TrimSpace(out) == "" {
		return nil, nil
	}

	var containers []Info
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var raw struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			State string `json:"state"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		containers = append(containers, Info{
			ID:      raw.ID,
			Name:    raw.Name,
			Status:  raw.Status,
			Running: raw.State == "running",
		})
	}

	return containers, nil
}

// InspectHealth returns the health status of a container.
func (d *Docker) InspectHealth(ctx context.Context, name string) (string, error) {
	out, err := d.run(ctx, "inspect", "--format", "{{.State.Health.Status}}", name)
	if err != nil {
		// Container may not have a health check
		return "none", nil
	}
	return strings.TrimSpace(out), nil
}

// ContainerStats returns CPU and memory usage.
func (d *Docker) ContainerStats(ctx context.Context, name string) (cpu float64, memMB float64, err error) {
	out, err := d.run(ctx, "stats", "--no-stream", "--format",
		`{"cpu":"{{.CPUPerc}}","mem":"{{.MemUsage}}"}`, name)
	if err != nil {
		return 0, 0, err
	}

	var raw struct {
		CPU string `json:"cpu"`
		Mem string `json:"mem"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &raw); err != nil {
		return 0, 0, err
	}

	// Parse "12.34%"
	cpuStr := strings.TrimSuffix(raw.CPU, "%")
	fmt.Sscanf(cpuStr, "%f", &cpu)

	// Parse "1.2GiB / 3GiB" or "800MiB / 3GiB"
	parts := strings.Split(raw.Mem, "/")
	if len(parts) >= 1 {
		memMB = parseMemory(strings.TrimSpace(parts[0]))
	}

	return cpu, memMB, nil
}

// Exec runs a command inside a container.
func (d *Docker) Exec(ctx context.Context, name string, cmd ...string) (string, error) {
	args := append([]string{"exec", name}, cmd...)
	return d.run(ctx, args...)
}

// Restart restarts a container.
func (d *Docker) Restart(ctx context.Context, name string) error {
	_, err := d.run(ctx, "restart", name)
	return err
}

// StopAndStart does a stop followed by start.
func (d *Docker) StopAndStart(ctx context.Context, name string) error {
	if _, err := d.run(ctx, "stop", name); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	if _, err := d.run(ctx, "start", name); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	return nil
}

// IsRunning checks if a container is running.
func (d *Docker) IsRunning(ctx context.Context, name string) (bool, error) {
	out, err := d.run(ctx, "inspect", "--format", "{{.State.Running}}", name)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "true", nil
}

func (d *Docker) run(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func parseMemory(s string) float64 {
	s = strings.TrimSpace(s)
	var val float64
	if strings.HasSuffix(s, "GiB") {
		fmt.Sscanf(strings.TrimSuffix(s, "GiB"), "%f", &val)
		return val * 1024
	}
	if strings.HasSuffix(s, "MiB") {
		fmt.Sscanf(strings.TrimSuffix(s, "MiB"), "%f", &val)
		return val
	}
	if strings.HasSuffix(s, "KiB") {
		fmt.Sscanf(strings.TrimSuffix(s, "KiB"), "%f", &val)
		return val / 1024
	}
	return 0
}
