package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"
)

const (
	ServiceLabel = "host.apex.agent"
	plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>run</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/launchd-stdout.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/launchd-stderr.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    </dict>
</dict>
</plist>`
)

// LegacyPlists are the old per-script plist labels to remove.
var LegacyPlists = []string{
	"com.apex.tunnel",
	"com.apex.telemetry",
	"com.apex.backup",
}

type plistData struct {
	Label      string
	BinaryPath string
	LogDir     string
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", ServiceLabel+".plist")
}

// InstallService installs and loads the launchd plist.
func InstallService(binaryPath, logDir string) error {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	data := plistData{
		Label:      ServiceLabel,
		BinaryPath: binaryPath,
		LogDir:     logDir,
	}

	tmpl, err := template.New("plist").Parse(plistTemplate)
	if err != nil {
		return err
	}

	path := plistPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return err
	}

	// Load the service
	return exec.Command("launchctl", "load", path).Run()
}

// UninstallService stops and removes the launchd plist.
func UninstallService() error {
	path := plistPath()
	exec.Command("launchctl", "unload", path).Run() // Ignore error if not loaded
	return os.Remove(path)
}

// RemoveLegacyPlists unloads and removes old per-script plists.
func RemoveLegacyPlists() {
	home, _ := os.UserHomeDir()
	agentsDir := filepath.Join(home, "Library", "LaunchAgents")

	for _, label := range LegacyPlists {
		path := filepath.Join(agentsDir, label+".plist")
		if _, err := os.Stat(path); err == nil {
			exec.Command("launchctl", "unload", path).Run()
			os.Remove(path)
		}
	}
}

// IsServiceLoaded checks if the launchd service is running.
func IsServiceLoaded() bool {
	out, err := exec.Command("launchctl", "list", ServiceLabel).Output()
	return err == nil && len(out) > 0
}

// RestartService restarts the agent via launchd.
func RestartService() error {
	path := plistPath()
	if err := exec.Command("launchctl", "unload", path).Run(); err != nil {
		return fmt.Errorf("unload: %w", err)
	}
	return exec.Command("launchctl", "load", path).Run()
}
