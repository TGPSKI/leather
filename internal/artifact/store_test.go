package artifact

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

func makeArtifact(curingName, content string) model.Artifact {
	return model.Artifact{
		ID:         GenerateArtifactID(),
		HideID:     "hide_test_20260101_0000_0001",
		HideKind:   "github.pr_review_thread",
		CuringName: curingName,
		AgentName:  "pr-review",
		Content:    content,
		CreatedAt:  time.Now().Unix(),
	}
}

func TestStore_WriteAndGet(t *testing.T) {
	s := NewStore(t.TempDir())
	a := makeArtifact("pr-review", "artifact content")

	if err := s.Write(a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := s.Get(a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Content != a.Content {
		t.Errorf("content mismatch: got %q, want %q", got.Content, a.Content)
	}
	if got.CuringName != a.CuringName {
		t.Errorf("CuringName: got %q", got.CuringName)
	}
}

func TestStore_Get_NotExist(t *testing.T) {
	s := NewStore(t.TempDir())
	_, err := s.Get("artifact_20260101_0000_0000")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestStore_List_Empty(t *testing.T) {
	s := NewStore(t.TempDir())
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list, got %d", len(list))
	}
}

func TestStore_List_NotExistDir(t *testing.T) {
	s := NewStore("/tmp/does_not_exist_leather_test_" + GenerateArtifactID())
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if list != nil {
		t.Errorf("expected nil list, got %v", list)
	}
}

func TestStore_List_SortedNewestFirst(t *testing.T) {
	s := NewStore(t.TempDir())
	a1 := makeArtifact("pr-review", "old")
	a1.CreatedAt = 100
	a2 := makeArtifact("pr-review", "new")
	a2.CreatedAt = 200

	s.Write(a1) //nolint:errcheck
	s.Write(a2) //nolint:errcheck

	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(list))
	}
	if list[0].CreatedAt < list[1].CreatedAt {
		t.Errorf("expected newest first: got %d then %d", list[0].CreatedAt, list[1].CreatedAt)
	}
}

func TestStore_ListByCuring(t *testing.T) {
	s := NewStore(t.TempDir())
	a1 := makeArtifact("pr-review", "first")
	a2 := makeArtifact("pr-review", "second")
	a3 := makeArtifact("repo-triage", "triage")

	s.Write(a1) //nolint:errcheck
	s.Write(a2) //nolint:errcheck
	s.Write(a3) //nolint:errcheck

	prList, err := s.ListByCuring("pr-review")
	if err != nil {
		t.Fatal(err)
	}
	if len(prList) != 2 {
		t.Errorf("expected 2 pr-review artifacts, got %d", len(prList))
	}

	triageList, err := s.ListByCuring("repo-triage")
	if err != nil {
		t.Fatal(err)
	}
	if len(triageList) != 1 {
		t.Errorf("expected 1 repo-triage artifact, got %d", len(triageList))
	}
}

func TestStore_ListByCuring_NotExist(t *testing.T) {
	s := NewStore(t.TempDir())
	list, err := s.ListByCuring("nonexistent")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if list != nil {
		t.Errorf("expected nil list, got %v", list)
	}
}

func TestStore_Write_CreatesSubdir(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	a := makeArtifact("new-curing", "content")
	if err := s.Write(a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	info, err := os.Stat(dir + "/new-curing")
	if err != nil {
		t.Fatalf("curing subdir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}
