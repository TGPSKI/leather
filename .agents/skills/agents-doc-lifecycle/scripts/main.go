// Package main implements the agents-doc-lifecycle skill tool for leather.
// It audits and optionally repairs AGENTS.md and .subagents/AGENTS-*.md
// to keep them synchronized with the codebase.
//
// Usage:
//
//	bash .agents/skills/agents-doc-lifecycle/scripts/run.sh sync [--fix]
//	bash .agents/skills/agents-doc-lifecycle/scripts/run.sh audit
//	bash .agents/skills/agents-doc-lifecycle/scripts/run.sh check
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// findRepoRoot walks upward from dir until it finds a directory containing
// AGENTS.md, which marks the leather repository root. It returns an error if
// the filesystem root is reached without finding the marker file.
func findRepoRoot(dir string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("findRepoRoot: AGENTS.md not found searching upward from %s", dir)
		}
		dir = parent
	}
}

// config holds all resolved options for a single tool invocation.
type config struct {
	rootFile        string
	subagentsDir    string
	divideThreshold int
	unionThreshold  int
	fix             bool
}

// issue represents a single audit finding.
type issue struct {
	kind    string // "unregistered", "divide", "union"
	file    string
	message string
}

func (i issue) String() string {
	return fmt.Sprintf("[%s] %s: %s", i.kind, i.file, i.message)
}

// run is the testable entry point. It returns an exit code suitable for
// passing to os.Exit in main. It auto-discovers the repository root by
// searching upward from the current working directory for AGENTS.md, and
// chdirs there before executing any file operations.
func run(args []string, stdout, stderr io.Writer) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "agents-doc-lifecycle: getwd: %v\n", err)
		return 1
	}
	root, err := findRepoRoot(cwd)
	if err != nil {
		fmt.Fprintf(stderr, "agents-doc-lifecycle: %v\n", err)
		return 1
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintf(stderr, "agents-doc-lifecycle: chdir %s: %v\n", root, err)
		return 1
	}

	fs := flag.NewFlagSet("agents-doc-lifecycle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFile := fs.String("root", "AGENTS.md", "path to root AGENTS.md")
	subDir := fs.String("subagents-dir", ".subagents", "path to subagents directory")
	divideThreshold := fs.Int("divide-threshold", 400, "line count that triggers a divide warning")
	unionThreshold := fs.Int("union-threshold", 150, "line count below which a guide is a merge candidate")
	fix := fs.Bool("fix", false, "patch AGENTS.md with stub rows for unregistered guides (sync only)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg := config{
		rootFile:        *rootFile,
		subagentsDir:    *subDir,
		divideThreshold: *divideThreshold,
		unionThreshold:  *unionThreshold,
		fix:             *fix,
	}

	action := fs.Arg(0)
	if action == "" {
		action = "audit"
	}

	switch action {
	case "sync":
		issues, err := runSync(cfg, stdout)
		if err != nil {
			fmt.Fprintf(stderr, "agents-doc-lifecycle/sync: %v\n", err)
			return 1
		}
		if len(issues) > 0 {
			return 1
		}
		return 0

	case "audit", "check":
		issues, err := runAudit(cfg, stdout)
		if err != nil {
			fmt.Fprintf(stderr, "agents-doc-lifecycle/audit: %v\n", err)
			return 1
		}
		if len(issues) > 0 {
			return 1
		}
		return 0

	default:
		fmt.Fprintf(stderr, "agents-doc-lifecycle: unknown command %q (valid: sync, audit, check)\n", action)
		return 2
	}
}

// runSync checks that every .subagents/AGENTS-*.md file is linked in AGENTS.md.
// If cfg.fix is true it appends stub routing rows for any missing guides.
// Returns the list of issues still outstanding after any fixes.
func runSync(cfg config, out io.Writer) ([]issue, error) {
	guides, err := readSubagentFiles(cfg.subagentsDir)
	if err != nil {
		return nil, fmt.Errorf("runSync/readSubagentFiles: %w", err)
	}

	rootContent, err := os.ReadFile(cfg.rootFile)
	if err != nil {
		return nil, fmt.Errorf("runSync/readRoot: %w", err)
	}

	var found []issue
	var toFix []string

	for _, guide := range guides {
		link := guideLink(cfg.rootFile, cfg.subagentsDir, guide)
		if !isLinkedIn(link, rootContent) {
			iss := issue{
				kind:    "unregistered",
				file:    link,
				message: "not referenced in AGENTS.md routing table",
			}
			found = append(found, iss)
			fmt.Fprintln(out, iss)
			toFix = append(toFix, guide)
		}
	}

	if cfg.fix && len(toFix) > 0 {
		for _, guide := range toFix {
			link := guideLink(cfg.rootFile, cfg.subagentsDir, guide)
			if err := appendStubRow(cfg.rootFile, link, guide); err != nil {
				return found, fmt.Errorf("runSync/appendStubRow %s: %w", guide, err)
			}
			fmt.Fprintf(out, "  → appended stub row for %s\n", link)
		}
		// Re-read and re-check to confirm fixes took effect.
		rootContent, err = os.ReadFile(cfg.rootFile)
		if err != nil {
			return found, fmt.Errorf("runSync/rereadRoot: %w", err)
		}
		var remaining []issue
		for _, iss := range found {
			if !isLinkedIn(iss.file, rootContent) {
				remaining = append(remaining, iss)
			}
		}
		return remaining, nil
	}

	if len(found) == 0 {
		fmt.Fprintf(out, "sync: all %d guide(s) registered in %s\n", len(guides), cfg.rootFile)
	}
	return found, nil
}

