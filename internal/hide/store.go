package hide

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/tgpski/leather/internal/fileutil"
	"github.com/tgpski/leather/internal/jsonstore"
	"github.com/tgpski/leather/internal/safepath"
)

// StoreEntry is the persisted metadata for a stored hide.
type StoreEntry struct {
	// ID is the unique hide identifier.
	ID string `json:"id"`
	// Kind is the hide type label (e.g. "github.pr_review_thread").
	Kind string `json:"kind"`
	// Source is the origin: tool name, webhook path, or "cli".
	Source string `json:"source"`
	// SizeBytes is the byte count of the stored content.
	SizeBytes int64 `json:"size_bytes"`
	// CreatedAt is the Unix timestamp when the hide was written.
	CreatedAt int64 `json:"created_at"`
	// Metadata holds arbitrary key-value labels for this hide.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Store is a file-backed persistent store for raw hides (large inputs).
// Each hide occupies one subdirectory: <dir>/<id>/meta.json + <dir>/<id>/content.
// The Store type realizes the HideStore concept from the glossary.
type Store struct {
	dir string
}

// NewStore returns a Store rooted at dir. Dir need not exist yet.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Put writes content and metadata to disk; returns the generated StoreEntry.
// Write order: content first, then meta.json. If the process crashes between the two,
// the directory exists without meta.json — List silently skips it (partial-write guard).
func (s *Store) Put(kind, source string, content []byte, meta map[string]string) (StoreEntry, error) {
	id := generateHideID(kind)
	entryDir, err := safepath.Anchor(s.dir, id)
	if err != nil {
		return StoreEntry{}, fmt.Errorf("hide/store.Put: invalid id %s: %w", id, err)
	}
	if err := os.MkdirAll(entryDir, 0700); err != nil {
		return StoreEntry{}, fmt.Errorf("hide/store.Put: mkdir %s: %w", id, err)
	}

	// Write content first.
	contentPath := filepath.Join(entryDir, "content")
	if err := os.WriteFile(contentPath, content, 0600); err != nil {
		return StoreEntry{}, fmt.Errorf("hide/store.Put: write content %s: %w", id, err)
	}

	entry := StoreEntry{
		ID:        id,
		Kind:      kind,
		Source:    source,
		SizeBytes: int64(len(content)),
		CreatedAt: time.Now().Unix(),
		Metadata:  meta,
	}

	// Write meta second (crash-safe ordering: content exists before metadata).
	metaPath := filepath.Join(entryDir, "meta.json")
	if err := jsonstore.Save(metaPath, entry, 0600); err != nil {
		return StoreEntry{}, fmt.Errorf("hide/store.Put: write meta %s: %w", id, err)
	}
	return entry, nil
}

// Get returns the StoreEntry metadata and raw content for the given hide ID.
// Returns an os.ErrNotExist-wrapped error when the hide directory, content file,
// or meta.json file does not exist (including partial-write cases).
// Returns a different error only when meta.json exists but is malformed JSON.
func (s *Store) Get(id string) (StoreEntry, []byte, error) {
	entryDir, err := safepath.Anchor(s.dir, id)
	if err != nil {
		return StoreEntry{}, nil, fmt.Errorf("hide/store.Get %s: %w", id, err)
	}

	// Check directory exists.
	if !fileutil.Exists(entryDir) {
		return StoreEntry{}, nil, fmt.Errorf("hide/store.Get %s: %w", id, os.ErrNotExist)
	}

	// Read content.
	contentPath := filepath.Join(entryDir, "content")
	content, err := os.ReadFile(contentPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StoreEntry{}, nil, fmt.Errorf("hide/store.Get %s: content: %w", id, os.ErrNotExist)
		}
		return StoreEntry{}, nil, fmt.Errorf("hide/store.Get %s: read content: %w", id, err)
	}

	// Read meta.
	metaPath := filepath.Join(entryDir, "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StoreEntry{}, nil, fmt.Errorf("hide/store.Get %s: meta: %w", id, os.ErrNotExist)
		}
		return StoreEntry{}, nil, fmt.Errorf("hide/store.Get %s: read meta: %w", id, err)
	}

	var entry StoreEntry
	if err := json.Unmarshal(metaBytes, &entry); err != nil {
		return StoreEntry{}, nil, fmt.Errorf("hide/store.Get %s: parse meta: %w", id, err)
	}
	return entry, content, nil
}

