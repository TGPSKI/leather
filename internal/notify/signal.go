package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// signalDefaultAPIURL is the default signal-cli REST API base URL.
const signalDefaultAPIURL = "http://127.0.0.1:8080"

// signalMaxBytes is the practical Signal message length limit.
const signalMaxBytes = 4096

// signalSendPath is the signal-cli REST API v2 send endpoint.
const signalSendPath = "/v2/send"

// SignalNotifier delivers messages via a locally-running signal-cli REST API.
// See https://github.com/bbernhard/signal-cli-rest-api for the server side.
type SignalNotifier struct {
	name    string
	from    string
	to      string // individual recipient (E.164)
	groupID string // group recipient; used when to is empty
	apiURL  string
	apiKey  string // optional; sent as Authorization: Bearer header
	client  *http.Client
}

// newSignalNotifier constructs a SignalNotifier, resolving the API key (if any)
// from the configured SecretRef at construction time.
func newSignalNotifier(cfg model.NotifyBackendConfig) (*SignalNotifier, error) {
	if cfg.From == "" {
		return nil, fmt.Errorf("notify/signal %q: from number is required", cfg.Name)
	}
	if cfg.To == "" && cfg.GroupID == "" {
		return nil, fmt.Errorf("notify/signal %q: to or group_id is required", cfg.Name)
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = signalDefaultAPIURL
	}
	// API key is optional; many self-hosted signal-cli instances run without auth.
	var apiKey string
	if cfg.Token.Pass != "" || cfg.Token.Env != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		k, err := resolve(ctx, cfg.Token)
		if err != nil {
			return nil, fmt.Errorf("notify/signal %q: token: %w", cfg.Name, err)
		}
		apiKey = k
	}
	return &SignalNotifier{
		name:    cfg.Name,
		from:    cfg.From,
		to:      cfg.To,
		groupID: cfg.GroupID,
		apiURL:  apiURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Name returns the backend config name.
func (s *SignalNotifier) Name() string { return s.name }

// Send delivers msg to the configured Signal recipient.
func (s *SignalNotifier) Send(ctx context.Context, msg Message) error {
	text := formatSignal(msg)
	return s.doSend(ctx, text)
}

func (s *SignalNotifier) doSend(ctx context.Context, text string) error {
	payload := map[string]any{
		"message": text,
		"number":  s.from,
	}
	if s.groupID != "" {
		payload["recipients"] = []string{}
		payload["group_id"] = s.groupID
	} else {
		payload["recipients"] = []string{s.to}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notify/signal %q: marshal: %w", s.name, err)
	}
	url := strings.TrimRight(s.apiURL, "/") + signalSendPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify/signal %q: build request: %w", s.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify/signal %q: http: %w", s.name, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notify/signal %q: API error %d: %s", s.name, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// formatSignal returns the plain-text message body, truncated to signalMaxBytes.
func formatSignal(msg Message) string {
	header := fmt.Sprintf("[%s]", msg.AgentName)
	if len(msg.Tags) > 0 {
		header += " (" + strings.Join(msg.Tags, ", ") + ")"
	}
	full := header + "\n\n" + msg.Content
	return truncateUTF8(full, signalMaxBytes)
}
