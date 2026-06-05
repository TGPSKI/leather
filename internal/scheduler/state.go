package scheduler

import (
	"fmt"
	"path/filepath"

	"github.com/tgpski/leather/internal/jsonstore"
	"github.com/tgpski/leather/internal/model"
)

const stateFileName = "jobs.json"

// saveState writes jobs to dir/jobs.json atomically using a temp-file rename.
// The file is created with permissions 0600. If dir is empty, saveState is a no-op.
func saveState(dir string, jobs []model.Job) error {
	if dir == "" {
		return nil
	}
	if err := jsonstore.Save(filepath.Join(dir, stateFileName), jobs, 0600); err != nil {
		return fmt.Errorf("scheduler/saveState: %w", err)
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
	var jobs []model.Job
	found, err := jsonstore.Load(path, &jobs)
	if err != nil {
		return nil, fmt.Errorf("scheduler/LoadState: %w", err)
	}
	if !found {
		return []model.Job{}, nil
	}
	return jobs, nil
}