// Cut returns a bounded page of a stored hide without loading the full content.
// Reads only the required byte range using ReadAt semantics. Reuses the Cut type.
func (s *Store) Cut(id string, page, pageSizeBytes int) (Cut, error) {
	if pageSizeBytes <= 0 {
		pageSizeBytes = 3800
	}
	entryDir, err := safepath.Anchor(s.dir, id)
	if err != nil {
		return Cut{}, fmt.Errorf("hide/store.Cut %s: %w", id, err)
	}

	// Read meta to get source.
	metaPath := filepath.Join(entryDir, "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Cut{}, fmt.Errorf("hide/store.Cut %s: %w", id, os.ErrNotExist)
		}
		return Cut{}, fmt.Errorf("hide/store.Cut %s: read meta: %w", id, err)
	}
	var entry StoreEntry
	if err := json.Unmarshal(metaBytes, &entry); err != nil {
		return Cut{}, fmt.Errorf("hide/store.Cut %s: parse meta: %w", id, err)
	}

	contentPath := filepath.Join(entryDir, "content")
	f, err := os.Open(contentPath)
	if err != nil {
		return Cut{}, fmt.Errorf("hide/store.Cut %s: open content: %w", id, err)
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return Cut{}, fmt.Errorf("hide/store.Cut %s: stat content: %w", id, err)
	}
	size := int(fi.Size())
	total := totalPages(size, pageSizeBytes)
	if page < 1 || page > total {
		return Cut{}, fmt.Errorf("hide/store.Cut %s: page %d out of range [1,%d]", id, page, total)
	}

	start := int64((page - 1) * pageSizeBytes)
	end := start + int64(pageSizeBytes)
	if end > fi.Size() {
		end = fi.Size()
	}
	buf := make([]byte, end-start)
	if _, err := f.ReadAt(buf, start); err != nil {
		return Cut{}, fmt.Errorf("hide/store.Cut %s: read page %d: %w", id, page, err)
	}

	return Cut{
		HideID:     id,
		Source:     entry.Source,
		PageNumber: page,
		TotalPages: total,
		SizeBytes:  len(buf),
		Content:    string(buf),
		IsFinal:    page == total,
	}, nil
}

// List returns all StoreEntry values sorted by CreatedAt descending.
// Silently skips directories with missing or malformed meta.json.
func (s *Store) List() ([]StoreEntry, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("hide/store.List: %w", err)
	}

	var out []StoreEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(s.dir, e.Name(), "meta.json")
		metaBytes, err := os.ReadFile(metaPath)
		if err != nil {
			continue // skip partial writes or non-hide dirs
		}
		var entry StoreEntry
		if err := json.Unmarshal(metaBytes, &entry); err != nil {
			continue // skip malformed
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

// Delete removes the hide directory and all its contents from disk.
func (s *Store) Delete(id string) error {
	entryDir, err := safepath.Anchor(s.dir, id)
	if err != nil {
		return fmt.Errorf("hide/store.Delete %s: %w", id, err)
	}
	if err := os.RemoveAll(entryDir); err != nil {
		return fmt.Errorf("hide/store.Delete %s: %w", id, err)
	}
	return nil
}

// LoadIntoBuffer reads the full hide content and returns an in-memory HideBuffer.
// This is the bridge between the persistent Store and the runner's HideBuffer field.
// pageSize <= 0 defaults to 3800.
func (s *Store) LoadIntoBuffer(id string, pageSize int) (*HideBuffer, error) {
	if pageSize <= 0 {
		pageSize = 3800
	}
	entry, content, err := s.Get(id)
	if err != nil {
		return nil, fmt.Errorf("hide/store.LoadIntoBuffer: %w", err)
	}
	buf := NewHideBuffer(pageSize)
	buf.storeWithID(entry.ID, entry.Source, string(content), time.Unix(entry.CreatedAt, 0))
	return buf, nil
}
