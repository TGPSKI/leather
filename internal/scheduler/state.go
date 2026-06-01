package scheduler

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tgpski/leather/internal/model"
)

const stateFileName = "jobs.json"

// saveState writes jobs to dir/jobs.json atomically using a temp-file rename.
// The file is created with permissions 0600. If dir is empty, saveState is a no-op.
func saveState(dir string, jobs []model.Job) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("scheduler/saveState: mkdir %q: %w", dir, err)
	}

	data, err := json.Marshal(jobs)
	if err != nil {
		return fmt.Errorf("scheduler/saveState: marshal: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".jobs-*.json")
	if err != nil {
		return fmt.Errorf("scheduler/saveState: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("scheduler/saveState: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("scheduler/saveState: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("scheduler/saveState: close: %w", err)
	}

	dest := filepath.Join(dir, stateFileName)
	if err := os.Rename(tmpName, dest); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("scheduler/saveState: rename: %w", err)
	}
	return nil
}

// LoadState reads the persisted job records from dir/jobs.json.
// Returns an empty (non-nil) slice and no error when the file does not exist.
func LoadState(dir string) ([]model.Job, error) {
	if dir == "" {
		return []model.Job{}, nil
	}
	path := filepath.Join(dir, stateFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []model.Job{}, nil
		}
		return nil, fmt.Errorf("scheduler/LoadState: read %q: %w", path, err)
	}
	var jobs []model.Job
	if err := json.Unmarshal(data, &jobs); err != nil {
		return nil, fmt.Errorf("scheduler/LoadState: unmarshal %q: %w", path, err)
	}
	return jobs, nil
}
