package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/model"
)

// --- acquireProcessLock ---

func TestAcquireProcessLock_Single(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "leather.lock")

	f, err := acquireProcessLock(path)
	if err != nil {
		t.Fatalf("acquireProcessLock: %v", err)
	}
	defer releaseProcessLock(f)

	if f == nil {
		t.Error("expected non-nil *os.File")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("lock file should exist: %v", statErr)
	}
}

func TestAcquireProcessLock_Second_Fails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "leather.lock")

	f1, err := acquireProcessLock(path)
	if err != nil {
		t.Fatalf("first acquireProcessLock: %v", err)
	}
	defer releaseProcessLock(f1)

	_, err = acquireProcessLock(path)
	if err == nil {
		t.Error("expected error acquiring lock held by another descriptor")
	}
}

// --- initTannery ---

func TestInitTannery_EmptyFile_NoOp(t *testing.T) {
	deps := &apiDeps{log: testLogger(t)}
	td, err := initTannery(context.Background(), "", deps)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if td != nil {
		t.Errorf("expected nil tanneryDeps when tanneryFile is empty")
	}
}

func TestInitTannery_BadValidation_Fails(t *testing.T) {
	dir := t.TempDir()
	curingDir := filepath.Join(dir, "curings")
	if err := os.MkdirAll(curingDir, 0700); err != nil {
		t.Fatal(err)
	}

	// Route references a curing that doesn't exist in curing_dir.
	yamlContent := "hide_dir: " + filepath.Join(dir, "hides") + "\n" +
		"curing_dir: " + curingDir + "\n" +
		"artifact_dir: " + filepath.Join(dir, "artifacts") + "\n" +
		"routes:\n" +
		"  - name: r1\n" +
		"    match:\n" +
		"      source: github\n" +
		"    hide_kind: github.pr\n" +
		"    curing: nonexistent-curing\n" +
		"    queue: default\n" +
		"queues:\n" +
		"  default:\n" +
		"    concurrency: 1\n"

	cfgPath := filepath.Join(dir, "tannery.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0600); err != nil {
		t.Fatal(err)
	}

	deps := &apiDeps{
		cfg: model.Config{
			LLMEndpoint:   "http://localhost:11434",
			MaxToolRounds: 1,
		},
		queueMgr: nil, // no queue manager needed; validation fails before supervisor
		log:      testLogger(t),
	}

	_, err := initTannery(context.Background(), cfgPath, deps)
	if err == nil {
		t.Error("expected validation error for route referencing unknown curing")
	}
}

func TestInitTannery_MissingCuringDir_NoError(t *testing.T) {
	dir := t.TempDir()

	// tannery.yaml with no routes — zero curings is fine when routes are also empty.
	yamlContent := "hide_dir: " + filepath.Join(dir, "hides") + "\n" +
		"curing_dir: " + filepath.Join(dir, "no-such-curings") + "\n" +
		"artifact_dir: " + filepath.Join(dir, "artifacts") + "\n"
	cfgPath := filepath.Join(dir, "tannery.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0600); err != nil {
		t.Fatal(err)
	}

	deps := &apiDeps{
		cfg: model.Config{
			LLMEndpoint:   "http://localhost:11434",
			MaxToolRounds: 1,
		},
		log: testLogger(t),
	}

	td, err := initTannery(context.Background(), cfgPath, deps)
	if err != nil {
		t.Fatalf("expected no error for empty curing_dir with no routes, got %v", err)
	}
	defer drainTannery(td)
	if td == nil {
		t.Fatal("expected non-nil tanneryDeps")
	}
	if len(td.curingDefs) != 0 {
		t.Errorf("expected 0 curing defs, got %d", len(td.curingDefs))
	}
}

func TestInitTannery_LockConflict(t *testing.T) {
	dir := t.TempDir()
	hideDir := filepath.Join(dir, "hides")
	if err := os.MkdirAll(hideDir, 0700); err != nil {
		t.Fatal(err)
	}

	yamlContent := "hide_dir: " + hideDir + "\n" +
		"curing_dir: " + filepath.Join(dir, "curings") + "\n" +
		"artifact_dir: " + filepath.Join(dir, "artifacts") + "\n"
	cfgPath := filepath.Join(dir, "tannery.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Acquire the lock first.
	lockPath := filepath.Join(hideDir, "leather.lock")
	firstLock, err := acquireProcessLock(lockPath)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer releaseProcessLock(firstLock)

	deps := &apiDeps{
		cfg: model.Config{LLMEndpoint: "http://localhost:11434"},
		log: testLogger(t),
	}

	_, err = initTannery(context.Background(), cfgPath, deps)
	if err == nil {
		t.Error("expected error when hide_dir is already locked by another instance")
	}
}

// --- drainTannery ---

func TestDrainTannery_NilSafe(t *testing.T) {
	// Must not panic.
	drainTannery(nil)
}

// --- agentsByName ---

func TestAgentsByName(t *testing.T) {
	agents := []model.Agent{
		{Name: "alpha"},
		{Name: "beta"},
	}
	m := agentsByName(agents)
	if len(m) != 2 {
		t.Errorf("len = %d, want 2", len(m))
	}
	if _, ok := m["alpha"]; !ok {
		t.Error("alpha missing")
	}
	if _, ok := m["beta"]; !ok {
		t.Error("beta missing")
	}
}

// --- registerTanneryHandlers ---

func TestRegisterTanneryHandlers_NilNoOp(t *testing.T) {
	// Must not panic with nil tanneryDeps.
	registerTanneryHandlers(nil, nil, nil)
}

// --- ValidateTannery integration (config package, called from initTannery) ---

func TestValidateTannery_CalledOnInit(t *testing.T) {
	// Verify that ValidateTannery is invoked by checking it rejects bad config.
	dir := t.TempDir()
	yamlContent := "hide_dir: " + filepath.Join(dir, "hides") + "\n" +
		"curing_dir: " + filepath.Join(dir, "curings") + "\n" +
		"artifact_dir: " + filepath.Join(dir, "artifacts") + "\n" +
		"routes:\n" +
		"  - name: bad-route\n" +
		"    curing: missing\n" +
		"    queue: missing-queue\n" +
		"queues:\n" +
		"  default:\n" +
		"    concurrency: 1\n"
	cfgPath := filepath.Join(dir, "tannery.yaml")
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0600); err != nil {
		t.Fatal(err)
	}

	deps := &apiDeps{
		cfg: model.Config{LLMEndpoint: "http://localhost:11434"},
		log: testLogger(t),
	}

	_, err := initTannery(context.Background(), cfgPath, deps)
	if err == nil {
		t.Error("expected validation error")
	}

	// Verify it's the right kind of error.
	cfg := config.TanneryConfig{
		Routes: []model.TanneryRoute{{Name: "bad-route", Curing: "missing", Queue: "missing-queue"}},
		Queues: map[string]model.QueueConcurrencyConfig{"default": {}},
	}
	if verr := config.ValidateTannery(cfg, nil); verr == nil {
		t.Error("ValidateTannery should reject this config")
	}
}
