package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInit_DefaultDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()

	var out, errOut bytes.Buffer
	code := RunInit([]string{"--dir", dir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, errOut.String())
	}

	want := []string{
		"config.yaml",
		filepath.Join("agents", "my-agent.agent.md"),
		filepath.Join("agents", "my-agent.lifecycle.yaml"),
		"Makefile",
	}
	for _, rel := range want {
		path := filepath.Join(dir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist: %v", rel, err)
		}
	}

	stdout := out.String()
	if !strings.Contains(stdout, "Project initialised") {
		t.Errorf("stdout %q missing success message", stdout)
	}
	if !strings.Contains(stdout, dir) {
		t.Errorf("stdout %q missing target path", stdout)
	}
}

func TestRunInit_CustomDir_CreatedIfAbsent(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "newproject")

	var out bytes.Buffer
	code := RunInit([]string{"--dir", target}, &out, io.Discard)
	if code != 0 {
		t.Fatalf("exit code = %d for non-existent target dir", code)
	}

	if _, err := os.Stat(filepath.Join(target, "config.yaml")); err != nil {
		t.Errorf("config.yaml not created in new directory: %v", err)
	}
}

func TestRunInit_NoOverwrite_FailsOnExistingFiles(t *testing.T) {
	dir := t.TempDir()

	// First init succeeds.
	var out1 bytes.Buffer
	if code := RunInit([]string{"--dir", dir}, &out1, io.Discard); code != 0 {
		t.Fatalf("first init failed with code %d", code)
	}

	// Second init without --overwrite must fail.
	var out2, errOut2 bytes.Buffer
	code := RunInit([]string{"--dir", dir}, &out2, &errOut2)
	if code == 0 {
		t.Fatal("expected non-zero exit when files already exist, got 0")
	}

	stderr := errOut2.String()
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("stderr %q missing 'already exists' message", stderr)
	}
	if !strings.Contains(stderr, "--overwrite") {
		t.Errorf("stderr %q missing '--overwrite' hint", stderr)
	}
}

func TestRunInit_Overwrite_ReplacesExistingFiles(t *testing.T) {
	dir := t.TempDir()

	// First init.
	if code := RunInit([]string{"--dir", dir}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("first init failed")
	}

	// Corrupt the config to confirm overwrite replaces it.
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("NOT YAML"), 0644); err != nil {
		t.Fatal(err)
	}

	// Second init with --overwrite must succeed and restore valid content.
	var errOut bytes.Buffer
	code := RunInit([]string{"--dir", dir, "--overwrite"}, io.Discard, &errOut)
	if code != 0 {
		t.Fatalf("overwrite init failed with code %d, stderr = %q", code, errOut.String())
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "NOT YAML" {
		t.Error("config.yaml was not replaced by --overwrite")
	}
}

func TestRunInit_ScaffoldedFilesPassSchemaValidation(t *testing.T) {
	dir := t.TempDir()

	var errOut bytes.Buffer
	code := RunInit([]string{"--dir", dir}, io.Discard, &errOut)
	if code != 0 {
		t.Fatalf("init failed: %s", errOut.String())
	}

	// runInitValidate is called inside RunInit; a non-zero exit would have
	// already failed above. Confirm no schema violation lines leaked to stderr.
	if strings.Contains(errOut.String(), "field") {
		t.Errorf("schema violations found in stderr: %s", errOut.String())
	}
}

func TestRunInit_CreatesOutputLines(t *testing.T) {
	dir := t.TempDir()

	var out bytes.Buffer
	RunInit([]string{"--dir", dir}, &out, io.Discard) //nolint:errcheck

	stdout := out.String()
	for _, want := range []string{"created:", ".env", "config.yaml", "my-agent.agent.md", "my-agent.lifecycle.yaml", "Makefile"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q\nfull output:\n%s", want, stdout)
		}
	}
}

func TestRun_Init_Dispatches(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	code := Run([]string{"init", "--dir", dir}, &out, io.Discard, "dev", "none")
	if code != 0 {
		t.Errorf("Run(init) exit code = %d", code)
	}
}
