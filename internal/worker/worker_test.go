package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
)

// testLogger returns a no-op logger suitable for tests.
func testLogger(t *testing.T) *logging.Logger {
	t.Helper()
	return logging.New("test", model.LogLevelError)
}

// TestLoadDir_Empty returns no workers when dir is empty.
func TestLoadDir_Empty(t *testing.T) {
	defs, err := LoadDir(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("want 0 workers, got %d", len(defs))
	}
}

// TestLoadDir_Missing returns no error for a missing directory.
func TestLoadDir_Missing(t *testing.T) {
	defs, err := LoadDir("/this/path/does/not/exist")
	if err != nil {
		t.Fatalf("unexpected error for missing dir: %v", err)
	}
	if len(defs) != 0 {
		t.Errorf("want 0 workers, got %d", len(defs))
	}
}

// TestLoadDir_ValidWorker parses a well-formed *.worker.yaml file.
func TestLoadDir_ValidWorker(t *testing.T) {
	dir := t.TempDir()
	yaml := `name: test-poller
type: http_poll
interval: 30s
url: "https://example.com/items"
headers:
  Authorization: "Bearer token"
output:
  queue: test-queue
  dedup_key: number
`
	if err := os.WriteFile(filepath.Join(dir, "test.worker.yaml"), []byte(yaml), 0600); err != nil {
		t.Fatal(err)
	}
	defs, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("want 1 def, got %d", len(defs))
	}
	d := defs[0]
	if d.Name != "test-poller" {
		t.Errorf("Name: got %q", d.Name)
	}
	if d.Interval != 30*time.Second {
		t.Errorf("Interval: got %v", d.Interval)
	}
	if d.Output.Queue != "test-queue" {
		t.Errorf("Output.Queue: got %q", d.Output.Queue)
	}
	if d.Output.DedupKey != "number" {
		t.Errorf("Output.DedupKey: got %q", d.Output.DedupKey)
	}
	if d.Headers["Authorization"] != "Bearer token" {
		t.Errorf("Headers Authorization: got %q", d.Headers["Authorization"])
	}
}

// TestHTTPPollWorker_NewItemsEnqueued verifies that new items are enqueued
// and deduplicated correctly across two poll cycles.
func TestHTTPPollWorker_NewItemsEnqueued(t *testing.T) {
	// First response: two items.
	firstItems := []map[string]any{
		{"number": float64(1), "title": "issue one"},
		{"number": float64(2), "title": "issue two"},
	}
	// Second response: same items plus one new one.
	secondItems := append(firstItems, map[string]any{"number": float64(3), "title": "issue three"})

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var items []map[string]any
		if callCount == 1 {
			items = firstItems
		} else {
			items = secondItems
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items) //nolint:errcheck
	}))
	defer srv.Close()

	dir := t.TempDir()
	mgr := queue.NewManager(dir)

	def := model.WorkerDefinition{
		Name:     "test-worker",
		Type:     "http_poll",
		Interval: time.Hour, // large interval; we call poll() directly
		URL:      srv.URL,
		Output:   model.WorkerOutput{Queue: "test-q", DedupKey: "number"},
	}
	log := testLogger(t)
	w, err := newHTTPPollWorker(def, mgr, log)
	if err != nil {
		t.Fatalf("newHTTPPollWorker: %v", err)
	}

	ctx := context.Background()

	// First poll: should enqueue 2 items.
	w.poll(ctx)
	q, err := mgr.Get("test-q")
	if err != nil {
		t.Fatalf("mgr.Get: %v", err)
	}
	if q.Len() != 2 {
		t.Fatalf("after first poll: want 2 items, got %d", q.Len())
	}

	// Second poll: same 2 items already seen + 1 new item → only 1 new item enqueued.
	w.poll(ctx)
	if q.Len() != 3 {
		t.Fatalf("after second poll: want 3 items, got %d", q.Len())
	}
}

// TestHTTPPollWorker_NonArrayResponse logs a warning and enqueues nothing.
func TestHTTPPollWorker_NonArrayResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"not": "an array"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	dir := t.TempDir()
	mgr := queue.NewManager(dir)

	def := model.WorkerDefinition{
		Name:     "bad-worker",
		Type:     "http_poll",
		Interval: time.Hour,
		URL:      srv.URL,
		Output:   model.WorkerOutput{Queue: "bad-q", DedupKey: "id"},
	}
	w, err := newHTTPPollWorker(def, mgr, testLogger(t))
	if err != nil {
		t.Fatalf("newHTTPPollWorker: %v", err)
	}
	w.poll(context.Background())

	q, err := mgr.Get("bad-q")
	if err != nil {
		t.Fatalf("mgr.Get: %v", err)
	}
	if q.Len() != 0 {
		t.Errorf("want 0 items after bad response, got %d", q.Len())
	}
}

// TestSupervisor_StartAndDrain verifies the supervisor starts and drains cleanly.
func TestSupervisor_StartAndDrain(t *testing.T) {
	// Use a server that immediately returns an empty array.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]")) //nolint:errcheck
	}))
	defer srv.Close()

	defs := []model.WorkerDefinition{{
		Name:     "sup-worker",
		Type:     "http_poll",
		Interval: 100 * time.Millisecond,
		URL:      srv.URL,
		Output:   model.WorkerOutput{Queue: "sup-q", DedupKey: "id"},
	}}

	dir := t.TempDir()
	mgr := queue.NewManager(dir)
	log := testLogger(t)

	sup := NewSupervisor(defs, mgr, log)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	sup.Start(ctx)
	<-ctx.Done()
	sup.Drain() // must not block indefinitely
}
