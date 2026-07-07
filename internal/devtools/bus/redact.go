package bus

import (
	"encoding/json"

	"github.com/TGPSKI/leather/internal/secret"
)

const maxStringBytes = 4096

// RedactEvent returns a copy of ev with sensitive payload fields redacted.
func RedactEvent(ev Event) Event {
	out := cloneEvent(ev)
	out.Payload = RedactPayload(ev.Payload)
	out.Err = truncateString(out.Err, maxStringBytes)
	return out
}

// RedactPayload redacts known secret fields and truncates large strings.
func RedactPayload(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return append([]byte(nil), raw...)
	}

	redacted := redactValue("", decoded)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return append([]byte(nil), raw...)
	}
	return encoded
}

func redactValue(key string, value any) any {
	if secret.IsSensitiveKey(key) {
		return "[REDACTED]"
	}

	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = redactValue(k, v)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactValue(key, item))
		}
		return out
	case string:
		return truncateString(typed, maxStringBytes)
	default:
		return value
	}
}

func truncateString(text string, maxBytes int) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	return text[:maxBytes] + "...[truncated]"
}
