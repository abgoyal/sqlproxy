package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Level represents log severity
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

func ParseLevel(s string) Level {
	switch s {
	case "debug", "DEBUG":
		return LevelDebug
	case "info", "INFO":
		return LevelInfo
	case "warn", "WARN", "warning", "WARNING":
		return LevelWarn
	case "error", "ERROR":
		return LevelError
	default:
		return LevelInfo
	}
}

// Config for the logger
type Config struct {
	Level       string `yaml:"level"`        // debug, info, warn, error
	FilePath    string `yaml:"file_path"`    // Log file path (empty = stdout only)
	MaxSizeMB   int    `yaml:"max_size_mb"`  // Max size before rotation
	MaxBackups  int    `yaml:"max_backups"`  // Max number of old files to keep
	MaxAgeDays  int    `yaml:"max_age_days"` // Max days to retain old files
	Compress    bool   `yaml:"compress"`     // Compress rotated files
	AlsoStdout  bool   // Also write to stdout (set programmatically, not from YAML)
}

// Logger provides structured, leveled logging with rotation
type Logger struct {
	level  Level
	writer io.Writer
	mu     sync.Mutex

	// For lumberjack rotation
	lumberjack *lumberjack.Logger
}

// Global logger instance
var defaultLogger *Logger

// Init initializes the global logger
func Init(cfg Config) error {
	logger, err := New(cfg)
	if err != nil {
		return err
	}
	defaultLogger = logger
	return nil
}

// New creates a new logger
func New(cfg Config) (*Logger, error) {
	l := &Logger{
		level: ParseLevel(cfg.Level),
	}

	// Set defaults
	if cfg.MaxSizeMB == 0 {
		cfg.MaxSizeMB = 100 // 100MB default
	}
	if cfg.MaxBackups == 0 {
		cfg.MaxBackups = 5
	}
	if cfg.MaxAgeDays == 0 {
		cfg.MaxAgeDays = 30
	}

	if cfg.FilePath != "" {
		// Ensure directory exists
		dir := filepath.Dir(cfg.FilePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create log directory: %w", err)
		}

		l.lumberjack = &lumberjack.Logger{
			Filename:   cfg.FilePath,
			MaxSize:    cfg.MaxSizeMB,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAgeDays,
			Compress:   cfg.Compress,
			LocalTime:  true,
		}

		// Write to file, and optionally also stdout (for interactive mode)
		if cfg.AlsoStdout {
			l.writer = io.MultiWriter(os.Stdout, l.lumberjack)
		} else {
			l.writer = l.lumberjack
		}
	} else {
		l.writer = os.Stdout
	}

	return l, nil
}

// Close closes the logger
func (l *Logger) Close() error {
	if l.lumberjack != nil {
		return l.lumberjack.Close()
	}
	return nil
}

// SetLevel changes the log level at runtime
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// Entry represents a structured log entry (wide event)
type Entry struct {
	Timestamp string         `json:"ts"`
	Level     string         `json:"level"`
	Message   string         `json:"msg"`
	Fields    map[string]any `json:"fields,omitempty"`

	// Request tracing fields (for wide events)
	RequestID   string `json:"request_id,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	QueryName   string `json:"query_name,omitempty"`
}

func (l *Logger) log(level Level, msg string, fields map[string]any) {
	if level < l.level {
		return
	}

	entry := Entry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Level:     level.String(),
		Message:   msg,
		Fields:    fields,
	}

	// Extract tracing fields if present
	if fields != nil {
		if rid, ok := fields["request_id"].(string); ok {
			entry.RequestID = rid
			delete(fields, "request_id")
		}
		if ep, ok := fields["endpoint"].(string); ok {
			entry.Endpoint = ep
			delete(fields, "endpoint")
		}
		if qn, ok := fields["query_name"].(string); ok {
			entry.QueryName = qn
			delete(fields, "query_name")
		}
		if len(fields) == 0 {
			entry.Fields = nil
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	data, _ := json.Marshal(entry)
	l.writer.Write(append(data, '\n'))
}

// Debug logs at debug level
func (l *Logger) Debug(msg string, fields map[string]any) {
	l.log(LevelDebug, msg, fields)
}

// Info logs at info level
func (l *Logger) Info(msg string, fields map[string]any) {
	l.log(LevelInfo, msg, fields)
}

// Warn logs at warn level
func (l *Logger) Warn(msg string, fields map[string]any) {
	l.log(LevelWarn, msg, fields)
}

// Error logs at error level
func (l *Logger) Error(msg string, fields map[string]any) {
	l.log(LevelError, msg, fields)
}

// Package-level functions using default logger

func Debug(msg string, fields map[string]any) {
	if defaultLogger != nil {
		defaultLogger.Debug(msg, fields)
	}
}

func Info(msg string, fields map[string]any) {
	if defaultLogger != nil {
		defaultLogger.Info(msg, fields)
	}
}

func Warn(msg string, fields map[string]any) {
	if defaultLogger != nil {
		defaultLogger.Warn(msg, fields)
	}
}

func Error(msg string, fields map[string]any) {
	if defaultLogger != nil {
		defaultLogger.Error(msg, fields)
	}
}

func SetLevel(level Level) {
	if defaultLogger != nil {
		defaultLogger.SetLevel(level)
	}
}

func Close() error {
	if defaultLogger != nil {
		return defaultLogger.Close()
	}
	return nil
}
