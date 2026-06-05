// Package fileutil provides small filesystem helpers shared across leather:
// existence checks and atomic file writes via a temp-file rename. All writes
// create the parent directory (0700) if needed and replace the destination
// atomically so readers never observe a partially written file.
package fileutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Exists reports whether path refers to an existing filesystem entry.
// Errors other than "not exist" (e.g. permission denied) are treated as
// "exists" so callers do not silently skip an entry they cannot stat.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// AtomicWriteFile writes data to path atomically. It creates the parent
// directory (mode 0700) if necessary, writes to a temp file in the same
// directory with the given perm, then renames it over path. On any error the
// temp file is removed and path is left untouched.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return AtomicWriteFileFunc(path, perm, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	})
}

// AtomicWriteFileFunc is AtomicWriteFile with a streaming writer: the file
// contents are produced by write. This suits encoders (e.g. a JSONL
// json.Encoder loop) that emit incrementally rather than from a single buffer.
func AtomicWriteFileFunc(path string, perm os.FileMode, write func(w io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("fileutil: mkdir %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("fileutil: create temp in %q: %w", dir, err)
	}
	tmpName := tmp.Name()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("fileutil: chmod %q: %w", tmpName, err)
	}
	if err := write(tmp); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("fileutil: write %q: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("fileutil: close %q: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("fileutil: rename %q -> %q: %w", tmpName, path, err)
	}
	return nil
}
