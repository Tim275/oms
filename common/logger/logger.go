package logger

import (
	"log/slog"
	"os"
)

// NewLogger creates a new structured logger with JSON format
func NewLogger(serviceName string) *slog.Logger {
	// Get log level from environment (default: INFO)
	level := getLogLevel(os.Getenv("LOG_LEVEL"))

	opts := &slog.HandlerOptions{
		Level: level,
	}

	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler)

	// Add service name to all log entries
	return logger.With(slog.String("service", serviceName))
}

func getLogLevel(levelStr string) slog.Level {
	switch levelStr {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
