package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTanneryYAML writes a minimal tannery.yaml with hide_dir, curing_dir, and artifact_dir
// pointing to subdirs of base. It creates the directories.
func writeTanneryYAML(t *testing.T, base string) string {
	t.Helper()
	hideDir := filepath.Join(base, "hides")
	curingDir := filepath.Join(base, "curings")
	artDir := filepath.Join(base, "artifacts")
	for _, d := range []string{hideDir, curingDir, artDir} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	content := "hide_dir: " + hideDir + "\ncuring_dir: " + curingDir + "\nartifact_dir: " + artDir + "\n"
	path := filepath.Join(base, "tannery.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunIngest_File(t *testing.T) {
	dir := t.TempDir()
	tannPath := writeTanneryYAML(t, dir)
	stateDir := filepath.Join(dir, "state")

	// Write a file to ingest.
	inputFile := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(inputFile, []byte("hello from file"), 0600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunIngest([]string{
		"--tannery", tannPath,
		"--kind", "test.raw",
		"--state-dir", stateDir,
		inputFile,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "hide_id") {
		t.Errorf("expected 'hide_id' in output, got:\n%s", out)
	}
}

func TestRunIngest_Stdin(t *testing.T) {
	dir := t.TempDir()
	tannPath := writeTanneryYAML(t, dir)
	stateDir := filepath.Join(dir, "state")

	// Redirect stdin to a reader with our payload.
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() { os.Stdin = old }()
	_, _ = w.WriteString("hello from stdin")
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := RunIngest([]string{
		"--tannery", tannPath,
		"--kind", "test.raw",
		"--state-dir", stateDir,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "hide_id") {
		t.Errorf("expected 'hide_id' in output, got:\n%s", stdout.String())
	}
}

func TestRunIngest_DryRun(t *testing.T) {
	dir := t.TempDir()
	tannPath := writeTanneryYAML(t, dir)
	stateDir := filepath.Join(dir, "state")

	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() { os.Stdin = old }()
	_, _ = w.WriteString("dry run payload")
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := RunIngest([]string{
		"--tannery", tannPath,
		"--kind", "test.raw",
		"--state-dir", stateDir,
		"--dry-run",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "[dry-run]") {
		t.Errorf("expected '[dry-run]' prefix in output, got:\n%s", out)
	}
	// Confirm no hide was written to disk.
	entries, _ := os.ReadDir(filepath.Join(dir, "hides"))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".lock") {
			t.Errorf("unexpected file in hides dir during dry-run: %s", e.Name())
		}
	}
}

func TestRunIngest_MissingKind(t *testing.T) {
	dir := t.TempDir()
	tannPath := writeTanneryYAML(t, dir)

	var stdout, stderr bytes.Buffer
	code := RunIngest([]string{
		"--tannery", tannPath,
		// --kind omitted
	}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunIngest_MissingTannery(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunIngest([]string{
		"--kind", "test.raw",
		// --tannery omitted
	}, &stdout, &stderr)

	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}
