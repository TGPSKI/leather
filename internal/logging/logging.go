// Package logging provides structured logging for leather components.
// It wraps log/slog with a component label and level control.
package logging

import (
	"io"
	"log/slog"
	"os"

	"github.com/tgpski/leather/internal/model"
)

// Logger is a component-scoped structured logger.
type Logger struct {
	inner *slog.Logger
}

// New returns a Logger that writes text-formatted output to stderr.
func New(component string, level model.LogLevel) *Logger {
	return NewWithWriter(component, level, os.Stderr, false)
}

// NewJSON returns a Logger that writes JSON-formatted output to stderr.
func NewJSON(component string, level model.LogLevel) *Logger {
	return NewWithWriter(component, level, os.Stderr, true)
}

// NewWithWriter returns a Logger writing to w. Useful for tests and alternate sinks.
func NewWithWriter(component string, level model.LogLevel, w io.Writer, jsonFormat bool) *Logger {
	opts := &slog.HandlerOptions{Level: slogLevel(level)}
	var h slog.Handler
	if jsonFormat {
		h = slog.NewJSONHandler(w, opts)
	} else {
		h = slog.NewTextHandler(w, opts)
	}
	return &Logger{inner: slog.New(h).With("component", component)}
}

// Debug emits a debug-level log entry.
func (l *Logger) Debug(msg string, args ...any) { l.inner.Debug(msg, args...) }

// Info emits an info-level log entry.
func (l *Logger) Info(msg string, args ...any) { l.inner.Info(msg, args...) }

// Warn emits a warn-level log entry.
func (l *Logger) Warn(msg string, args ...any) { l.inner.Warn(msg, args...) }

// Error emits an error-level log entry.
func (l *Logger) Error(msg string, args ...any) { l.inner.Error(msg, args...) }

// With returns a new Logger with additional key-value attributes attached.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{inner: l.inner.With(args...)}
}

// slogLevel converts a model.LogLevel to its slog equivalent.
func slogLevel(level model.LogLevel) slog.Level {
	switch level {
	case model.LogLevelDebug:
		return slog.LevelDebug
	case model.LogLevelWarn:
		return slog.LevelWarn
	case model.LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
