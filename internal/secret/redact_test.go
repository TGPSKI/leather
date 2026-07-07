package secret

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactJSON_SensitiveKey(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"Authorization": "Bearer sk-super-secret-token",
		"input":         "normal value",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := RedactJSON(raw)
	if strings.Contains(got, "sk-super-secret-token") {
		t.Fatalf("secret leaked in redacted output: %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker, got %q", got)
	}
	if !strings.Contains(got, "normal value") {
		t.Fatalf("non-sensitive value should survive redaction, got %q", got)
	}
}

func TestRedactJSON_BearerPatternInValue(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"header": "Authorization: Bearer sk-super-secret-token",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := RedactJSON(raw)
	if strings.Contains(got, "sk-super-secret-token") {
		t.Fatalf("secret leaked in redacted output: %q", got)
	}
}

func TestRedactJSON_NestedMapAndSlice(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"headers": map[string]any{
			"Authorization": "Bearer nested-secret",
		},
		"items": []any{
			map[string]any{"api_key": "abc123"},
			"plain",
		},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := RedactJSON(raw)
	if strings.Contains(got, "nested-secret") || strings.Contains(got, "abc123") {
		t.Fatalf("secret leaked in nested structure: %q", got)
	}
	if !strings.Contains(got, "plain") {
		t.Fatalf("non-sensitive nested value should survive: %q", got)
	}
}

func TestRedactJSON_NoSecrets_Unaffected(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"input": "hello", "count": 3})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got := RedactJSON(raw)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	if decoded["input"] != "hello" {
		t.Fatalf("input = %v, want hello", decoded["input"])
	}
}

func TestRedactJSON_InvalidJSONFallsBackToPatternScrub(t *testing.T) {
	got := RedactJSON([]byte("Authorization: Bearer sk-not-json-secret"))
	if strings.Contains(got, "sk-not-json-secret") {
		t.Fatalf("secret leaked in non-JSON fallback: %q", got)
	}
}

func TestRedactJSON_Empty(t *testing.T) {
	if got := RedactJSON(nil); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
