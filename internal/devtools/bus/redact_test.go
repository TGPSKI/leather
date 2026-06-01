package bus

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactPayload_RedactsSensitiveKeys(t *testing.T) {
	t.Helper()
	raw := json.RawMessage(`{"headers":{"Authorization":"Bearer abc","Cookie":"a=b"},"token":"xyz","ok":"value"}`)

	redacted := RedactPayload(raw)
	var decoded map[string]any
	if err := json.Unmarshal(redacted, &decoded); err != nil {
		t.Fatalf("unmarshal redacted payload: %v", err)
	}

	headers, _ := decoded["headers"].(map[string]any)
	if headers["Authorization"] != "[REDACTED]" {
		t.Fatalf("Authorization = %v, want [REDACTED]", headers["Authorization"])
	}
	if headers["Cookie"] != "[REDACTED]" {
		t.Fatalf("Cookie = %v, want [REDACTED]", headers["Cookie"])
	}
	if decoded["token"] != "[REDACTED]" {
		t.Fatalf("token = %v, want [REDACTED]", decoded["token"])
	}
	if decoded["ok"] != "value" {
		t.Fatalf("ok = %v, want value", decoded["ok"])
	}
}

func TestRedactPayload_TruncatesLongStrings(t *testing.T) {
	t.Helper()
	long := strings.Repeat("x", maxStringBytes+100)
	encoded, err := json.Marshal(map[string]any{"message": long})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	redacted := RedactPayload(encoded)
	var decoded map[string]any
	if err := json.Unmarshal(redacted, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	msg, _ := decoded["message"].(string)
	if !strings.HasSuffix(msg, "...[truncated]") {
		t.Fatalf("message suffix = %q, want truncated marker", msg)
	}
	if len(msg) <= maxStringBytes {
		t.Fatalf("message len = %d, want > %d", len(msg), maxStringBytes)
	}
}

func TestRedactPayload_InvalidJSONPreserved(t *testing.T) {
	t.Helper()
	raw := json.RawMessage(`{"broken":`)
	redacted := RedactPayload(raw)
	if string(redacted) != string(raw) {
		t.Fatalf("redacted payload changed invalid JSON: got %q want %q", string(redacted), string(raw))
	}
}

func TestRedactEvent_AppliesPayloadAndErrTruncation(t *testing.T) {
	t.Helper()
	payload := json.RawMessage(`{"password":"supersecret"}`)
	out := RedactEvent(Event{Payload: payload, Err: strings.Repeat("e", maxStringBytes+3)})

	var decoded map[string]any
	if err := json.Unmarshal(out.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded["password"] != "[REDACTED]" {
		t.Fatalf("password = %v, want [REDACTED]", decoded["password"])
	}
	if !strings.HasSuffix(out.Err, "...[truncated]") {
		t.Fatalf("Err suffix = %q, want truncated marker", out.Err)
	}
}
