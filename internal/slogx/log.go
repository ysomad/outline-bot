package slogx

import (
	"log/slog"
	"os"
	"strings"
)

func Fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