// runAudit runs sync (without fix) then checks each guide's line count
// against the divide and union thresholds.
func runAudit(cfg config, out io.Writer) ([]issue, error) {
	syncIssues, err := runSync(cfg, out)
	if err != nil {
		return nil, err
	}

	guides, err := readSubagentFiles(cfg.subagentsDir)
	if err != nil {
		return nil, fmt.Errorf("runAudit/readSubagentFiles: %w", err)
	}

	var sizeIssues []issue
	for _, guide := range guides {
		path := filepath.Join(cfg.subagentsDir, guide)
		lines, err := countFileLines(path)
		if err != nil {
			return nil, fmt.Errorf("runAudit/countLines %s: %w", guide, err)
		}
		link := guideLink(cfg.rootFile, cfg.subagentsDir, guide)
		switch {
		case lines > cfg.divideThreshold:
			iss := issue{
				kind:    "divide",
				file:    link,
				message: fmt.Sprintf("%d lines exceeds divide threshold (%d) — consider splitting", lines, cfg.divideThreshold),
			}
			sizeIssues = append(sizeIssues, iss)
			fmt.Fprintln(out, iss)
		case lines < cfg.unionThreshold:
			iss := issue{
				kind:    "union",
				file:    link,
				message: fmt.Sprintf("%d lines below union threshold (%d) — consider merging with a related guide", lines, cfg.unionThreshold),
			}
			sizeIssues = append(sizeIssues, iss)
			fmt.Fprintln(out, iss)
		default:
			fmt.Fprintf(out, "audit: %s — %d lines (healthy)\n", link, lines)
		}
	}

	all := append(syncIssues, sizeIssues...)
	if len(all) == 0 {
		fmt.Fprintf(out, "audit: %s and %d guide(s) are healthy\n", cfg.rootFile, len(guides))
	}
	return all, nil
}

// readSubagentFiles returns the base filenames of AGENTS-*.md files in dir.
func readSubagentFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("readSubagentFiles: %w", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, "AGENTS-") && strings.HasSuffix(name, ".md") {
			files = append(files, name)
		}
	}
	return files, nil
}

// guideLink returns the slash-separated path from rootFile's directory to the
// given guide file inside subagentsDir. This is the string that should appear
// as a link in AGENTS.md regardless of whether the paths are absolute or
// relative.
func guideLink(rootFile, subagentsDir, guide string) string {
	rootDir := filepath.Dir(rootFile)
	abs := filepath.Join(subagentsDir, guide)
	// If subagentsDir is already absolute, Rel computes correctly.
	// If it is relative, Join keeps it relative and Rel still works.
	rel, err := filepath.Rel(rootDir, abs)
	if err != nil {
		// Fallback: use the joined path as-is.
		rel = filepath.Join(subagentsDir, guide)
	}
	return filepath.ToSlash(rel)
}

// isLinkedIn reports whether link appears as a substring of content, indicating
// that AGENTS.md references the given guide path.
func isLinkedIn(link string, content []byte) bool {
	return bytes.Contains(content, []byte(link))
}

// countFileLines returns the number of newline-terminated lines in path.
func countFileLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("countFileLines/open: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
	}
	if err := sc.Err(); err != nil {
		return 0, fmt.Errorf("countFileLines/scan: %w", err)
	}
	return n, nil
}

// appendStubRow inserts a placeholder routing table row into rootPath for the
// given guide. It locates the routing table by finding the "You're working on"
// header row and inserts after the last contiguous table row.
func appendStubRow(rootPath, link, guide string) error {
	content, err := os.ReadFile(rootPath)
	if err != nil {
		return fmt.Errorf("appendStubRow/read: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	insertIdx := findRoutingTableInsert(lines)
	if insertIdx < 0 {
		// Routing table not found; append before the final newline.
		insertIdx = len(lines)
	}

	// Derive a human-readable domain name from the guide filename.
	name := strings.TrimSuffix(strings.TrimPrefix(guide, "AGENTS-"), ".md")
	stub := fmt.Sprintf(
		"| <!-- TODO: describe %s domain --> | [%s](%s) | <!-- TODO: list packages --> |",
		name, guide, link,
	)

	updated := make([]string, 0, len(lines)+1)
	updated = append(updated, lines[:insertIdx]...)
	updated = append(updated, stub)
	updated = append(updated, lines[insertIdx:]...)

	if err := os.WriteFile(rootPath, []byte(strings.Join(updated, "\n")), 0600); err != nil {
		return fmt.Errorf("appendStubRow/write: %w", err)
	}
	return nil
}

// findRoutingTableInsert returns the line index immediately after the last
// contiguous table row in the "You're working on" routing block. Returns -1
// if the table is not found.
func findRoutingTableInsert(lines []string) int {
	tableStart := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") && strings.Contains(line, "working on") {
			tableStart = i
			break
		}
	}
	if tableStart < 0 {
		return -1
	}

	// Walk forward to find the last contiguous | line.
	last := tableStart
	for i := tableStart + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			last = i
		} else {
			break
		}
	}
	return last + 1
}
