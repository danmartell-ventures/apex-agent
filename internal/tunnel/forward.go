package tunnel

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ParseForwardsFile reads a forwards.conf file.
// Format: one line per forward: "remotePort localHost:localPort"
// Example: "41001 127.0.0.1:8080"
func ParseForwardsFile(path string) ([]Forward, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var forwards []Forward
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fwd, err := parseForwardLine(line)
		if err != nil {
			continue // Skip malformed lines
		}
		forwards = append(forwards, fwd)
	}

	return forwards, scanner.Err()
}

// WriteForwardsFile writes forwards to a file.
func WriteForwardsFile(path string, forwards []Forward) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	for _, fwd := range forwards {
		fmt.Fprintf(f, "%d %s:%d\n", fwd.RemotePort, fwd.LocalHost, fwd.LocalPort)
	}
	return nil
}

func parseForwardLine(line string) (Forward, error) {
	parts := strings.Fields(line)
	if len(parts) != 2 {
		return Forward{}, fmt.Errorf("expected 2 fields, got %d", len(parts))
	}

	remotePort, err := strconv.Atoi(parts[0])
	if err != nil {
		return Forward{}, fmt.Errorf("invalid remote port: %w", err)
	}

	host, portStr, err := splitHostPort(parts[1])
	if err != nil {
		return Forward{}, err
	}

	localPort, err := strconv.Atoi(portStr)
	if err != nil {
		return Forward{}, fmt.Errorf("invalid local port: %w", err)
	}

	return Forward{
		RemotePort: remotePort,
		LocalHost:  host,
		LocalPort:  localPort,
	}, nil
}

func splitHostPort(s string) (string, string, error) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", "", fmt.Errorf("missing port in %q", s)
	}
	return s[:i], s[i+1:], nil
}
