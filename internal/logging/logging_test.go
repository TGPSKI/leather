package logging

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tgpski/leather/internal/model"
)

func TestLogger_InfoWritesToWriter(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter("test-component", model.LogLevelInfo, &buf, false)
	log.Info("hello from test", "key", "value")
	out := buf.String()
	if !strings.Contains(out, "hello from test") {
		t.Errorf("expected 'hello from test' in output, got: %q", out)
	}
	if !strings.Contains(out, "test-component") {
		t.Errorf("expected 'test-component' in output, got: %q", out)
	}
}

func TestLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter("test", model.LogLevelDebug, &buf, true)
	log.Debug("json-message")
	out := buf.String()
	if !strings.Contains(out, `"msg"`) {
		t.Errorf("expected JSON msg field in output, got: %q", out)
	}
	if !strings.Contains(out, "json-message") {
		t.Errorf("expected message text in output, got: %q", out)
	}
}

func TestLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter("test", model.LogLevelWarn, &buf, false)
	log.Debug("debug-message")
	log.Info("info-message")
	log.Warn("warn-message")
	log.Error("error-message")
	out := buf.String()

	if strings.Contains(out, "debug-message") {
		t.Error("debug message should be filtered at warn level")
	}
	if strings.Contains(out, "info-message") {
		t.Error("info message should be filtered at warn level")
	}
	if !strings.Contains(out, "warn-message") {
		t.Errorf("warn message should appear at warn level, got: %q", out)
	}
	if !strings.Contains(out, "error-message") {
		t.Errorf("error message should appear at warn level, got: %q", out)
	}
}

func TestLogger_With(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter("test", model.LogLevelInfo, &buf, false)
	log2 := log.With("extra_key", "extra_value")
	log2.Info("with-test")
	out := buf.String()
	if !strings.Contains(out, "extra_key") {
		t.Errorf("expected extra_key in output, got: %q", out)
	}
	if !strings.Contains(out, "extra_value") {
		t.Errorf("expected extra_value in output, got: %q", out)
	}
}

func TestLogger_ErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	log := NewWithWriter("test", model.LogLevelError, &buf, false)
	log.Warn("should-be-suppressed")
	log.Error("should-appear")
	out := buf.String()
	if strings.Contains(out, "should-be-suppressed") {
		t.Error("warn message should be suppressed at error level")
	}
	if !strings.Contains(out, "should-appear") {
		t.Errorf("error message should appear at error level, got: %q", out)
	}
}

// --- New and NewJSON constructors ---

func TestNew_ReturnsUsableLogger(t *testing.T) {
	// New writes to os.Stderr; verify it returns a functional, non-nil logger.
	log := New("test-component", model.LogLevelWarn)
	if log == nil {
		t.Fatal("New returned nil")
	}
	// Must not panic when called; output goes to stderr (acceptable in tests).
	log.Warn("test-new-warn")
}

func TestNewJSON_ReturnsUsableLogger(t *testing.T) {
	// NewJSON writes JSON-formatted output to os.Stderr; verify non-nil and functional.
	log := NewJSON("test-component", model.LogLevelError)
	if log == nil {
		t.Fatal("NewJSON returned nil")
	}
	log.Error("test-newjson-error")
}
