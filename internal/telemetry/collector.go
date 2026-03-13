package telemetry

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/danmartell-ventures/apex-agent/internal/container"
	"github.com/danmartell-ventures/apex-agent/pkg/version"
)

// HostInfo contains system-level metrics.
type HostInfo struct {
	Hostname     string  `json:"hostname"`
	Platform     string  `json:"platform"`
	CPUCount     int     `json:"cpu_count"`
	MemTotalMB   int     `json:"mem_total_mb"`
	MemUsedMB    int     `json:"mem_used_mb"`
	DiskTotalGB  int     `json:"disk_total_gb"`
	DiskUsedGB   int     `json:"disk_used_gb"`
	LoadAvg      string  `json:"load_avg"`
	Uptime       string  `json:"uptime"`
	AgentVersion string  `json:"agent_version"`
}

// ContainerInfo contains per-container metrics for telemetry.
type ContainerInfo struct {
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	CPU     float64 `json:"cpu_percent"`
	MemMB   float64 `json:"mem_mb"`
}

// Payload is the telemetry report sent to the mothership.
type Payload struct {
	HostID        string          `json:"host_id"`
	Token         string          `json:"token"`
	Host          HostInfo        `json:"host"`
	Containers    []ContainerInfo `json:"containers"`
	ScriptVersion int             `json:"script_version"`
	Timestamp     string          `json:"timestamp"`
}

// Collect gathers system and container metrics.
func Collect(ctx context.Context, hostID, token string, containers []container.ContainerStatus) Payload {
	host := collectHostInfo()

	var cInfos []ContainerInfo
	for _, c := range containers {
		status := "stopped"
		if c.Running {
			status = "running"
		}
		cInfos = append(cInfos, ContainerInfo{
			Name:   c.Name,
			Status: status,
			CPU:    c.CPU,
			MemMB:  c.MemMB,
		})
	}

	return Payload{
		HostID:        hostID,
		Token:         token,
		Host:          host,
		Containers:    cInfos,
		ScriptVersion: 100, // Agent identified by version >= 100
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
	}
}

func collectHostInfo() HostInfo {
	hostname, _ := os.Hostname()

	info := HostInfo{
		Hostname:     hostname,
		Platform:     runtime.GOOS + "/" + runtime.GOARCH,
		CPUCount:     runtime.NumCPU(),
		AgentVersion: version.Version,
	}

	// Memory (macOS: sysctl)
	if out, err := execCmd("sysctl", "-n", "hw.memsize"); err == nil {
		var bytes int64
		fmt.Sscanf(strings.TrimSpace(out), "%d", &bytes)
		info.MemTotalMB = int(bytes / 1024 / 1024)
	}

	// Memory used (vm_stat)
	if out, err := execCmd("vm_stat"); err == nil {
		info.MemUsedMB = parseVMStatUsed(out, info.MemTotalMB)
	}

	// Disk
	if out, err := execCmd("df", "-g", "/"); err == nil {
		total, used := parseDfOutput(out)
		info.DiskTotalGB = total
		info.DiskUsedGB = used
	}

	// Load average
	if out, err := execCmd("sysctl", "-n", "vm.loadavg"); err == nil {
		info.LoadAvg = strings.TrimSpace(out)
	}

	// Uptime
	if out, err := execCmd("uptime"); err == nil {
		info.Uptime = strings.TrimSpace(out)
	}

	return info
}

func execCmd(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	return string(out), err
}

func parseVMStatUsed(output string, totalMB int) int {
	// Rough approximation: total - free - inactive
	var pagesFree, pagesInactive int64
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Pages free:") {
			fmt.Sscanf(line, "Pages free: %d", &pagesFree)
		}
		if strings.HasPrefix(line, "Pages inactive:") {
			fmt.Sscanf(line, "Pages inactive: %d", &pagesInactive)
		}
	}
	// macOS page size is 16384 on Apple Silicon, 4096 on Intel
	pageSize := int64(16384)
	if runtime.GOARCH == "amd64" {
		pageSize = 4096
	}
	freeMB := (pagesFree + pagesInactive) * pageSize / 1024 / 1024
	used := totalMB - int(freeMB)
	if used < 0 {
		used = 0
	}
	return used
}

func parseDfOutput(output string) (total, used int) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return
	}
	fields := strings.Fields(lines[1])
	if len(fields) >= 4 {
		fmt.Sscanf(fields[1], "%d", &total)
		fmt.Sscanf(fields[2], "%d", &used)
	}
	return
}
