package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- unit tests ---

func TestIsLinkedIn(t *testing.T) {
	tests := []struct {
		name    string
		link    string
		content string
		want    bool
	}{
		{
			name:    "present",
			link:    ".subagents/AGENTS-CORE.md",
			content: "| Core | [AGENTS-CORE.md](.subagents/AGENTS-CORE.md) | pkg |",
			want:    true,
		},
		{
			name:    "absent",
			link:    ".subagents/AGENTS-FOO.md",
			content: "# AGENTS.md\n## Routing\n",
			want:    false,
		},
		{
			name:    "no false positive on prefix overlap",
			link:    ".subagents/AGENTS-CORE.md",
			content: ".subagents/AGENTS-CORE-EXTRA.md",
			want:    false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isLinkedIn(tc.link, []byte(tc.content))
			if got != tc.want {
				t.Errorf("isLinkedIn(%q) = %v, want %v", tc.link, got, tc.want)
			}
		})
	}
}

func TestReadSubagentFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"AGENTS-CORE.md", "AGENTS-SERVE.md", "README.md", "notes.txt", "AGENTS-notes-backup"} {
		mustWrite(t, filepath.Join(dir, name), "# "+name)
	}

	got, err := readSubagentFiles(dir)
	if err != nil {
		t.Fatalf("readSubagentFiles: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d files, want 2: %v", len(got), got)
	}
}

func TestCountFileLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"three lines", "line1\nline2\nline3\n", 3},
		{"single no newline", "single", 1},
		{"two no trailing", "a\nb", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.md")
			mustWrite(t, path, tc.content)
			got, err := countFileLines(path)
			if err != nil {
				t.Fatalf("countFileLines: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGuideLink(t *testing.T) {
	tests := []struct {
		name         string
		rootFile     string
		subagentsDir string
		guide        string
		want         string
	}{
		{
			name:         "relative paths",
			rootFile:     "AGENTS.md",
			subagentsDir: ".subagents",
			guide:        "AGENTS-CORE.md",
			want:         ".subagents/AGENTS-CORE.md",
		},
		{
			name:         "absolute temp paths",
			rootFile:     "/tmp/proj/AGENTS.md",
			subagentsDir: "/tmp/proj/.subagents",
			guide:        "AGENTS-SERVE.md",
			want:         ".subagents/AGENTS-SERVE.md",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := guideLink(tc.rootFile, tc.subagentsDir, tc.guide)
			if got != tc.want {
				t.Errorf("guideLink = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFindRoutingTableInsert(t *testing.T) {
	content := strings.Join([]string{
		"# AGENTS.md",
		"",
		"### Subagent routing table",
		"",
		"| You're working on… | Load this guide | Owns |",
		"|---|---|---|",
		"| Core | [AGENTS-CORE.md](.subagents/AGENTS-CORE.md) | internal/model |",
		"| Serve | [AGENTS-SERVE.md](.subagents/AGENTS-SERVE.md) | internal/cli |",
		"",
		"If a task spans two domains...",
	}, "\n")

	lines := strings.Split(content, "\n")
	idx := findRoutingTableInsert(lines)
	if idx < 0 {
		t.Fatal("expected to find routing table, got -1")
	}
	// The line before the insert index must be a table row.
	prev := strings.TrimSpace(lines[idx-1])
	if !strings.HasPrefix(prev, "|") {
		t.Errorf("line before insert index %d is not a table row: %q", idx, lines[idx-1])
	}
	// The line at the insert index must not be a table row.
	if idx < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[idx]), "|") {
		t.Errorf("insert index %d points to a table row: %q", idx, lines[idx])
	}
}

func TestFindRoutingTableInsert_NoTable(t *testing.T) {
	lines := []string{"# AGENTS.md", "", "No table here."}
	if got := findRoutingTableInsert(lines); got != -1 {
		t.Errorf("expected -1, got %d", got)
	}
}

// --- integration-style tests using temp directories ---

func TestRunSync_AllRegistered(t *testing.T) {
	dir, subDir, rootFile := setupDir(t)
	mustWrite(t, filepath.Join(subDir, "AGENTS-CORE.md"), "# Core\n")
	mustWrite(t, rootFile, "| Core | [AGENTS-CORE.md](.subagents/AGENTS-CORE.md) | pkg |\n")

	cfg := config{rootFile: rootFile, subagentsDir: subDir}
	var out bytes.Buffer
	issues, err := runSync(cfg, &out)
	if err != nil {
		t.Fatalf("runSync: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues, got %v", issues)
	}
	_ = dir
}

func TestRunSync_UnregisteredGuide(t *testing.T) {
	_, subDir, rootFile := setupDir(t)
	mustWrite(t, filepath.Join(subDir, "AGENTS-NEW.md"), "# New\n")
	mustWrite(t, rootFile, "# AGENTS.md\n")

	cfg := config{rootFile: rootFile, subagentsDir: subDir}
	var out bytes.Buffer
	issues, err := runSync(cfg, &out)
	if err != nil {
		t.Fatalf("runSync: %v", err)
	}
	if len(issues) != 1 || issues[0].kind != "unregistered" {
		t.Errorf("expected 1 unregistered issue, got %v", issues)
	}
}

func TestRunSync_FixAppendsStubRow(t *testing.T) {
	_, subDir, rootFile := setupDir(t)
	mustWrite(t, filepath.Join(subDir, "AGENTS-FOO.md"), "# Foo\n")
	rootContent := strings.Join([]string{
		"# AGENTS.md",
		"",
		"| You're working on… | Load | Owns |",
		"|---|---|---|",
		"| Existing | [x](x) | y |",
		"",
		"Next section.",
		"",
	}, "\n")
	mustWrite(t, rootFile, rootContent)

	cfg := config{rootFile: rootFile, subagentsDir: subDir, fix: true}
	var out bytes.Buffer
	issues, err := runSync(cfg, &out)
	if err != nil {
		t.Fatalf("runSync --fix: %v", err)
	}

	updated, _ := os.ReadFile(rootFile)
	if !bytes.Contains(updated, []byte("AGENTS-FOO.md")) {
		t.Errorf("expected stub row in AGENTS.md after fix:\n%s", updated)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 remaining issues after fix, got %v", issues)
	}
}

func TestRunAudit_UnionCandidate(t *testing.T) {
	_, subDir, rootFile := setupDir(t)
	// 5 lines — well below union threshold of 150.
	mustWrite(t, filepath.Join(subDir, "AGENTS-THIN.md"), strings.Repeat("line\n", 5))
	mustWrite(t, rootFile, "| work | [AGENTS-THIN.md](.subagents/AGENTS-THIN.md) | pkg |\n")

	cfg := config{
		rootFile:        rootFile,
		subagentsDir:    subDir,
		divideThreshold: 400,
		unionThreshold:  150,
	}
	var out bytes.Buffer
	issues, err := runAudit(cfg, &out)
	if err != nil {
		t.Fatalf("runAudit: %v", err)
	}
	if len(issues) != 1 || issues[0].kind != "union" {
		t.Errorf("expected 1 union issue, got %v", issues)
	}
}

func TestRunAudit_DivideCandidate(t *testing.T) {
	_, subDir, rootFile := setupDir(t)
	// 500 lines — above divide threshold of 400.
	mustWrite(t, filepath.Join(subDir, "AGENTS-FAT.md"), strings.Repeat("line\n", 500))
	mustWrite(t, rootFile, "| work | [AGENTS-FAT.md](.subagents/AGENTS-FAT.md) | pkg |\n")

	cfg := config{
		rootFile:        rootFile,
		subagentsDir:    subDir,
		divideThreshold: 400,
		unionThreshold:  150,
	}
	var out bytes.Buffer
	issues, err := runAudit(cfg, &out)
	if err != nil {
		t.Fatalf("runAudit: %v", err)
	}
	if len(issues) != 1 || issues[0].kind != "divide" {
		t.Errorf("expected 1 divide issue, got %v", issues)
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"bogus"}, &out, &errOut)
	if code != 2 {
		t.Errorf("expected exit 2, got %d", code)
	}
}

// --- helpers ---

// setupDir creates a temp project directory with a .subagents subdirectory
// and returns (projectDir, subagentsDir, rootFilePath).
func setupDir(t *testing.T) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	subDir := filepath.Join(dir, ".subagents")
	if err := os.Mkdir(subDir, 0700); err != nil {
		t.Fatal(err)
	}
	rootFile := filepath.Join(dir, "AGENTS.md")
	return dir, subDir, rootFile
}

// mustWrite creates or overwrites path with content at permission 0600.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
