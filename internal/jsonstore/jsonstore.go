// Package jsonstore persists Go values as JSON files on disk. Save marshals a
// value and writes it atomically (via internal/fileutil); Load reads and
// unmarshals one, reporting absence without an error so callers can treat a
// missing file as empty state. It is the shared home for the marshal+atomic
// write and read+unmarshal patterns previously duplicated across the scheduler,
// cache, artifact, and hide stores.
package jsonstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/tgpski/leather/internal/fileutil"
)

// Save marshals v to JSON and writes it to path atomically with the given file
// permission. The parent directory is created (mode 0700) if needed.
func Save(path string, v any, perm os.FileMode) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("jsonstore: marshal %q: %w", path, err)
	}
	if err := fileutil.AtomicWriteFile(path, data, perm); err != nil {
		return fmt.Errorf("jsonstore: save %q: %w", path, err)
	}
	return nil
}

// Load reads path and unmarshals its JSON contents into v. It returns
// (false, nil) when the file does not exist, leaving v unmodified, so callers
// can distinguish "absent" from a decode error. A successful load returns
// (true, nil).
func Load(path string, v any) (found bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("jsonstore: read %q: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("jsonstore: unmarshal %q: %w", path, err)
	}
	return true, nil
}
