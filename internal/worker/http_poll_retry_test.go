package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
)

// TestParseRetryAfter covers the seconds form, the HTTP-date form, and
// negative / malformed inputs. (T4.2)
func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		hdr    string
		want   time.Duration
		wantOK bool
	}{
		{"empty", "", 0, false},
		{"seconds zero", "0", 0, true},
		{"seconds 30", "30", 30 * time.Second, true},
		{"seconds negative", "-5", 0, false},
		{"http-date future", now.Add(45 * time.Second).UTC().Format(http.TimeFormat), 45 * time.Second, true},
		{"http-date past", now.Add(-1 * time.Minute).UTC().Format(http.TimeFormat), 0, true},
		{"garbage", "soon-ish", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, ok := parseRetryAfter(tc.hdr, now)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && d != tc.want {
				t.Errorf("d = %v, want %v", d, tc.want)
			}
		})
	}
}

// TestHTTPPollWorker_HonorsRetryAfter verifies that a 429 response with
// Retry-After parks the poller until the wait elapses. (T4.2)
func TestHTTPPollWorker_HonorsRetryAfter(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	def := model.WorkerDefinition{
		Name:     "retry-after-test",
		Type:     "http_poll",
		URL:      srv.URL,
		Interval: 10 * time.Millisecond,
		Output:   model.WorkerOutput{Queue: "test-q"},
	}
	mgr := queue.NewManager(t.TempDir())
	w, err := newHTTPPollWorker(def, mgr, testLogger(t))
	if err != nil {
		t.Fatalf("newHTTPPollWorker: %v", err)
	}

	// First poll: hits the server, gets 429, sets nextAllowedAt ~60s out.
	w.poll(context.Background())
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("first poll hits = %d, want 1", got)
	}
	if atomic.LoadInt64(&w.nextAllowedAt) == 0 {
		t.Fatalf("nextAllowedAt should be set after 429 with Retry-After")
	}

	// Subsequent polls before the backoff elapses must NOT hit the server.
	for i := 0; i < 5; i++ {
		w.poll(context.Background())
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("during backoff, hits = %d, want 1", got)
	}
}
