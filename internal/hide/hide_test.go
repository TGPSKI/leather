package hide

import (
	"strings"
	"testing"
)

func TestHideBuffer_StoreAndGet(t *testing.T) {
	b := NewHideBuffer(3800)
	h := b.Store("gh_pr_thread", "some content")
	if h == nil {
		t.Fatal("Store returned nil")
	}
	if !strings.HasPrefix(h.ID, "hide_") {
		t.Errorf("ID should start with 'hide_', got %q", h.ID)
	}
	got, ok := b.Get(h.ID)
	if !ok {
		t.Fatal("Get returned false for stored hide")
	}
	if got.ID != h.ID || got.Content != "some content" {
		t.Errorf("Get returned wrong hide: %+v", got)
	}
}

func TestHideBuffer_Get_Unknown(t *testing.T) {
	b := NewHideBuffer(3800)
	_, ok := b.Get("does-not-exist")
	if ok {
		t.Error("Get should return false for unknown id")
	}
}

func TestHideBuffer_ResolveID(t *testing.T) {
	b := NewHideBuffer(100)
	h := b.Store("tool", "content")

	got, ok := b.ResolveID(h.ID)
	if !ok || got != h.ID {
		t.Fatalf("ResolveID(existing) = %q, %v; want %q, true", got, ok, h.ID)
	}

	got, ok = b.ResolveID("hallucinated")
	if !ok || got != h.ID {
		t.Fatalf("ResolveID(single active fallback) = %q, %v; want %q, true", got, ok, h.ID)
	}

	b.Store("tool", "other")
	if got, ok := b.ResolveID("hallucinated"); ok {
		t.Fatalf("ResolveID(ambiguous) = %q, true; want false", got)
	}
}

func TestHideBuffer_Cut_SinglePage(t *testing.T) {
	b := NewHideBuffer(100)
	content := strings.Repeat("x", 50) // 50 bytes < 100 page size
	h := b.Store("tool", content)

	cut, err := b.Cut(h.ID, 1)
	if err != nil {
		t.Fatalf("Cut error: %v", err)
	}
	if cut.TotalPages != 1 {
		t.Errorf("expected TotalPages=1, got %d", cut.TotalPages)
	}
	if !cut.IsFinal {
		t.Error("expected IsFinal=true for single-page hide")
	}
	if cut.Content != content {
		t.Errorf("expected full content in cut, got %q", cut.Content)
	}
	if cut.SizeBytes != len(content) {
		t.Errorf("expected SizeBytes=%d, got %d", len(content), cut.SizeBytes)
	}
}

func TestHideBuffer_Cut_MultiPage(t *testing.T) {
	pageSize := 100
	b := NewHideBuffer(pageSize)
	// 3 full pages
	content := strings.Repeat("a", pageSize) + strings.Repeat("b", pageSize) + strings.Repeat("c", pageSize)
	h := b.Store("tool", content)

	cut1, err := b.Cut(h.ID, 1)
	if err != nil {
		t.Fatalf("Cut(1) error: %v", err)
	}
	if cut1.TotalPages != 3 {
		t.Errorf("expected TotalPages=3, got %d", cut1.TotalPages)
	}
	if cut1.IsFinal {
		t.Error("page 1 should not be final")
	}
	if cut1.Content != strings.Repeat("a", pageSize) {
		t.Error("page 1 content mismatch")
	}

	cut3, err := b.Cut(h.ID, 3)
	if err != nil {
		t.Fatalf("Cut(3) error: %v", err)
	}
	if !cut3.IsFinal {
		t.Error("page 3 should be final")
	}
	if cut3.Content != strings.Repeat("c", pageSize) {
		t.Error("page 3 content mismatch")
	}
}

func TestHideBuffer_Cut_OutOfRange(t *testing.T) {
	b := NewHideBuffer(100)
	h := b.Store("tool", "hello")

	if _, err := b.Cut(h.ID, 0); err == nil {
		t.Error("expected error for page 0")
	}
	if _, err := b.Cut(h.ID, 2); err == nil {
		t.Error("expected error for page > TotalPages")
	}
}

func TestHideBuffer_Cut_UnknownID(t *testing.T) {
	b := NewHideBuffer(100)
	if _, err := b.Cut("bad-id", 1); err == nil {
		t.Error("expected error for unknown id")
	}
}

func TestHideBuffer_Search_Found(t *testing.T) {
	pageSize := 100
	b := NewHideBuffer(pageSize)
	// "unique-marker" lives in page 2
	content := strings.Repeat("a", pageSize) + "page two has unique-marker here" + strings.Repeat("b", pageSize-31)
	h := b.Store("tool", content)

	cut, found, err := b.Search(h.ID, "unique-marker")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	if cut.PageNumber != 2 {
		t.Errorf("expected page 2, got page %d", cut.PageNumber)
	}
	if !strings.Contains(cut.Content, "unique-marker") {
		t.Error("returned cut does not contain the query")
	}
}

func TestHideBuffer_Search_NotFound(t *testing.T) {
	b := NewHideBuffer(100)
	h := b.Store("tool", "hello world")

	cut, found, err := b.Search(h.ID, "no-such-thing")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if found {
		t.Error("expected found=false")
	}
	// Fallback must be page 1.
	if cut.PageNumber != 1 {
		t.Errorf("expected fallback page 1, got %d", cut.PageNumber)
	}
}

func TestHideBuffer_Search_UnknownID(t *testing.T) {
	b := NewHideBuffer(100)
	if _, _, err := b.Search("bad-id", "query"); err == nil {
		t.Error("expected error for unknown id")
	}
}

