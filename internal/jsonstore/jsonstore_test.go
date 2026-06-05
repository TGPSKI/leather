package jsonstore

import (
	"os"
	"path/filepath"
	"testing"
)

type record struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "rec.json")
	want := record{Name: "leather", Count: 3}

	if err := Save(path, want, 0600); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var got record
	found, err := Load(path, &got)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("perm = %o, want 0600", perm)
	}
}

func TestLoadMissingFile(t *testing.T) {
	var got record
	found, err := Load(filepath.Join(t.TempDir(), "absent.json"), &got)
	if err != nil {
		t.Fatalf("Load missing: unexpected err %v", err)
	}
	if found {
		t.Error("found = true, want false for missing file")
	}
	if got != (record{}) {
		t.Errorf("v mutated on missing file: %+v", got)
	}
}

func TestLoadMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	var got record
	if _, err := Load(path, &got); err == nil {
		t.Error("Load malformed: want error, got nil")
	}
}

func TestSaveUnmarshalableValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chan.json")
	if err := Save(path, make(chan int), 0600); err == nil {
		t.Error("Save unmarshalable: want error, got nil")
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Error("file should not be created when marshal fails")
	}
}
