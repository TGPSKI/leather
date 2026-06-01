package queue

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

func TestFileQueue_RoundTrip(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.jsonl")

	q, err := NewFileQueue(path)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}

	item := model.QueueItem{
		ID:         "abc",
		Payload:    map[string]any{"number": float64(42)},
		EnqueuedAt: time.Now().Unix(),
	}
	if err := q.Enqueue(item); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if q.Len() != 1 {
		t.Fatalf("want Len=1 after enqueue, got %d", q.Len())
	}

	got, ok, err := q.Dequeue()
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if !ok {
		t.Fatal("Dequeue returned ok=false on non-empty queue")
	}
	if got.ID != item.ID {
		t.Errorf("want ID=%q, got %q", item.ID, got.ID)
	}
	if q.Len() != 0 {
		t.Fatalf("want Len=0 after dequeue, got %d", q.Len())
	}
}

func TestFileQueue_EmptyDequeue(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	q, err := NewFileQueue(filepath.Join(dir, "empty.jsonl"))
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	_, ok, err := q.Dequeue()
	if err != nil {
		t.Fatalf("Dequeue on empty queue returned error: %v", err)
	}
	if ok {
		t.Fatal("Dequeue on empty queue returned ok=true")
	}
}

func TestFileQueue_Persistence(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "persist.jsonl")

	// Enqueue two items.
	q, err := NewFileQueue(path)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := q.Enqueue(model.QueueItem{
			ID:         fmt.Sprintf("item-%d", i),
			Payload:    map[string]any{"n": float64(i)},
			EnqueuedAt: time.Now().Unix(),
		}); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	// Reload from disk — simulate process restart.
	q2, err := NewFileQueue(path)
	if err != nil {
		t.Fatalf("NewFileQueue reload: %v", err)
	}
	if q2.Len() != 2 {
		t.Fatalf("want 2 items after reload, got %d", q2.Len())
	}
	item, ok, err := q2.Dequeue()
	if err != nil || !ok {
		t.Fatalf("Dequeue after reload: ok=%v err=%v", ok, err)
	}
	if item.ID != "item-0" {
		t.Errorf("FIFO order: want item-0, got %s", item.ID)
	}
}

func TestFileQueue_MultipleItems(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	q, err := NewFileQueue(filepath.Join(dir, "multi.jsonl"))
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	const n = 5
	for i := 0; i < n; i++ {
		q.Enqueue(model.QueueItem{ID: fmt.Sprintf("%d", i), EnqueuedAt: time.Now().Unix()}) //nolint:errcheck
	}
	if q.Len() != n {
		t.Fatalf("want %d, got %d", n, q.Len())
	}
	for i := 0; i < n; i++ {
		item, ok, err := q.Dequeue()
		if !ok || err != nil {
			t.Fatalf("step %d: ok=%v err=%v", i, ok, err)
		}
		if item.ID != fmt.Sprintf("%d", i) {
			t.Errorf("step %d: want %d, got %s", i, i, item.ID)
		}
	}
}

