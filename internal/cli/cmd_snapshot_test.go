package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeSnapshotStateDir populates a minimal state directory for snapshot tests.
func makeSnapshotStateDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dirs := []string{
		filepath.Join(dir, "queues"),
		filepath.Join(dir, "runs"),
		filepath.Join(dir, "cache"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	files := map[string]string{
		filepath.Join(dir, "queues", "work.jsonl"):   `{"id":"q1"}` + "\n",
		filepath.Join(dir, "runs", "my-agent.jsonl"): `{"agent":"my-agent"}` + "\n",
		filepath.Join(dir, "cache", "resp.json"):     `{"cached":true}` + "\n",
		filepath.Join(dir, "leather.lock"):           "",   // should be skipped
		filepath.Join(dir, "devtools.token"):         "tk", // should be skipped
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return dir
}

func TestSnapshotSaveRestore_RoundTrip(t *testing.T) {
	stateDir := makeSnapshotStateDir(t)
	archivePath := filepath.Join(t.TempDir(), "snap.tar.gz")
	restoreDir := t.TempDir()

	var stdout, stderr bytes.Buffer

	// Save
	code := RunSnapshotSave([]string{
		"--state-dir", stateDir,
		"--output", archivePath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("save exited %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(archivePath); err != nil {
		t.Fatalf("archive not created: %v", err)
	}

	stdout.Reset()
	stderr.Reset()

	// Restore into empty dir
	code = RunSnapshotRestore([]string{
		"--state-dir", restoreDir,
		"--input", archivePath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("restore exited %d: %s", code, stderr.String())
	}

	// Verify files present in restored dir
	wantFiles := []string{
		filepath.Join(restoreDir, "queues", "work.jsonl"),
		filepath.Join(restoreDir, "runs", "my-agent.jsonl"),
		filepath.Join(restoreDir, "cache", "resp.json"),
	}
	for _, wf := range wantFiles {
		if _, err := os.Stat(wf); err != nil {
			t.Errorf("expected restored file %s: %v", wf, err)
		}
	}

	// Verify transient files were NOT restored
	for _, skip := range []string{"leather.lock", "devtools.token"} {
		p := filepath.Join(restoreDir, skip)
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("transient file %s should not have been restored", skip)
		}
	}

	// Verify content round-trip for one file
	got, err := os.ReadFile(filepath.Join(restoreDir, "queues", "work.jsonl"))
	if err != nil {
		t.Fatalf("read restored queue file: %v", err)
	}
	if string(got) != `{"id":"q1"}`+"\n" {
		t.Errorf("content mismatch: %q", got)
	}
}

func TestSnapshotSave_LockHeld(t *testing.T) {
	stateDir := t.TempDir()
	lockPath := filepath.Join(stateDir, "leather.lock")
	lf, err := acquireProcessLock(lockPath)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer releaseProcessLock(lf)

	var stdout, stderr bytes.Buffer
	code := RunSnapshotSave([]string{
		"--state-dir", stateDir,
		"--output", filepath.Join(t.TempDir(), "snap.tar.gz"),
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit when lock is held, got 0")
	}
	if !strings.Contains(stderr.String(), "serve is running") {
		t.Errorf("expected 'serve is running' in stderr, got: %s", stderr.String())
	}
}

func TestSnapshotRestore_NonEmptyNoForce(t *testing.T) {
	stateDir := makeSnapshotStateDir(t)
	archivePath := filepath.Join(t.TempDir(), "snap.tar.gz")

	// Create archive from a fresh state dir
	var stdout, stderr bytes.Buffer
	code := RunSnapshotSave([]string{
		"--state-dir", stateDir,
		"--output", archivePath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("save failed: %s", stderr.String())
	}

	// Restore into same non-empty dir without --force
	stderr.Reset()
	stdout.Reset()
	destDir := t.TempDir()
	// Put something in destDir
	if err := os.WriteFile(filepath.Join(destDir, "existing.jsonl"), []byte("x\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code = RunSnapshotRestore([]string{
		"--state-dir", destDir,
		"--input", archivePath,
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit for non-empty dir without --force")
	}
	if !strings.Contains(stderr.String(), "not empty") {
		t.Errorf("expected 'not empty' in stderr, got: %s", stderr.String())
	}
}

func TestSnapshotRestore_MissingInput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunSnapshotRestore([]string{
		"--state-dir", t.TempDir(),
		"--input", "/nonexistent/path/snap.tar.gz",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected non-zero exit for missing input file")
	}
}
