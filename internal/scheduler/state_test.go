package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tgpski/leather/internal/model"
)

func TestSaveLoadState_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	jobs := []model.Job{
		{AgentName: "alpha", Status: model.JobStatusSuccess, LastRun: 1000, RunCount: 3},
		{AgentName: "beta", Status: model.JobStatusError, LastError: "connection refused"},
	}

	if err := saveState(dir, jobs); err != nil {
		t.Fatalf("saveState: %v", err)
	}

	got, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(got) != len(jobs) {
		t.Fatalf("len = %d, want %d", len(got), len(jobs))
	}
	for i, want := range jobs {
		if got[i].AgentName != want.AgentName {
			t.Errorf("[%d] AgentName = %q, want %q", i, got[i].AgentName, want.AgentName)
		}
		if got[i].Status != want.Status {
			t.Errorf("[%d] Status = %q, want %q", i, got[i].Status, want.Status)
		}
		if got[i].LastError != want.LastError {
			t.Errorf("[%d] LastError = %q, want %q", i, got[i].LastError, want.LastError)
		}
		if got[i].RunCount != want.RunCount {
			t.Errorf("[%d] RunCount = %d, want %d", i, got[i].RunCount, want.RunCount)
		}
	}
}

func TestLoadState_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	jobs, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState on empty dir: unexpected error: %v", err)
	}
	if jobs == nil {
		t.Error("expected non-nil slice, got nil")
	}
	if len(jobs) != 0 {
		t.Errorf("expected empty slice, got %d jobs", len(jobs))
	}
}

func TestSaveState_EmptyDir_NoOp(t *testing.T) {
	if err := saveState("", []model.Job{{AgentName: "x"}}); err != nil {
		t.Fatalf("saveState with empty dir should be no-op: %v", err)
	}
}

func TestLoadState_EmptyDir_NoOp(t *testing.T) {
	jobs, err := LoadState("")
	if err != nil {
		t.Fatalf("LoadState with empty dir should be no-op: %v", err)
	}
	if jobs == nil {
		t.Error("expected non-nil slice")
	}
}

func TestSaveState_FilePermissions(t *testing.T) {
	dir := t.TempDir()
	if err := saveState(dir, []model.Job{}); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("file permissions = %04o, want 0600", perm)
	}
}

func TestSaveState_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	jobs := []model.Job{{AgentName: "z", RunCount: 7}}
	if err := saveState(dir, jobs); err != nil {
		t.Fatalf("saveState: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var out []model.Job
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 || out[0].AgentName != "z" || out[0].RunCount != 7 {
		t.Errorf("unexpected content: %+v", out)
	}
}

func TestSaveState_Overwrites(t *testing.T) {
	dir := t.TempDir()
	first := []model.Job{{AgentName: "first"}}
	second := []model.Job{{AgentName: "second"}, {AgentName: "third"}}

	if err := saveState(dir, first); err != nil {
		t.Fatalf("saveState first: %v", err)
	}
	if err := saveState(dir, second); err != nil {
		t.Fatalf("saveState second: %v", err)
	}
	got, err := LoadState(dir)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 jobs after overwrite, got %d", len(got))
	}
	if got[0].AgentName != "second" {
		t.Errorf("expected 'second', got %q", got[0].AgentName)
	}
}
