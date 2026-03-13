package platform

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CheckSSH verifies that local SSH is accessible on port 22.
func CheckSSH() error {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:22", 3*time.Second)
	if err != nil {
		return fmt.Errorf("SSH not accessible on port 22: %w\nEnable Remote Login in System Settings > General > Sharing", err)
	}
	conn.Close()
	return nil
}

// InstallAuthorizedKey adds a public key to ~/.ssh/authorized_keys.
func InstallAuthorizedKey(pubKey string) error {
	home, _ := os.UserHomeDir()
	sshDir := filepath.Join(home, ".ssh")

	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return err
	}

	authKeysPath := filepath.Join(sshDir, "authorized_keys")

	// Read existing
	existing, _ := os.ReadFile(authKeysPath)

	// Check if key already present
	if len(existing) > 0 {
		for _, line := range splitLines(string(existing)) {
			if line == pubKey {
				return nil // Already installed
			}
		}
	}

	// Append
	f, err := os.OpenFile(authKeysPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "%s\n", pubKey)
	return err
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// DetectHardware returns system specs.
func DetectHardware() (cpuCores int, ramMB int, diskGB int, err error) {
	// CPU cores
	out, err := exec.Command("sysctl", "-n", "hw.ncpu").Output()
	if err == nil {
		fmt.Sscanf(string(out), "%d", &cpuCores)
	}

	// RAM
	out, err = exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err == nil {
		var bytes int64
		fmt.Sscanf(string(out), "%d", &bytes)
		ramMB = int(bytes / 1024 / 1024)
	}

	// Disk
	out, err = exec.Command("df", "-g", "/").Output()
	if err == nil {
		lines := splitLines(string(out))
		if len(lines) >= 2 {
			var total int
			fmt.Sscanf(lines[1], "%*s %d", &total)
			diskGB = total
		}
	}

	return cpuCores, ramMB, diskGB, nil
}
