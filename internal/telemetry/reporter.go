package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/danmartell-ventures/apex-agent/internal/config"
	"github.com/danmartell-ventures/apex-agent/internal/container"
)

const reportInterval = 15 * time.Second

// Reporter sends telemetry to the mothership.
type Reporter struct {
	cfg     config.ServerConfig
	monitor *container.Monitor
	log     *slog.Logger
	client  *http.Client
}

func NewReporter(cfg config.ServerConfig, monitor *container.Monitor, log *slog.Logger) *Reporter {
	return &Reporter{
		cfg:     cfg,
		monitor: monitor,
		log:     log.With("component", "telemetry"),
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Run starts the telemetry loop.
func (r *Reporter) Run(ctx context.Context) error {
	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()

	// Initial report
	r.report(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			r.report(ctx)
		}
	}
}

func (r *Reporter) report(ctx context.Context) {
	containers := r.monitor.Containers()
	payload := Collect(ctx, r.cfg.HostID, r.cfg.ReportingToken, containers)

	data, err := json.Marshal(payload)
	if err != nil {
		r.log.Error("failed to marshal telemetry", "error", err)
		return
	}

	url := fmt.Sprintf("%s/api/docker-hosts/telemetry", r.cfg.URL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(data))
	if err != nil {
		r.log.Error("failed to create request", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		r.log.Debug("telemetry report failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		r.log.Warn("telemetry report rejected", "status", resp.StatusCode)
	}
}
