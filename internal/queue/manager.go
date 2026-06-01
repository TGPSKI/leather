package queue

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/safepath"
)

// Manager holds named FileQueues, creating new ones on first access.
// All methods are safe for concurrent use.
type Manager struct {
	mu     sync.Mutex
	dir    string
	queues map[string]*FileQueue
}

// NewManager returns a Manager that stores queue files under dir.
// The directory is created lazily when the first queue file is written.
func NewManager(dir string) *Manager {
	return &Manager{
		dir:    dir,
		queues: make(map[string]*FileQueue),
	}
}

// Get returns the named queue, creating it if it does not yet exist.
// The backing file is <dir>/<name>.jsonl for static queues, or
// <dir>/<prefix>/<id>.jsonl for single-use queues whose name contains a "/".
func (m *Manager) Get(name string) (*FileQueue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if q, ok := m.queues[name]; ok {
		return q, nil
	}
	// Anchor the queue name against the manager directory to prevent path traversal.
	// name may contain "/" for single-use queue prefixes (e.g. "pr-meta/abc123").
	anchored, err := safepath.Anchor(m.dir, filepath.FromSlash(name)+".jsonl")
	if err != nil {
		return nil, fmt.Errorf("queue/Manager.Get: invalid name %q: %w", name, err)
	}
	if err := os.MkdirAll(filepath.Dir(anchored), 0700); err != nil {
		return nil, fmt.Errorf("queue/Manager.Get: mkdir %s: %w", filepath.Dir(anchored), err)
	}
	q, err := NewFileQueue(anchored)
	if err != nil {
		return nil, fmt.Errorf("queue/Manager.Get %q: %w", name, err)
	}
	m.queues[name] = q
	return q, nil
}

// Names returns the names of all queues that have been accessed via Get.
func (m *Manager) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.queues))
	for n := range m.queues {
		names = append(names, n)
	}
	return names
}

// Enqueue is a convenience method that calls Get then Enqueue.
func (m *Manager) Enqueue(queueName string, item model.QueueItem) error {
	q, err := m.Get(queueName)
	if err != nil {
		return err
	}
	return q.Enqueue(item)
}

// Depth returns the number of items currently in the named queue.
// Returns 0 when the queue has not been opened yet (file may still hold items;
// Depth is accurate only after Get has been called at least once for the queue).
func (m *Manager) Depth(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if q, ok := m.queues[name]; ok {
		return q.Len()
	}
	return 0
}

// NamesWithPrefix returns the names of all single-use queues stored under the
// <dir>/<prefix>/ subdirectory. Each returned name has the form "<prefix>/<id>".
// It reads the subdirectory directly so it discovers queues created by other
// processes or not yet opened via Get.
func (m *Manager) NamesWithPrefix(prefix string) ([]string, error) {
	prefixDir, err := safepath.Anchor(m.dir, prefix)
	if err != nil {
		return nil, fmt.Errorf("queue/Manager.NamesWithPrefix: invalid prefix %q: %w", prefix, err)
	}
	entries, err := os.ReadDir(prefixDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue/Manager.NamesWithPrefix: readdir %s: %w", prefixDir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := e.Name()
		if !strings.HasSuffix(base, ".jsonl") {
			continue
		}
		names = append(names, prefix+"/"+strings.TrimSuffix(base, ".jsonl"))
	}
	return names, nil
}

// NamesContaining returns the names of all queues on disk whose names contain
// substr as a substring. It reads the queue directory and one level of
// subdirectories directly so it discovers queues created by other workers or
// not yet opened via Get.
// Use this to check whether a hide_id is still referenced by any live queue.
func (m *Manager) NamesContaining(substr string) ([]string, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("queue/Manager.NamesContaining: readdir %s: %w", m.dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			// Walk one level of subdirectories (single-use queue prefix dirs).
			subDir := filepath.Join(m.dir, e.Name())
			subs, serr := os.ReadDir(subDir)
			if serr != nil {
				continue
			}
			for _, se := range subs {
				if se.IsDir() || !strings.HasSuffix(se.Name(), ".jsonl") {
					continue
				}
				name := e.Name() + "/" + strings.TrimSuffix(se.Name(), ".jsonl")
				if strings.Contains(name, substr) {
					names = append(names, name)
				}
			}
			continue
		}
		base := e.Name()
		if !strings.HasSuffix(base, ".jsonl") {
			continue
		}
		name := strings.TrimSuffix(base, ".jsonl")
		if strings.Contains(name, substr) {
			names = append(names, name)
		}
	}
	return names, nil
}

// Delete removes the named queue from the in-memory map and deletes its backing
// file. For single-use queues stored in a subdirectory, the subdirectory is
// removed when it becomes empty. Returns nil when the file does not exist (idempotent).
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	delete(m.queues, name)
	m.mu.Unlock()
	anchored, err := safepath.Anchor(m.dir, filepath.FromSlash(name)+".jsonl")
	if err != nil {
		return fmt.Errorf("queue/Manager.Delete %q: invalid name: %w", name, err)
	}
	if err := os.Remove(anchored); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("queue/Manager.Delete %q: %w", name, err)
	}
	// Remove the parent subdirectory when it is now empty (single-use queue dirs).
	parent := filepath.Dir(anchored)
	if parent != m.dir {
		_ = os.Remove(parent) // no-op if the directory is not empty
	}
	return nil
}

// EnqueueIfAbsent enqueues item into the named queue only when no item with the
// same ID already exists in the queue. It returns (true, nil) when enqueued and
// (false, nil) when skipped due to a duplicate. An error is returned only for
// I/O failures.
func (m *Manager) EnqueueIfAbsent(name string, item model.QueueItem) (bool, error) {
	q, err := m.Get(name)
	if err != nil {
		return false, fmt.Errorf("queue/Manager.EnqueueIfAbsent %q: %w", name, err)
	}
	// Scan existing items for a matching ID.
	for _, existing := range q.Scan() {
		if existing.ID == item.ID {
			return false, nil
		}
	}
	if err := q.Enqueue(item); err != nil {
		return false, fmt.Errorf("queue/Manager.EnqueueIfAbsent %q: enqueue: %w", name, err)
	}
	return true, nil
}
