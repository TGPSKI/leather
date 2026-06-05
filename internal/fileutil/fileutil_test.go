package fileutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestExists(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "present")
	if err := os.WriteFile(existing, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "existing file", path: existing, want: true},
		{name: "existing dir", path: dir, want: true},
		{name: "missing file", path: filepath.Join(dir, "absent"), want: false},
		{name: "missing parent", path: filepath.Join(dir, "no", "such", "file"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Exists(tt.path); got != tt.want {
				t.Errorf("Exists(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestAtomicWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "data.bin") // nested: parent must be created

	if err := AtomicWriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatalf("first write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first" {
		t.Errorf("content = %q, want %q", got, "first")
	}

	// Overwrite replaces contents atomically.
	if err := AtomicWriteFile(path, []byte("second"), 0600); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "second" {
		t.Errorf("content after overwrite = %q, want %q", got, "second")
	}

	// Permission bits are honored.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("perm = %o, want 0600", perm)
	}

	// No leftover temp files in the directory.
	assertNoTempFiles(t, filepath.Dir(path))
}

func TestAtomicWriteFileFunc(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.txt")

	err := AtomicWriteFileFunc(path, 0644, func(w io.Writer) error {
		for _, s := range []string{"a\n", "b\n", "c\n"} {
			if _, err := io.WriteString(w, s); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("AtomicWriteFileFunc: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "a\nb\nc\n" {
		t.Errorf("content = %q", got)
	}
}

func TestAtomicWriteFileFunc_WriteErrorCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fail.txt")
	sentinel := errors.New("boom")

	err := AtomicWriteFileFunc(path, 0600, func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want wrapped %v", err, sentinel)
	}
	if Exists(path) {
		t.Errorf("destination %q should not exist after a failed write", path)
	}
	assertNoTempFiles(t, dir)
}

func TestAtomicWriteFile_MkdirFails(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file, then try to write "under" it as if it were a
	// directory. MkdirAll must fail because a path component is not a directory.
	notADir := filepath.Join(dir, "file")
	if err := os.WriteFile(notADir, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	err := AtomicWriteFile(filepath.Join(notADir, "child", "data"), []byte("y"), 0600)
	if err == nil {
		t.Fatal("expected error when parent path is a file, got nil")
	}
}

// assertNoTempFiles fails if any ".tmp-*" file remains in dir.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if len(e.Name()) >= 5 && e.Name()[:5] == ".tmp-" {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}
