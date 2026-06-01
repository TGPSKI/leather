package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// ---- helpers ----------------------------------------------------------------

func makeMsg(agent, content string, tags ...string) Message {
	return Message{
		AgentName: agent,
		Content:   content,
		Tags:      tags,
		Timestamp: time.Now(),
	}
}

func decodeJSONRequest(t *testing.T, r *http.Request, dst any) bool {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Errorf("read request body: %v", err)
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		t.Errorf("decode request body: %v", err)
		return false
	}
	return true
}

func writeTestResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Errorf("write response body: %v", err)
	}
}

// ---- TelegramNotifier -------------------------------------------------------

func TestTelegramSend_Success(t *testing.T) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !decodeJSONRequest(t, r, &got) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
		writeTestResponse(t, w, `{"ok":true}`)
	}))
	defer srv.Close()

	n := &TelegramNotifier{
		name:    "test",
		chatID:  "12345",
		token:   "mytoken",
		apiBase: srv.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	if err := n.Send(context.Background(), makeMsg("my-agent", "hello world")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["chat_id"] != "12345" {
		t.Errorf("chat_id: got %v want 12345", got["chat_id"])
	}
	text, _ := got["text"].(string)
	if !strings.Contains(text, "my-agent") || !strings.Contains(text, "hello world") {
		t.Errorf("text missing agent name or content: %q", text)
	}
}

func TestTelegramSend_RateLimit(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			writeTestResponse(t, w, `{"ok":false}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		writeTestResponse(t, w, `{"ok":true}`)
	}))
	defer srv.Close()

	n := &TelegramNotifier{
		name:    "test",
		chatID:  "12345",
		token:   "mytoken",
		apiBase: srv.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	if err := n.Send(context.Background(), makeMsg("agent", "msg")); err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 HTTP calls; got %d", calls)
	}
}

func TestTelegramSend_NonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeTestResponse(t, w, `{"ok":false,"description":"bad request"}`)
	}))
	defer srv.Close()

	n := &TelegramNotifier{
		name:    "test",
		chatID:  "12345",
		token:   "token",
		apiBase: srv.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	if err := n.Send(context.Background(), makeMsg("agent", "msg")); err == nil {
		t.Fatal("expected error on 400 response")
	}
}

func TestFormatTelegram_NoTags(t *testing.T) {
	msg := makeMsg("agent", "body")
	out := formatTelegram(msg)
	if !strings.HasPrefix(out, "*[agent]*") {
		t.Errorf("expected bold bracketed agent name, got: %q", out)
	}
}

func TestFormatTelegram_WithTags(t *testing.T) {
	msg := makeMsg("agent", "body", "prod", "critical")
	out := formatTelegram(msg)
	if !strings.Contains(out, "prod") || !strings.Contains(out, "critical") {
		t.Errorf("expected tags in output: %q", out)
	}
}

func TestFormatTelegram_Truncate(t *testing.T) {
	bigContent := strings.Repeat("x", telegramMaxBytes*2)
	msg := makeMsg("agent", bigContent)
	out := formatTelegram(msg)
	if len(out) > telegramMaxBytes {
		t.Errorf("output length %d exceeds limit %d", len(out), telegramMaxBytes)
	}
}

// ---- SignalNotifier ---------------------------------------------------------

func TestSignalSend_Success(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !decodeJSONRequest(t, r, &got) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	n := &SignalNotifier{
		name:   "test",
		from:   "+15551234567",
		to:     "+15557654321",
		apiURL: srv.URL,
		client: &http.Client{Timeout: 5 * time.Second},
	}
	if err := n.Send(context.Background(), makeMsg("my-agent", "signal body")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["number"] != "+15551234567" {
		t.Errorf("from number: got %v want +15551234567", got["number"])
	}
	text, _ := got["message"].(string)
	if !strings.Contains(text, "my-agent") {
		t.Errorf("message missing agent name: %q", text)
	}
}

func TestSignalSend_WithAuth(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	n := &SignalNotifier{
		name:   "test",
		from:   "+15551234567",
		to:     "+15557654321",
		apiURL: srv.URL,
		apiKey: "secret-key",
		client: &http.Client{Timeout: 5 * time.Second},
	}
	if err := n.Send(context.Background(), makeMsg("agent", "msg")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authHeader != "Bearer secret-key" {
		t.Errorf("Authorization header: got %q want %q", authHeader, "Bearer secret-key")
	}
}

func TestSignalSend_WithGroupID(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !decodeJSONRequest(t, r, &got) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	n := &SignalNotifier{
		name:    "test",
		from:    "+15551234567",
		groupID: "grp123",
		apiURL:  srv.URL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
	if err := n.Send(context.Background(), makeMsg("agent", "group msg")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["group_id"] != "grp123" {
		t.Errorf("group_id: got %v want grp123", got["group_id"])
	}
}

func TestSignalSend_NonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeTestResponse(t, w, `error details`)
	}))
	defer srv.Close()

	n := &SignalNotifier{
		name:   "test",
		from:   "+15551234567",
		to:     "+15557654321",
		apiURL: srv.URL,
		client: &http.Client{Timeout: 5 * time.Second},
	}
	if err := n.Send(context.Background(), makeMsg("agent", "msg")); err == nil {
		t.Fatal("expected error on 500 response")
	}
}

func TestFormatSignal_Truncate(t *testing.T) {
	bigContent := strings.Repeat("y", signalMaxBytes*2)
	msg := makeMsg("agent", bigContent, "tag1")
	out := formatSignal(msg)
	if len(out) > signalMaxBytes {
		t.Errorf("output length %d exceeds limit %d", len(out), signalMaxBytes)
	}
}

// ---- newSignalNotifier validation -------------------------------------------

func TestNewSignalNotifier_MissingFrom(t *testing.T) {
	_, err := newSignalNotifier(model.NotifyBackendConfig{
		Name: "bad",
		Type: "signal",
		To:   "+1555",
	})
	if err == nil {
		t.Fatal("expected error when from is missing")
	}
}

func TestNewSignalNotifier_MissingRecipient(t *testing.T) {
	_, err := newSignalNotifier(model.NotifyBackendConfig{
		Name: "bad",
		Type: "signal",
		From: "+1555",
	})
	if err == nil {
		t.Fatal("expected error when to and group_id are both missing")
	}
}

// ---- secret resolution ------------------------------------------------------

func TestResolve_Env(t *testing.T) {
	t.Setenv("TEST_NOTIFY_SECRET", "env-value")
	ref := model.SecretRef{Env: "TEST_NOTIFY_SECRET"}
	got, err := resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "env-value" {
		t.Errorf("got %q want %q", got, "env-value")
	}
}

func TestResolve_PassNotInPath_FallsBackToEnv(t *testing.T) {
	// Override PATH so 'pass' is definitely not found.
	old := os.Getenv("PATH")
	os.Setenv("PATH", t.TempDir()) // empty dir, no pass binary
	defer os.Setenv("PATH", old)

	t.Setenv("TEST_FALLBACK_SECRET", "fallback-value")
	ref := model.SecretRef{
		Pass: "some/path",
		Env:  "TEST_FALLBACK_SECRET",
	}
	got, err := resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "fallback-value" {
		t.Errorf("got %q want %q", got, "fallback-value")
	}
}

func TestResolve_BothEmpty(t *testing.T) {
	_, err := resolve(context.Background(), model.SecretRef{})
	if err == nil {
		t.Fatal("expected error when both pass and env are empty")
	}
}

func TestResolve_EnvMissing(t *testing.T) {
	_ = os.Unsetenv("NOTIFY_MISSING_ENV_VAR_XYZ")
	_, err := resolve(context.Background(), model.SecretRef{Env: "NOTIFY_MISSING_ENV_VAR_XYZ"})
	if err == nil {
		t.Fatal("expected error when env var is not set")
	}
}

// ---- BuildMap ---------------------------------------------------------------

func TestBuildMap_Telegram(t *testing.T) {
	// Telegram requires a real token; inject via env.
	t.Setenv("TEST_TG_TOKEN", "fake-token")
	cfgs := []model.NotifyBackendConfig{
		{
			Name:   "tg",
			Type:   "telegram",
			ChatID: "-100111",
			Token:  model.SecretRef{Env: "TEST_TG_TOKEN"},
		},
	}
	m, errs := BuildMap(cfgs)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if _, ok := m["tg"]; !ok {
		t.Error("expected tg entry in map")
	}
}

func TestBuildMap_UnknownType(t *testing.T) {
	cfgs := []model.NotifyBackendConfig{
		{Name: "bad", Type: "carrier-pigeon"},
	}
	_, errs := BuildMap(cfgs)
	if len(errs) == 0 {
		t.Fatal("expected error for unknown backend type")
	}
}

func TestBuildMap_PartialErrors(t *testing.T) {
	t.Setenv("TEST_TG_TOKEN2", "token2")
	cfgs := []model.NotifyBackendConfig{
		{Name: "good", Type: "telegram", ChatID: "123", Token: model.SecretRef{Env: "TEST_TG_TOKEN2"}},
		{Name: "bad", Type: "unknown"},
	}
	m, errs := BuildMap(cfgs)
	if len(errs) == 0 {
		t.Fatal("expected at least one error")
	}
	if len(m) != 1 {
		t.Errorf("expected 1 good backend; got %d", len(m))
	}
}

// ---- Name() and Error() -------------------------------------------------------

func TestTelegramNotifier_Name(t *testing.T) {
	t.Setenv("TEST_TG_NAME_TOKEN", "tok")
	n := &TelegramNotifier{name: "my-telegram"}
	if n.Name() != "my-telegram" {
		t.Errorf("Name() = %q, want my-telegram", n.Name())
	}
}

func TestSignalNotifier_Name(t *testing.T) {
	n := &SignalNotifier{name: "my-signal"}
	if n.Name() != "my-signal" {
		t.Errorf("Name() = %q, want my-signal", n.Name())
	}
}

func TestTelegramError_Error(t *testing.T) {
	e := &telegramRateLimitError{retryAfter: 5 * time.Second}
	msg := e.Error()
	if !strings.Contains(msg, "rate limited") {
		t.Errorf("Error() = %q, want to contain 'rate limited'", msg)
	}
}
