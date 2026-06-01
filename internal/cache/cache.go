// Package cache provides a sha256-keyed, file-backed response cache with
// per-entry TTL.
//
// Cache entries are stored as JSON files under a configured directory:
//
//	<dir>/<sha256key>.json   →  {"value":"...","expires_at":1234567890}
//
// File permissions are 0600. Expiry is lazy: expired entries are deleted on
// the first Get call that reads them. Set uses an atomic tmp-file rename so
// partial writes are never visible.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// entry is the on-disk JSON representation of a single cache record.
type entry struct {
	// Value is the cached response content.
	Value string `json:"value"`
	// ExpiresAt is the Unix timestamp (seconds) after which the entry is stale.
	// Zero means the entry never expires.
	ExpiresAt int64 `json:"expires_at"`
}

// FileCache is a sha256-keyed file cache. Create with NewFileCache.
// All exported methods are safe for concurrent use; each call is a separate
// file-system operation protected only by OS-level atomicity.
type FileCache struct {
	dir string
}

// NewFileCache returns a FileCache that stores entries under dir.
// The directory is created if it does not exist.
func NewFileCache(dir string) (*FileCache, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("cache/NewFileCache: %w", err)
	}
	return &FileCache{dir: dir}, nil
}

// Get returns (value, true) when a non-expired entry exists for key.
// Expired entries are deleted lazily. Returns ("", false) on any miss or error.
func (c *FileCache) Get(key string) (string, bool) {
	path := c.entryPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return "", false
	}
	if e.ExpiresAt != 0 && time.Now().Unix() >= e.ExpiresAt {
		_ = os.Remove(path) // best-effort lazy expiry
		return "", false
	}
	return e.Value, true
}

// Set writes value into the cache under key with the given ttl.
// A ttl of zero means the entry never expires.
// Returns an error if the file cannot be written.
func (c *FileCache) Set(key, value string, ttl time.Duration) error {
	e := entry{Value: value}
	if ttl > 0 {
		e.ExpiresAt = time.Now().Add(ttl).Unix()
	}
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("cache/Set: marshal: %w", err)
	}
	// Atomic write: write to a temp file then rename.
	tmp := c.entryPath(key) + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("cache/Set: write tmp: %w", err)
	}
	if err := os.Rename(tmp, c.entryPath(key)); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cache/Set: rename: %w", err)
	}
	return nil
}

// entryPath returns the file path for a given key.
func (c *FileCache) entryPath(key string) string {
	return filepath.Join(c.dir, key+".json")
}
