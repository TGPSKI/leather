package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDocumentedRespectsWordBoundaries(t *testing.T) {
	doc := "| `Drain` | wait for in-flight items |\n`(*Bus).Publish` fans out.\nSee NewRouterFactory too.\n"
	cases := map[string]bool{
		"Drain":       true,  // exact, backticked
		"Publish":     true,  // inside a method reference
		"NewRouter":   false, // only a prefix of NewRouterFactory
		"Rain":        false, // suffix of Drain
		"Bus":         true,
		"Unmentioned": false,
	}
	for name, want := range cases {
		if got := documented(doc, name); got != want {
			t.Errorf("documented(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDocPathForWalksUpToParentDoc(t *testing.T) {
	dir := t.TempDir()
	mods := filepath.Join(dir, "modules")
	if err := os.MkdirAll(mods, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"devtools.md", "queue.md"} {
		if err := os.WriteFile(filepath.Join(mods, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A nested package with no doc of its own resolves to the parent's doc:
	// docs/modules/devtools.md documents bus, causality, and sources.
	if got := docPathFor("internal/devtools/bus", mods); got != filepath.Join(mods, "devtools.md") {
		t.Errorf("nested package: got %q, want devtools.md", got)
	}
	if got := docPathFor("internal/queue", mods); got != filepath.Join(mods, "queue.md") {
		t.Errorf("flat package: got %q, want queue.md", got)
	}
	if got := docPathFor("internal/nosuch", mods); got != "" {
		t.Errorf("undocumented package: got %q, want empty", got)
	}
}

func TestLoadBaselineParsesMaxAndEntries(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "undocumented.txt")
	body := "# a comment\n#\n# max: 7\n\ninternal/queue.Drain\ninternal/tool.Wait\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := loadBaseline(p)
	if err != nil {
		t.Fatal(err)
	}
	if !b.hasMax || b.max != 7 {
		t.Errorf("max = %d (set=%v), want 7", b.max, b.hasMax)
	}
	if len(b.entries) != 2 {
		t.Fatalf("entries = %v, want 2", b.entries)
	}
	if b.entries["internal/queue.Drain"] != 5 {
		t.Errorf("line for queue.Drain = %d, want 5", b.entries["internal/queue.Drain"])
	}
}

func TestLoadBaselineMissingFileIsEmptyNotError(t *testing.T) {
	b, err := loadBaseline(filepath.Join(t.TempDir(), "absent.txt"))
	if err != nil {
		t.Fatalf("missing baseline should not error, got %v", err)
	}
	if len(b.entries) != 0 || b.hasMax {
		t.Errorf("missing baseline should be empty, got %+v", b)
	}
}

func TestScanExportsSkipsUnexportedAndTests(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", `package a

type Exported struct{}
type unexported struct{}

const KeptConst = 1
const droppedConst = 2

func Fn() {}
func fn() {}

// A method on an exported type is public API.
func (e *Exported) Method() {}

// A method on an unexported type is not reachable from outside.
func (u *unexported) AlsoHidden() {}
`)
	write("a_test.go", `package a

func TestThing() {}
type TestOnlyType struct{}
`)

	got, err := scanExports(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range got {
		names[e.Name] = true
	}
	for _, want := range []string{"Exported", "KeptConst", "Fn", "Method"} {
		if !names[want] {
			t.Errorf("missing exported symbol %q", want)
		}
	}
	for _, unwanted := range []string{"unexported", "droppedConst", "fn", "AlsoHidden", "TestThing", "TestOnlyType"} {
		if names[unwanted] {
			t.Errorf("should not have collected %q", unwanted)
		}
	}
}

func TestExportKeyFormat(t *testing.T) {
	e := export{Pkg: "internal/queue", Name: "Drain"}
	if e.Key() != "internal/queue.Drain" {
		t.Errorf("Key() = %q, want internal/queue.Drain", e.Key())
	}
}
