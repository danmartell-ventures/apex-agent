package update

import (
	"context"
	"log/slog"
	"time"

	"github.com/creativeprojects/go-selfupdate"

	"github.com/danmartell-ventures/apex-agent/internal/platform"
	"github.com/danmartell-ventures/apex-agent/pkg/version"
)

const (
	checkInterval = 6 * time.Hour
	repo          = "danmartell-ventures/apex-agent"
)

// Updater handles self-updates from GitHub releases.
type Updater struct {
	log     *slog.Logger
	enabled bool
}

func NewUpdater(enabled bool, log *slog.Logger) *Updater {
	return &Updater{
		log:     log.With("component", "updater"),
		enabled: enabled,
	}
}

// Run periodically checks for updates.
func (u *Updater) Run(ctx context.Context) error {
	if !u.enabled {
		u.log.Info("auto-update disabled")
		return nil
	}

	// Check immediately on start, then every 6 hours
	u.check(ctx)

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			u.check(ctx)
		}
	}
}

// CheckNow performs an immediate update check and returns whether an update was applied.
func (u *Updater) CheckNow(ctx context.Context) (updated bool, newVersion string, err error) {
	return u.doUpdate(ctx)
}

func (u *Updater) check(ctx context.Context) {
	updated, newVer, err := u.doUpdate(ctx)
	if err != nil {
		u.log.Error("update check failed", "error", err)
		return
	}
	if updated {
		u.log.Info("updated successfully, restarting via launchd", "new_version", newVer)
		if err := platform.RestartService(); err != nil {
			u.log.Error("failed to restart after update", "error", err)
		}
	}
}

func (u *Updater) doUpdate(ctx context.Context) (bool, string, error) {
	if version.Version == "dev" {
		u.log.Debug("skipping update check in dev build")
		return false, "", nil
	}

	source, err := selfupdate.NewGitHubSource(selfupdate.GitHubConfig{})
	if err != nil {
		return false, "", err
	}

	updater, err := selfupdate.NewUpdater(selfupdate.Config{
		Source: source,
	})
	if err != nil {
		return false, "", err
	}

	latest, found, err := updater.DetectLatest(ctx, selfupdate.NewRepositorySlug("danmartell-ventures", "apex-agent"))
	if err != nil {
		return false, "", err
	}
	if !found {
		return false, "", nil
	}

	currentVer := version.Version
	// Strip leading 'v' if present for comparison
	if len(currentVer) > 0 && currentVer[0] == 'v' {
		currentVer = currentVer[1:]
	}

	if !latest.GreaterThan(currentVer) {
		u.log.Debug("already up to date", "current", version.Version, "latest", latest.Version())
		return false, "", nil
	}

	u.log.Info("update available", "current", version.Version, "latest", latest.Version())

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return false, "", err
	}

	if err := updater.UpdateTo(ctx, latest, exe); err != nil {
		return false, "", err
	}

	return true, latest.Version(), nil
}