func TestManager_Get(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	mgr := NewManager(dir)

	q, err := mgr.Get("alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	// Second call returns the same instance.
	q2, err := mgr.Get("alpha")
	if err != nil {
		t.Fatalf("Get second call: %v", err)
	}
	if q != q2 {
		t.Error("expected same *FileQueue instance on repeated Get")
	}

	// Enqueue via convenience method.
	if err := mgr.Enqueue("alpha", model.QueueItem{ID: "x", EnqueuedAt: time.Now().Unix()}); err != nil {
		t.Fatalf("Enqueue via Manager: %v", err)
	}
	if q.Len() != 1 {
		t.Errorf("want Len=1, got %d", q.Len())
	}

	// File must be created in dir with mode 0600.
	fi, err := os.Stat(filepath.Join(dir, "alpha.jsonl"))
	if err != nil {
		t.Fatalf("stat queue file: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("want mode 0600, got %o", fi.Mode().Perm())
	}
}

func TestFileQueue_Peek(t *testing.T) {
	dir := t.TempDir()
	q, err := NewFileQueue(filepath.Join(dir, "peek.jsonl"))
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}

	// Peek on empty queue.
	_, ok := q.Peek()
	if ok {
		t.Fatal("Peek on empty queue should return ok=false")
	}

	item := model.QueueItem{ID: "peek-item", EnqueuedAt: time.Now().Unix()}
	if err := q.Enqueue(item); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	peeked, ok := q.Peek()
	if !ok {
		t.Fatal("Peek on non-empty queue returned ok=false")
	}
	if peeked.ID != item.ID {
		t.Errorf("Peek ID = %q, want %q", peeked.ID, item.ID)
	}
	// Peek should not remove the item.
	if q.Len() != 1 {
		t.Errorf("Len after Peek = %d, want 1", q.Len())
	}
}

func TestManager_Names(t *testing.T) {
	dir := t.TempDir()
	mgr := NewManager(dir)

	// No queues accessed yet.
	if names := mgr.Names(); len(names) != 0 {
		t.Errorf("Names on fresh manager = %v, want empty", names)
	}

	// Access two queues.
	if _, err := mgr.Get("beta"); err != nil {
		t.Fatalf("Get beta: %v", err)
	}
	if _, err := mgr.Get("alpha"); err != nil {
		t.Fatalf("Get alpha: %v", err)
	}

	names := mgr.Names()
	if len(names) != 2 {
		t.Fatalf("Names len = %d, want 2", len(names))
	}
	// Both names present (order not guaranteed).
	found := make(map[string]bool)
	for _, n := range names {
		found[n] = true
	}
	if !found["alpha"] || !found["beta"] {
		t.Errorf("Names = %v, want [alpha beta]", names)
	}
}

func TestFileQueue_Drain(t *testing.T) {
	tests := []struct {
		name      string
		enqueue   []string // item IDs to enqueue before drain
		wantCount int
	}{
		{name: "empty queue", enqueue: nil, wantCount: 0},
		{name: "single item", enqueue: []string{"a"}, wantCount: 1},
		{name: "multiple items", enqueue: []string{"x", "y", "z"}, wantCount: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Helper()
			dir := t.TempDir()
			q, err := NewFileQueue(filepath.Join(dir, "drain.jsonl"))
			if err != nil {
				t.Fatalf("NewFileQueue: %v", err)
			}
			for _, id := range tc.enqueue {
				if err := q.Enqueue(model.QueueItem{ID: id, EnqueuedAt: time.Now().Unix()}); err != nil {
					t.Fatalf("Enqueue %s: %v", id, err)
				}
			}
			items, err := q.Drain()
			if err != nil {
				t.Fatalf("Drain: %v", err)
			}
			if len(items) != tc.wantCount {
				t.Errorf("Drain returned %d items, want %d", len(items), tc.wantCount)
			}
			if q.Len() != 0 {
				t.Errorf("queue Len after Drain = %d, want 0", q.Len())
			}
			// Verify FIFO order of returned items.
			for i, item := range items {
				if item.ID != tc.enqueue[i] {
					t.Errorf("items[%d].ID = %q, want %q", i, item.ID, tc.enqueue[i])
				}
			}
			// Verify backing file is truncated (reload gives empty queue).
			q2, err := NewFileQueue(filepath.Join(dir, "drain.jsonl"))
			if err != nil {
				t.Fatalf("NewFileQueue reload: %v", err)
			}
			if q2.Len() != 0 {
				t.Errorf("reloaded queue Len = %d, want 0 after drain", q2.Len())
			}
		})
	}
}

// writeRawLines writes raw bytes directly to a queue backing file, bypassing
// the normal save path so we can inject corrupt or edge-case content.
func writeRawLines(t *testing.T, path string, lines []string) {
	t.Helper()
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeRawLines: %v", err)
	}
}

func TestLoad_SkipsCorruptLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.jsonl")

	good1 := `{"id":"a","enqueued_at":1}`
	good2 := `{"id":"b","enqueued_at":2}`
	good3 := `{"id":"c","enqueued_at":3}`
	bad := `{not valid json!!!`

	writeRawLines(t, path, []string{good1, bad, good2, good3})

	q, err := NewFileQueue(path)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	if q.Len() != 3 {
		t.Errorf("want 3 items loaded, got %d", q.Len())
	}
}

func TestLoad_AllCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allcorrupt.jsonl")
	writeRawLines(t, path, []string{`{bad`, `{also bad`, `!!`})

	q, err := NewFileQueue(path)
	if err != nil {
		t.Fatalf("NewFileQueue should not error on all-corrupt file, got: %v", err)
	}
	if q.Len() != 0 {
		t.Errorf("want 0 items, got %d", q.Len())
	}
}

func TestLoad_EmptyLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "emptylines.jsonl")

	good1 := `{"id":"x","enqueued_at":1}`
	good2 := `{"id":"y","enqueued_at":2}`
	writeRawLines(t, path, []string{good1, "", "   ", good2, ""})

	q, err := NewFileQueue(path)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	if q.Len() != 2 {
		t.Errorf("want 2 items, got %d", q.Len())
	}
}

func TestLoad_LargeItem(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.jsonl")

	// Build a payload that exceeds the default 64KB scanner buffer.
	bigVal := strings.Repeat("x", 200*1024) // 200 KB
	line := fmt.Sprintf(`{"id":"big","enqueued_at":1,"payload":{"v":%q}}`, bigVal)
	writeRawLines(t, path, []string{line})

	q, err := NewFileQueue(path)
	if err != nil {
		t.Fatalf("NewFileQueue: %v", err)
	}
	if q.Len() != 1 {
		t.Errorf("want 1 item, got %d", q.Len())
	}
}