func TestHideBuffer_Format_NotFinal(t *testing.T) {
	b := NewHideBuffer(10)
	// 3 pages so page 1 is not final
	content := strings.Repeat("x", 30)
	h := b.Store("src", content)

	cut, err := b.Cut(h.ID, 1)
	if err != nil {
		t.Fatalf("Cut error: %v", err)
	}
	formatted := cut.Format()
	if !strings.Contains(formatted, "[HIDE") {
		t.Error("Format missing [HIDE header")
	}
	if !strings.Contains(formatted, "page 1/") {
		t.Error("Format missing page 1/ indicator")
	}
	if !strings.Contains(formatted, "[END") {
		t.Error("Format missing [END footer")
	}
	if !strings.Contains(formatted, "hide_next") && !strings.Contains(formatted, "hide_jump") {
		t.Error("Format missing navigation hint for non-final page")
	}
}

func TestHideBuffer_Format_Final(t *testing.T) {
	b := NewHideBuffer(100)
	h := b.Store("src", "short content")

	cut, err := b.Cut(h.ID, 1)
	if err != nil {
		t.Fatalf("Cut error: %v", err)
	}
	if !cut.IsFinal {
		t.Fatal("expected IsFinal=true")
	}
	formatted := cut.Format()
	if !strings.Contains(formatted, "complete content") {
		t.Error("Format should mention 'complete content' for final page")
	}
	if strings.Contains(formatted, "hide_next") || strings.Contains(formatted, "hide_jump") {
		t.Error("Final page should not mention navigation tools")
	}
}

func TestHideBuffer_Format_FinalWithReflectionHint(t *testing.T) {
	b := NewHideBuffer(100)
	b.ReflectionHint = "List 3-5 key facts from this page only. Do not produce final output yet."
	h := b.Store("src", "short content")

	cut, err := b.Cut(h.ID, 1)
	if err != nil {
		t.Fatalf("Cut error: %v", err)
	}
	if !cut.IsFinal {
		t.Fatal("expected IsFinal=true")
	}
	formatted := cut.Format()
	if !strings.Contains(formatted, "This is the final page. No further pages remain.") {
		t.Fatalf("final reflection page should mention final page: %q", formatted)
	}
	if !strings.Contains(formatted, "Do not produce final output yet") {
		t.Fatalf("final reflection page missing summary-only instruction: %q", formatted)
	}
	if strings.Contains(formatted, "This is the complete content") {
		t.Fatalf("final reflection page should not use default final-page hint: %q", formatted)
	}
}

func TestToolDefs(t *testing.T) {
	defs := ToolDefs()
	if len(defs) != 3 {
		t.Fatalf("expected 3 tool defs, got %d", len(defs))
	}
	wantNames := map[string]bool{"hide_next": true, "hide_jump": true, "hide_search": true}
	for _, d := range defs {
		if d.Type != "hide" {
			t.Errorf("tool %q: expected Type=hide, got %q", d.Name, d.Type)
		}
		if !wantNames[d.Name] {
			t.Errorf("unexpected tool name %q", d.Name)
		}
		delete(wantNames, d.Name)
	}
	if len(wantNames) != 0 {
		t.Errorf("missing tool defs: %v", wantNames)
	}
}

func TestHideBuffer_EmptyContent(t *testing.T) {
	b := NewHideBuffer(100)
	h := b.Store("tool", "")

	cut, err := b.Cut(h.ID, 1)
	if err != nil {
		t.Fatalf("Cut error: %v", err)
	}
	if cut.TotalPages != 1 {
		t.Errorf("expected TotalPages=1 for empty content, got %d", cut.TotalPages)
	}
	if !cut.IsFinal {
		t.Error("expected IsFinal=true for empty content")
	}
}

func TestHideBuffer_LargeItem(t *testing.T) {
	pageSize := 3800
	b := NewHideBuffer(pageSize)
	// 200 KB — well within the 4 MB ceiling
	content := strings.Repeat("z", 200*1024)
	h := b.Store("big_tool", content)

	expectedPages := totalPages(len(content), pageSize)
	cut1, err := b.Cut(h.ID, 1)
	if err != nil {
		t.Fatalf("Cut(1) error: %v", err)
	}
	if cut1.TotalPages != expectedPages {
		t.Errorf("expected TotalPages=%d, got %d", expectedPages, cut1.TotalPages)
	}

	cutLast, err := b.Cut(h.ID, expectedPages)
	if err != nil {
		t.Fatalf("Cut(%d) error: %v", expectedPages, err)
	}
	if !cutLast.IsFinal {
		t.Error("last page should be final")
	}
}

func TestHideBuffer_Search_CaseInsensitive(t *testing.T) {
	b := NewHideBuffer(3800)
	h := b.Store("tool", "Hello World")

	_, found, err := b.Search(h.ID, "HELLO")
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if !found {
		t.Error("expected case-insensitive match for HELLO")
	}
}

func TestGenerateHideID(t *testing.T) {
	id := generateHideID("gh_pr_thread")
	if !strings.HasPrefix(id, "hide_gh_pr_thread_") {
		t.Errorf("unexpected ID format: %q", id)
	}
}

func TestSanitizeSource(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"gh_pr_thread", "gh_pr_thread"},
		{"My Tool!", "my_tool_"},
		{"tool-name.v2", "tool_name_v2"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := sanitizeSource(tc.in); got != tc.want {
			t.Errorf("sanitizeSource(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
