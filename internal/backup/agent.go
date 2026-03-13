package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/danmartell-ventures/apex-agent/internal/config"
)

// Agent handles nightly backups.
type Agent struct {
	cfg    config.Config
	log    *slog.Logger
	client *http.Client
}

func NewAgent(cfg config.Config, log *slog.Logger) *Agent {
	return &Agent{
		cfg: cfg,
		log: log.With("component", "backup"),
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// Run starts the backup scheduler.
func (a *Agent) Run(ctx context.Context) error {
	if !a.cfg.Backup.Enabled {
		a.log.Info("backup disabled")
		return nil
	}

	for {
		now := time.Now()
		next := a.nextBackupTime(now)
		wait := next.Sub(now)

		a.log.Info("next backup scheduled", "at", next.Format(time.RFC3339), "in", wait)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
			a.runBackup(ctx)
		}
	}
}

func (a *Agent) nextBackupTime(now time.Time) time.Time {
	hour, minute := a.parseSchedule()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if now.After(next) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

func (a *Agent) parseSchedule() (int, int) {
	var hour, minute int
	fmt.Sscanf(a.cfg.Backup.Schedule, "%d:%d", &hour, &minute)
	return hour, minute
}

func (a *Agent) runBackup(ctx context.Context) {
	a.log.Info("starting backup")

	// Get presigned URL from mothership
	url, err := a.getPresignedURL(ctx)
	if err != nil {
		a.log.Error("failed to get presigned URL", "error", err)
		return
	}

	// Create tar of data dir
	tarPath, err := a.createTar(ctx)
	if err != nil {
		a.log.Error("failed to create tar", "error", err)
		return
	}
	defer os.Remove(tarPath)

	// Upload
	if err := a.upload(ctx, tarPath, url); err != nil {
		a.log.Error("backup upload failed", "error", err)
		return
	}

	// Report completion
	if err := a.reportCompletion(ctx); err != nil {
		a.log.Warn("failed to report backup completion", "error", err)
	}

	a.log.Info("backup completed successfully")
}

func (a *Agent) getPresignedURL(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/api/docker-hosts/%s/backup-url", a.cfg.Server.URL, a.cfg.Server.HostID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Server.ReportingToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.URL, nil
}

func (a *Agent) createTar(ctx context.Context) (string, error) {
	dataDir := a.cfg.Docker.DataDir
	tarPath := filepath.Join(os.TempDir(), fmt.Sprintf("apex-backup-%s.tar.gz", time.Now().Format("20060102")))

	// Get list of instance directories
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return "", fmt.Errorf("reading data dir: %w", err)
	}

	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}

	if len(dirs) == 0 {
		return "", fmt.Errorf("no instance data to backup")
	}

	args := []string{"-czf", tarPath, "-C", dataDir}
	args = append(args, dirs...)

	cmd := exec.CommandContext(ctx, "tar", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("tar: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return tarPath, nil
}

func (a *Agent) upload(ctx context.Context, tarPath, presignedURL string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", presignedURL, f)
	if err != nil {
		return err
	}
	req.ContentLength = stat.Size()
	req.Header.Set("Content-Type", "application/gzip")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("upload failed with status %d", resp.StatusCode)
	}
	return nil
}

func (a *Agent) reportCompletion(ctx context.Context) error {
	url := fmt.Sprintf("%s/api/docker-hosts/%s/backup-complete", a.cfg.Server.URL, a.cfg.Server.HostID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.cfg.Server.ReportingToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// RunNow triggers an immediate backup.
func (a *Agent) RunNow(ctx context.Context) {
	a.runBackup(ctx)
}
