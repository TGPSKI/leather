// Package safepath provides root-anchored path joins that reject traversal.
// All operations are pure functions with no I/O side effects.
package safepath

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrEscapesRoot is returned when a name resolves to a path outside the root.
var ErrEscapesRoot = errors.New("safepath: path escapes root")

// ErrAbsolute is returned when the name is an absolute path.
var ErrAbsolute = errors.New("safepath: name must not be absolute")

// ErrEmpty is returned when the name is empty.
var ErrEmpty = errors.New("safepath: name must not be empty")

// ErrNUL is returned when the name contains a NUL byte.
var ErrNUL = errors.New("safepath: name must not contain NUL")

// Anchor returns filepath.Join(root, name) after verifying the cleaned result
// remains within root. It rejects names that are:
//   - empty
//   - absolute paths
//   - contain NUL bytes
//   - resolve to a path outside root after cleaning (e.g. via ".." segments)
func Anchor(root, name string) (string, error) {
	if name == "" {
		return "", ErrEmpty
	}
	if strings.ContainsRune(name, 0) {
		return "", ErrNUL
	}
	if filepath.IsAbs(name) {
		return "", ErrAbsolute
	}

	// Clean root to a canonical form for prefix comparison.
	cleanRoot := filepath.Clean(root)

	joined := filepath.Join(cleanRoot, name)
	cleaned := filepath.Clean(joined)

	// The cleaned result must be the root itself or start with root + separator.
	if cleaned != cleanRoot && !strings.HasPrefix(cleaned, cleanRoot+string(filepath.Separator)) {
		return "", ErrEscapesRoot
	}

	return cleaned, nil
}
