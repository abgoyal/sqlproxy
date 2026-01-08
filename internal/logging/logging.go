package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	levelVar = new(slog.LevelVar) // For runtime level changes
	closer   io.Closer           // To close lumberjack on shutdown
)

// Init initializes the global logger
// If filePath is empty, logs to stdout; otherwise logs to file with rotation
func Init(level, filePath string, maxSizeMB, maxBackups, maxAgeDays int) error {
	levelVar.Set(parseLevel(level))

	var w io.Writer
	if filePath == "" {
		w = os.Stdout
	} else {
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return err
		}
		lj := &lumberjack.Logger{
			Filename:   filePath,
			MaxSize:    maxSizeMB,
			MaxBackups: maxBackups,
			MaxAge:     maxAgeDays,
			Compress:   true,
			LocalTime:  true,
		}
		w = lj
		closer = lj
	}

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: levelVar,
	})
	slog.SetDefault(slog.New(handler))
	return nil
}

// SetLevel changes log level at runtime
func SetLevel(level string) {
	levelVar.Set(parseLevel(level))
}

// Close closes the log file if any
func Close() error {
	if closer != nil {
		return closer.Close()
	}
	return nil
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug", "DEBUG":
		return slog.LevelDebug
	case "warn", "WARN", "warning", "WARNING":
		return slog.LevelWarn
	case "error", "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Convenience wrappers matching our existing call sites
func Debug(msg string, fields map[string]any) { slog.Debug(msg, mapToAttrs(fields)...) }
func Info(msg string, fields map[string]any)  { slog.Info(msg, mapToAttrs(fields)...) }
func Warn(msg string, fields map[string]any)  { slog.Warn(msg, mapToAttrs(fields)...) }
func Error(msg string, fields map[string]any) { slog.Error(msg, mapToAttrs(fields)...) }

func mapToAttrs(m map[string]any) []any {
	if m == nil {
		return nil
	}
	attrs := make([]any, 0, len(m)*2)
	for k, v := range m {
		attrs = append(attrs, k, v)
	}
	return attrs
}
