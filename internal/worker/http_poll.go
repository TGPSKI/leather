package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
)

// HTTPPollWorker polls an HTTP endpoint at a fixed interval, deduplicates
// responses by a configured key field, and pushes new items into a named queue.
//
// Deduplication is in-memory; the seen set is initialized from the current
// queue contents on startup to avoid re-queuing items already pending.
type HTTPPollWorker struct {
	def        model.WorkerDefinition
	mgr        *queue.Manager
	seen       map[string]bool
	log        *logging.Logger
	client     *http.Client
	lastPollAt int64 // Unix timestamp of most recent poll; 0 = never polled (atomic)
	// nextAllowedAt is the earliest time the next poll may run, as a Unix
	// nanosecond timestamp. Updated when an upstream returns 429/503 with a
	// Retry-After header so the poller honours rate-limit guidance instead of
	// hammering the endpoint at its fixed interval. (T4.2)
	nextAllowedAt int64
}

// retryAfterMax bounds the parsed Retry-After value so a hostile or
// misconfigured upstream cannot park the poller for hours. (T4.2)
const retryAfterMax = 5 * time.Minute

// newHTTPPollWorker creates a new HTTPPollWorker for def.
// It seeds the in-memory dedup set from items already in the queue.
func newHTTPPollWorker(def model.WorkerDefinition, mgr *queue.Manager, log *logging.Logger) (*HTTPPollWorker, error) {
	w := &HTTPPollWorker{
		def:    def,
		mgr:    mgr,
		seen:   make(map[string]bool),
		log:    log,
		client: &http.Client{Timeout: 30 * time.Second},
	}
	if err := w.seedSeen(); err != nil {
		return nil, fmt.Errorf("worker/newHTTPPollWorker %s: seed dedup: %w", def.Name, err)
	}
	return w, nil
}

// seedSeen pre-populates the in-memory dedup set from the current queue
// so that items already waiting are not re-enqueued on the first poll.
func (w *HTTPPollWorker) seedSeen() error {
	if w.def.Output.DedupKey == "" {
		return nil
	}
	q, err := w.mgr.Get(w.def.Output.Queue)
	if err != nil {
		return err
	}
	n := q.Len()
	if n == 0 {
		return nil
	}
	buf := make([]model.QueueItem, 0, n)
	for i := 0; i < n; i++ {
		item, ok, err := q.Dequeue()
		if err != nil || !ok {
			break
		}
		buf = append(buf, item)
	}
	for _, item := range buf {
		if key := extractKey(item.Payload, w.def.Output.DedupKey); key != "" {
			w.seen[key] = true
		}
		if err := q.Enqueue(item); err != nil {
			w.log.Warn("seed: re-enqueue failed", "worker", w.def.Name, "id", item.ID, "error", err)
		}
	}
	return nil
}

// Run loops until ctx is cancelled, calling poll at each interval.
func (w *HTTPPollWorker) Run(ctx context.Context) {
	w.log.Info("worker started", "worker", w.def.Name, "interval", w.def.Interval)
	ticker := time.NewTicker(w.def.Interval)
	defer ticker.Stop()
	// Poll immediately on start.
	w.poll(ctx)
	for {
		select {
		case <-ctx.Done():
			w.log.Info("worker stopping", "worker", w.def.Name)
			return
		case <-ticker.C:
			w.poll(ctx)
		}
	}
}

// poll fetches the configured URL and enqueues any new items.
func (w *HTTPPollWorker) poll(ctx context.Context) {
	// Honour any Retry-After backoff set by a previous 429/503. (T4.2)
	if next := atomic.LoadInt64(&w.nextAllowedAt); next > 0 && time.Now().UnixNano() < next {
		return
	}
	url := expandEnvTemplates(w.def.URL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		w.log.Warn("worker poll: build request failed", "worker", w.def.Name, "error", err)
		return
	}
	for k, v := range w.def.Headers {
		req.Header.Set(k, expandEnvTemplates(v))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		w.log.Warn("worker poll: request failed", "worker", w.def.Name, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); ok {
			if d > retryAfterMax {
				d = retryAfterMax
			}
			atomic.StoreInt64(&w.nextAllowedAt, time.Now().Add(d).UnixNano())
			w.log.Warn("worker poll: backing off per Retry-After", "worker", w.def.Name, "status", resp.StatusCode, "wait", d)
		} else {
			w.log.Warn("worker poll: non-2xx response", "worker", w.def.Name, "status", resp.StatusCode)
		}
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		w.log.Warn("worker poll: non-2xx response", "worker", w.def.Name, "status", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // cap at 4 MB
	if err != nil {
		w.log.Warn("worker poll: read body failed", "worker", w.def.Name, "error", err)
		return
	}

	var items []map[string]any
	if err := json.Unmarshal(body, &items); err != nil {
		w.log.Warn("worker poll: JSON parse failed", "worker", w.def.Name, "error", err)
		return
	}

	newCount := 0
	for _, raw := range items {
		key := extractKey(raw, w.def.Output.DedupKey)
		if w.def.Output.DedupKey != "" {
			if key == "" || w.seen[key] {
				continue
			}
		}
		item := model.QueueItem{
			ID:         uniqueID(w.def.Name, key),
			AgentName:  "",
			Payload:    raw,
			EnqueuedAt: time.Now().Unix(),
		}
		if err := w.mgr.Enqueue(w.def.Output.Queue, item); err != nil {
			w.log.Warn("worker poll: enqueue failed", "worker", w.def.Name, "key", key, "error", err)
			continue
		}
		if w.def.Output.DedupKey != "" && key != "" {
			w.seen[key] = true
		}
		newCount++
	}
	if newCount > 0 {
		w.log.Info("worker poll: enqueued new items", "worker", w.def.Name, "count", newCount)
	}
	atomic.StoreInt64(&w.lastPollAt, time.Now().Unix())
}

// extractKey reads the dedupKey field from item. Returns "" if not found.
func extractKey(item map[string]any, dedupKey string) string {
	if dedupKey == "" {
		return ""
	}
	k := strings.TrimPrefix(dedupKey, ".")
	v, ok := item[k]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// expandEnvTemplates replaces {{env:VAR}} expressions with the corresponding
// environment variable value.
func expandEnvTemplates(s string) string {
	const prefix = "{{env:"
	const suffix = "}}"
	for {
		start := strings.Index(s, prefix)
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], suffix)
		if end < 0 {
			break
		}
		end += start
		varName := s[start+len(prefix) : end]
		s = s[:start] + os.Getenv(varName) + s[end+len(suffix):]
	}
	return s
}

// parseRetryAfter parses an HTTP Retry-After header value as either a
// non-negative integer number of seconds or an HTTP-date. Returns the
// resulting wait duration relative to now and ok=true on success. (T4.2)
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// uniqueID constructs a stable item ID from the worker name and the dedup key.
func uniqueID(workerName, key string) string {
	if key == "" {
		return fmt.Sprintf("%s-%d", workerName, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s-%s", workerName, key)
}
