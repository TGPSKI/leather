// Package queue provides a durable, file-backed FIFO queue for QueueItems.
//
// Items are persisted as JSONL (one JSON object per line) in a single flat file.
// All operations are protected by a mutex; Enqueue and Dequeue atomically
// rewrite the backing file to ensure consistency.
package queue

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/tgpski/leather/internal/fileutil"
	"github.com/tgpski/leather/internal/ids"
	"github.com/tgpski/leather/internal/model"
)

// FileQueue is a mutex-protected, JSONL-backed FIFO queue.
// Create with NewFileQueue; all exported methods are safe for concurrent use.
type FileQueue struct {
	mu    sync.Mutex
	path  string
	items []model.QueueItem
}

// NewFileQueue creates a FileQueue backed by path, loading any existing items.
// If path does not exist, the queue starts empty and the file is created on
// the first Enqueue call.
func NewFileQueue(path string) (*FileQueue, error) {
	q := &FileQueue{path: path}
	if err := q.load(); err != nil {
		return nil, fmt.Errorf("queue/NewFileQueue %s: %w", path, err)
	}
	return q, nil
}

// Enqueue appends item to the tail of the queue and persists to disk.
func (q *FileQueue) Enqueue(item model.QueueItem) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.items = append(q.items, item)
	return q.save()
}

// Dequeue removes and returns the head item. Returns (item, true, nil) on
// success, ({}, false, nil) when the queue is empty, and ({}, false, err)
// if a persistence error occurs.
func (q *FileQueue) Dequeue() (model.QueueItem, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return model.QueueItem{}, false, nil
	}
	item := q.items[0]
	q.items = q.items[1:]
	if err := q.save(); err != nil {
		// Roll back the in-memory pop so state stays consistent.
		q.items = append([]model.QueueItem{item}, q.items...)
		return model.QueueItem{}, false, fmt.Errorf("queue/Dequeue: %w", err)
	}
	return item, true, nil
}

// Len returns the number of items currently in the queue.
func (q *FileQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// Peek returns the head item without removing it.
// Returns ({}, false) when the queue is empty.
func (q *FileQueue) Peek() (model.QueueItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return model.QueueItem{}, false
	}
	return q.items[0], true
}

// Scan returns a snapshot of all items currently in the queue without removing any.
// The returned slice is a copy; mutating it does not affect the queue.
func (q *FileQueue) Scan() []model.QueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]model.QueueItem, len(q.items))
	copy(out, q.items)
	return out
}

// DequeueByIDs removes and returns the items whose IDs appear in ids, preserving
// queue order for the remaining items. IDs not found in the queue are silently
// ignored. Returns the matched items in queue order and persists the change.
func (q *FileQueue) DequeueByIDs(ids []string) ([]model.QueueItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	want := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		want[id] = struct{}{}
	}
	var matched, remaining []model.QueueItem
	for _, item := range q.items {
		if _, ok := want[item.ID]; ok {
			matched = append(matched, item)
		} else {
			remaining = append(remaining, item)
		}
	}
	if len(matched) == 0 {
		return nil, nil
	}
	q.items = remaining
	if err := q.save(); err != nil {
		// Roll back so state stays consistent.
		q.items = append(matched, remaining...)
		return nil, fmt.Errorf("queue/DequeueByIDs: %w", err)
	}
	return matched, nil
}

// Drain removes and returns all items from the queue. The backing file is truncated.
func (q *FileQueue) Drain() ([]model.QueueItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	items := make([]model.QueueItem, len(q.items))
	copy(items, q.items)
	q.items = nil
	return items, q.save()
}

// load reads the backing JSONL file into q.items.
// Corrupt or unparseable lines are skipped with a warning written to stderr;
// the remaining valid items are loaded. This ensures a single bad line from a
// prior partial write does not discard all subsequent queue items.
// It is called only from NewFileQueue before the queue is shared.
func (q *FileQueue) load() error {
	f, err := os.Open(q.path)
	if os.IsNotExist(err) {
		return nil // empty queue; file created on first Enqueue
	}
	if err != nil {
		return fmt.Errorf("queue/load: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024) // allow items up to 4 MB
	lineNum := 0
	skipped := 0
	for scanner.Scan() {
		lineNum++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var item model.QueueItem
		if err := json.Unmarshal(line, &item); err != nil {
			fmt.Fprintf(os.Stderr, "queue/load: skipped corrupt line path=%s line=%d error=%v\n",
				q.path, lineNum, err)
			skipped++
			continue
		}
		q.items = append(q.items, item)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("queue/load: scan: %w", err)
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "queue/load: recovered with skipped lines path=%s loaded=%d skipped=%d\n",
			q.path, len(q.items), skipped)
	}
	return nil
}

// save atomically rewrites the backing JSONL file via a temp-file rename.
// Must be called with q.mu held.
func (q *FileQueue) save() error {
	return fileutil.AtomicWriteFileFunc(q.path, 0600, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		for _, item := range q.items {
			if err := enc.Encode(item); err != nil {
				return err
			}
		}
		return nil
	})
}

// GenerateItemID generates a unique queue item ID in the form
// "item_<yyyymmdd>_<HHMM>_<4hex>".
func GenerateItemID() string {
	return ids.TimestampHex("item")
}
