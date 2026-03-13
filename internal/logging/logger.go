package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Setup creates a JSON slog logger that writes to a rotating file and optionally to stderr.
func Setup(logDir, level string, toStderr bool) (*slog.Logger, error) {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	lj := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "agent.log"),
		MaxSize:    10, // MB
		MaxBackups: 3,
		MaxAge:     14, // days
	}

	var w io.Writer = lj
	if toStderr {
		w = io.MultiWriter(lj, os.Stderr)
	}

	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler), nil
}

// LogPath returns the path to the current log file.
func LogPath(logDir string) string {
	return filepath.Join(logDir, "agent.log")
}
