package secret

import (
	"encoding/json"
	"regexp"
	"strings"
)

// RedactJSON takes raw JSON bytes (e.g. marshaled tool call arguments) and
// returns a JSON string with sensitive-looking keys' values replaced by
// "[REDACTED]", and known bearer/basic-auth patterns scrubbed from any
// remaining string values. Used by the runner to persist tool call args in
// run records (.state/runs/*.jsonl) without leaking credentials.
//
// If raw is not valid JSON (e.g. a bare string argument), it falls back to
// scrubbing bearer/basic-auth patterns directly in the raw text.
func RedactJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return redactPatterns(string(raw))
	}
	redacted := redactValue("", decoded)
	encoded, err := json.Marshal(redacted)
	if err != nil {
		return redactPatterns(string(raw))
	}
	return string(encoded)
}

func redactValue(key string, value any) any {
	if IsSensitiveKey(key) {
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
		return redactPatterns(typed)
	default:
		return value
	}
}

// IsSensitiveKey reports whether key looks like it holds a credential. It is
// shared by replay/run-record redaction and devtools bus event redaction so
// credential-key heuristics stay in one place.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}
	switch k {
	case "tokens", "prompt_tokens", "completion_tokens", "total_tokens", "max_tokens":
		return false
	}
	if strings.HasSuffix(k, "_tokens") {
		return false
	}
	sensitiveContains := []string{
		"authorization",
		"cookie",
		"password",
		"passwd",
		"secret",
		"token",
		"api_key",
		"apikey",
		"access_key",
		"private_key",
	}
	for _, part := range sensitiveContains {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}

// authPattern matches "Authorization: Bearer <token>" / "Authorization: Basic
// <creds>" style header text that may appear embedded in a string value even
// when the enclosing JSON key isn't itself named "authorization" (e.g. a raw
// header line inside a request body or args string).
var authPattern = regexp.MustCompile(`(?i)(authorization\s*:?\s*(?:bearer|basic)\s+)\S+`)

// bearerPattern matches a bare "Bearer <token>" fragment without a leading
// "Authorization:" label.
var bearerPattern = regexp.MustCompile(`(?i)(\bbearer\s+)\S+`)

func redactPatterns(s string) string {
	s = authPattern.ReplaceAllString(s, "${1}[REDACTED]")
	s = bearerPattern.ReplaceAllString(s, "${1}[REDACTED]")
	return s
}
