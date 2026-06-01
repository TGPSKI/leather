package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tgpski/leather/internal/model"
)

// telegramMaxBytes is the Telegram sendMessage character limit.
// Messages longer than this are truncated with a trailing ellipsis.
const telegramMaxBytes = 4096

// telegramAPIBase is the base URL for Telegram Bot API calls.
const telegramAPIBase = "https://api.telegram.org"

// TelegramNotifier delivers messages via the Telegram Bot API.
type TelegramNotifier struct {
	name    string
	chatID  string
	token   string
	apiBase string // overridable in tests
	client  *http.Client
}

// newTelegramNotifier constructs a TelegramNotifier, resolving the bot token
// from the configured SecretRef at construction time.
func newTelegramNotifier(cfg model.NotifyBackendConfig) (*TelegramNotifier, error) {
	if cfg.ChatID == "" {
		return nil, fmt.Errorf("notify/telegram %q: chat_id is required", cfg.Name)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	token, err := resolve(ctx, cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("notify/telegram %q: token: %w", cfg.Name, err)
	}
	return &TelegramNotifier{
		name:    cfg.Name,
		chatID:  cfg.ChatID,
		token:   token,
		apiBase: telegramAPIBase,
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Name returns the backend config name.
func (t *TelegramNotifier) Name() string { return t.name }

// Send delivers msg to the configured Telegram chat.
// On HTTP 429 it reads Retry-After and retries once before returning an error.
func (t *TelegramNotifier) Send(ctx context.Context, msg Message) error {
	text := formatTelegram(msg)
	return t.sendWithRetry(ctx, text)
}

func (t *TelegramNotifier) sendWithRetry(ctx context.Context, text string) error {
	err := t.doSend(ctx, text)
	if err == nil {
		return nil
	}
	var rateLimitErr *telegramRateLimitError
	if !isRateLimitError(err, &rateLimitErr) {
		return err
	}
	// Retry once after the indicated wait.
	wait := rateLimitErr.retryAfter
	if wait <= 0 {
		wait = 5 * time.Second
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
	}
	return t.doSend(ctx, text)
}

func (t *TelegramNotifier) doSend(ctx context.Context, text string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBase, t.token)
	body, err := json.Marshal(map[string]string{
		"chat_id":    t.chatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
	if err != nil {
		return fmt.Errorf("notify/telegram %q: marshal: %w", t.name, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("notify/telegram %q: build request: %w", t.name, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("notify/telegram %q: http: %w", t.name, scrubTokenFromURLError(err))
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode == http.StatusTooManyRequests {
		wait := parseRetryAfter(resp)
		return &telegramRateLimitError{retryAfter: wait}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notify/telegram %q: API error %d: %s", t.name, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// formatTelegram returns the Markdown-formatted message body, truncated to
// telegramMaxBytes. Tags are rendered on the first line when non-empty.
func formatTelegram(msg Message) string {
	header := fmt.Sprintf("*[%s]*", escapeTelegramMd(msg.AgentName))
	if len(msg.Tags) > 0 {
		header += " (" + strings.Join(msg.Tags, ", ") + ")"
	}
	full := header + "\n\n" + msg.Content
	return truncateUTF8(full, telegramMaxBytes)
}

// escapeTelegramMd escapes Markdown special characters in a plain-text string.
func escapeTelegramMd(s string) string {
	s = strings.ReplaceAll(s, "_", "\\_")
	s = strings.ReplaceAll(s, "*", "\\*")
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}

// truncateUTF8 cuts s to at most maxBytes bytes, rounding down to a valid
// UTF-8 boundary and appending "…" when truncation occurs.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Leave room for the ellipsis (3 bytes in UTF-8).
	limit := maxBytes - 3
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit] + "…"
}

// telegramRateLimitError is returned by doSend when the API responds 429.
type telegramRateLimitError struct {
	retryAfter time.Duration
}

func (e *telegramRateLimitError) Error() string {
	return fmt.Sprintf("telegram: rate limited; retry after %s", e.retryAfter)
}

func isRateLimitError(err error, out **telegramRateLimitError) bool {
	if rle, ok := err.(*telegramRateLimitError); ok {
		*out = rle
		return true
	}
	return false
}

// parseRetryAfter reads the Retry-After header from a 429 response and returns
// the indicated wait duration. Returns 5 s when the header is absent or
// unparseable.
func parseRetryAfter(resp *http.Response) time.Duration {
	ra := resp.Header.Get("Retry-After")
	if ra == "" {
		return 5 * time.Second
	}
	// Telegram Retry-After is always an integer number of seconds.
	var secs int
	if _, err := fmt.Sscanf(ra, "%d", &secs); err != nil || secs <= 0 {
		return 5 * time.Second
	}
	return time.Duration(secs) * time.Second
}

// botTokenRe matches the Telegram bot token segment in a URL path so it can be
// redacted from error messages before they are logged or wrapped in errors.
var botTokenRe = regexp.MustCompile(`/bot[^/]+/`)

// scrubTokenFromURLError redacts the bot token from any *url.Error in the
// error chain. The URL field of the *url.Error (which contains the full request
// URL including the token) is replaced with a redacted version. All other error
// types are returned unchanged.
func scrubTokenFromURLError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	urlErr.URL = botTokenRe.ReplaceAllString(urlErr.URL, "/bot<redacted>/")
	return urlErr
}
