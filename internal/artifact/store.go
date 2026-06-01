// Package artifact provides a file-backed store for curing output artifacts.
// Each artifact is written as a JSON file under <dir>/<curing-name>/<artifact-id>.json.
// The Store type realizes the ArtifactStore concept from the glossary.
package artifact

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/safepath"
)

// Store is a file-backed store for model.Artifact values.
type Store struct {
	dir string
}

// NewStore returns a Store rooted at dir. Dir need not exist yet.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Write persists a to disk. The caller must set a.ID before calling Write.
// Layout: <dir>/<curing-name>/<artifact-id>.json at permission 0600.
func (s *Store) Write(a model.Artifact) error {
	curingDir, err := safepath.Anchor(s.dir, a.CuringName)
	if err != nil {
		return fmt.Errorf("artifact/store.Write: invalid curing name %s: %w", a.CuringName, err)
	}
	if err := os.MkdirAll(curingDir, 0700); err != nil {
		return fmt.Errorf("artifact/store.Write: mkdir %s: %w", a.CuringName, err)
	}
	data, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("artifact/store.Write: marshal %s: %w", a.ID, err)
	}
	// Anchor the artifact ID (with .json suffix) against the curing directory.
	// a.ID is caller-controlled so must be validated.
	artifactFile, err := safepath.Anchor(curingDir, a.ID+".json")
	if err != nil {
		return fmt.Errorf("artifact/store.Write: invalid artifact id %s: %w", a.ID, err)
	}
	if err := os.WriteFile(artifactFile, data, 0600); err != nil {
		return fmt.Errorf("artifact/store.Write: write %s: %w", a.ID, err)
	}
	return nil
}

// List returns all artifacts across all curing directories sorted by CreatedAt descending.
// Silently skips entries that cannot be parsed.
func (s *Store) List() ([]model.Artifact, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("artifact/store.List: %w", err)
	}

	var out []model.Artifact
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		arts, err := s.ListByCuring(e.Name())
		if err != nil {
			continue // skip unreadable curing directories
		}
		out = append(out, arts...)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

// ListByCuring returns all artifacts produced by the named curing workflow,
// sorted by CreatedAt descending. Returns (nil, nil) when the directory does not exist.
func (s *Store) ListByCuring(curingName string) ([]model.Artifact, error) {
	curingDir, err := safepath.Anchor(s.dir, curingName)
	if err != nil {
		return nil, fmt.Errorf("artifact/store.ListByCuring: invalid curing name %s: %w", curingName, err)
	}
	entries, err := os.ReadDir(curingDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("artifact/store.ListByCuring %s: %w", curingName, err)
	}

	var out []model.Artifact
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(curingDir, e.Name()))
		if err != nil {
			continue
		}
		var a model.Artifact
		if err := json.Unmarshal(data, &a); err != nil {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt > out[j].CreatedAt
	})
	return out, nil
}

// Get performs a linear scan across all curing directories and returns the
// artifact with the given ID. Returns os.ErrNotExist if not found.
func (s *Store) Get(id string) (model.Artifact, error) {
	all, err := s.List()
	if err != nil {
		return model.Artifact{}, fmt.Errorf("artifact/store.Get %s: %w", id, err)
	}
	for _, a := range all {
		if a.ID == id {
			return a, nil
		}
	}
	return model.Artifact{}, fmt.Errorf("artifact/store.Get %s: %w", id, os.ErrNotExist)
}

// GenerateArtifactID generates a unique artifact ID in the form
// "artifact_<yyyymmdd>_<HHMM>_<4hex>".
func GenerateArtifactID() string {
	now := time.Now()
	suffix := rand.Int31n(0x10000) //nolint:gosec
	return fmt.Sprintf("artifact_%s_%04x",
		now.Format("20060102_1504"),
		suffix,
	)
}
