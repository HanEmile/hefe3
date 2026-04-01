// Package logger provides leveled logging for the application.
package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Level represents a logging level.
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

// ParseLevel parses a string into a Level.
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

// Logger is a leveled logger.
type Logger struct {
	mu     sync.Mutex
	level  Level
	output io.Writer
}

// Global default logger
var std = &Logger{
	level:  LevelInfo,
	output: os.Stderr,
}

// New creates a new logger with the given level and output.
func New(level Level, output io.Writer) *Logger {
	return &Logger{
		level:  level,
		output: output,
	}
}

// SetLevel sets the global logger level.
func SetLevel(level Level) {
	std.mu.Lock()
	defer std.mu.Unlock()
	std.level = level
}

// SetOutput sets the global logger output.
func SetOutput(w io.Writer) {
	std.mu.Lock()
	defer std.mu.Unlock()
	std.output = w
}

// GetLevel returns the current log level.
func GetLevel() Level {
	std.mu.Lock()
	defer std.mu.Unlock()
	return std.level
}

func (l *Logger) log(level Level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	timestamp := time.Now().Format("2006/01/02 15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(l.output, "%s [%s] %s\n", timestamp, level.String(), msg)
}

// Debug logs at debug level.
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(LevelDebug, format, args...)
}

// Info logs at info level.
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(LevelInfo, format, args...)
}

// Warn logs at warn level.
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(LevelWarn, format, args...)
}

// Error logs at error level.
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(LevelError, format, args...)
}

// Package-level functions use the default logger

// Debug logs at debug level.
func Debug(format string, args ...interface{}) {
	std.Debug(format, args...)
}

// Info logs at info level.
func Info(format string, args ...interface{}) {
	std.Info(format, args...)
}

// Warn logs at warn level.
func Warn(format string, args ...interface{}) {
	std.Warn(format, args...)
}

// Error logs at error level.
func Error(format string, args ...interface{}) {
	std.Error(format, args...)
}

// Fatal logs at error level and exits.
func Fatal(format string, args ...interface{}) {
	std.Error(format, args...)
	os.Exit(1)
}
