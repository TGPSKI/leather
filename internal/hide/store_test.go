package hide

import (
	"errors"
	"os"
	"testing"
)

func TestStore_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	content := []byte("hello store")
	meta := map[string]string{"pr": "42"}

	entry, err := s.Put("github.pr_review_thread", "webhook", content, meta)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if entry.ID == "" {
		t.Error("Put: empty ID")
	}
	if entry.SizeBytes != int64(len(content)) {
		t.Errorf("SizeBytes: got %d, want %d", entry.SizeBytes, len(content))
	}
	if entry.Kind != "github.pr_review_thread" {
		t.Errorf("Kind: got %q", entry.Kind)
	}

	got, gotContent, err := s.Get(entry.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(gotContent) != string(content) {
		t.Errorf("content mismatch: got %q", gotContent)
	}
	if got.Metadata["pr"] != "42" {
		t.Errorf("metadata: got %v", got.Metadata)
	}
}

func TestStore_Get_NotExist(t *testing.T) {
	s := NewStore(t.TempDir())
	_, _, err := s.Get("hide_does_not_exist_20260101_0000_0000")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestStore_Get_PartialWrite_NoMeta(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	// Write content but no meta.json.
	entryDir := dir + "/hide_partial_20260101_0000_abcd"
	if err := os.MkdirAll(entryDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryDir+"/content", []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Get("hide_partial_20260101_0000_abcd")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist for missing meta, got %v", err)
	}
}

func TestStore_Get_PartialWrite_MalformedMeta(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	id := "hide_bad_20260101_0000_abcd"
	entryDir := dir + "/" + id
	if err := os.MkdirAll(entryDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryDir+"/content", []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entryDir+"/meta.json", []byte("{invalid json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Get(id)
	if err == nil {
		t.Fatal("expected error for malformed meta")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("error should NOT be os.ErrNotExist for malformed meta, got %v", err)
	}
}

func TestStore_Cut_SinglePage(t *testing.T) {
	s := NewStore(t.TempDir())
	content := []byte("short")
	entry, err := s.Put("kind", "src", content, nil)
	if err != nil {
		t.Fatal(err)
	}
	cut, err := s.Cut(entry.ID, 1, 3800)
	if err != nil {
		t.Fatalf("Cut: %v", err)
	}
	if cut.TotalPages != 1 {
		t.Errorf("TotalPages: got %d, want 1", cut.TotalPages)
	}
	if !cut.IsFinal {
		t.Error("expected IsFinal=true")
	}
	if cut.Content != "short" {
		t.Errorf("content: got %q", cut.Content)
	}
}

func TestStore_Cut_MultiPage(t *testing.T) {
	s := NewStore(t.TempDir())
	pageSize := 10
	content := make([]byte, pageSize*3)
	for i := range content[:pageSize] {
		content[i] = 'a'
	}
	for i := range content[pageSize : pageSize*2] {
		content[pageSize+i] = 'b'
	}
	for i := range content[pageSize*2:] {
		content[pageSize*2+i] = 'c'
	}
	entry, err := s.Put("kind", "src", content, nil)
	if err != nil {
		t.Fatal(err)
	}
	cut1, err := s.Cut(entry.ID, 1, pageSize)
	if err != nil {
		t.Fatal(err)
	}
	if cut1.TotalPages != 3 {
		t.Errorf("TotalPages: got %d, want 3", cut1.TotalPages)
	}
	if cut1.IsFinal {
		t.Error("page 1 should not be final")
	}
	cut3, err := s.Cut(entry.ID, 3, pageSize)
	if err != nil {
		t.Fatal(err)
	}
	if !cut3.IsFinal {
		t.Error("page 3 should be final")
	}
}

func TestStore_Cut_OutOfRange(t *testing.T) {
	s := NewStore(t.TempDir())
	entry, _ := s.Put("kind", "src", []byte("hello"), nil)
	if _, err := s.Cut(entry.ID, 0, 100); err == nil {
		t.Error("expected error for page 0")
	}
	if _, err := s.Cut(entry.ID, 2, 100); err == nil {
		t.Error("expected error for page > TotalPages")
	}
}

func TestStore_List_Sorted(t *testing.T) {
	s := NewStore(t.TempDir())
	e1, _ := s.Put("k", "s", []byte("first"), nil)
	e2, _ := s.Put("k", "s", []byte("second"), nil)

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}
	// Both have same-second timestamps; either order is acceptable as long as both appear.
	ids := map[string]bool{list[0].ID: true, list[1].ID: true}
	if !ids[e1.ID] || !ids[e2.ID] {
		t.Errorf("List missing entries: got %v %v", list[0].ID, list[1].ID)
	}
}

func TestStore_List_SkipPartial(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	// Create a partial-write directory (no meta.json).
	partialDir := dir + "/hide_partial_20260101_0000_0000"
	if err := os.MkdirAll(partialDir, 0700); err != nil {
		t.Fatal(err)
	}

	s.Put("kind", "src", []byte("real"), nil) //nolint:errcheck
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range list {
		if e.ID == "hide_partial_20260101_0000_0000" {
			t.Error("partial-write entry should be skipped")
		}
	}
}

func TestStore_Delete(t *testing.T) {
	s := NewStore(t.TempDir())
	entry, _ := s.Put("k", "s", []byte("bye"), nil)
	if err := s.Delete(entry.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, _, err := s.Get(entry.ID)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected ErrNotExist after delete, got %v", err)
	}
}

func TestStore_LoadIntoBuffer(t *testing.T) {
	s := NewStore(t.TempDir())
	pageSize := 10
	content := make([]byte, pageSize*5)
	entry, _ := s.Put("k", "src", content, nil)

	buf, err := s.LoadIntoBuffer(entry.ID, pageSize)
	if err != nil {
		t.Fatalf("LoadIntoBuffer: %v", err)
	}
	if buf == nil {
		t.Fatal("LoadIntoBuffer returned nil buffer")
	}
	cut, err := buf.Cut(entry.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cut.TotalPages != 5 {
		t.Errorf("TotalPages: got %d, want 5", cut.TotalPages)
	}
}
