package container

// Recovery strategies are implemented in monitor.go as the escalation loop.
// This file provides additional diagnostic utilities.

import (
	"context"
	"fmt"
	"strings"
)

// DiagnosticResult contains diagnostic info about a container.
type DiagnosticResult struct {
	Name       string
	Running    bool
	Health     string
	DoctorOut  string
	DoctorErr  error
}

// RunDiagnostics runs doctor checks on all containers.
func (m *Monitor) RunDiagnostics(ctx context.Context) ([]DiagnosticResult, error) {
	containers, err := m.docker.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	var results []DiagnosticResult
	for _, c := range containers {
		result := DiagnosticResult{
			Name:    c.Name,
			Running: c.Running,
		}

		if c.Running {
			result.Health, _ = m.docker.InspectHealth(ctx, c.Name)
			out, err := m.docker.Exec(ctx, c.Name, "openclaw", "doctor")
			result.DoctorOut = strings.TrimSpace(out)
			result.DoctorErr = err
		}

		results = append(results, result)
	}

	return results, nil
}
