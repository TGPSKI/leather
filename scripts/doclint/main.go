// Command doclint is a deterministic documentation-consistency gate.
//
// It extracts every documented identifier that must have a referent in the code
// — CLI flags, environment variables, HTTP endpoints, and shell-tools JSON —
// and asserts each one resolves against ground truth scanned from the repo
// (flag registrations, env reads, registered routes, and the shell-mcp tool
// schema). Any documented token with no code referent is a violation and the
// gate exits non-zero.
//
// It is intentionally precise over exhaustive: it only flags tokens in
// unambiguous contexts (inline-code spans, HTTP-verb rows, fenced json blocks)
// so it has near-zero false positives and can gate CI. Semantic claims it
// cannot mechanically verify (e.g. a response content-type) are out of scope;
// schema/defs.go parity is a separate check (see the audit's schema issue).
//
// Usage:
//
//	go run ./scripts/doclint [-docs docs,.subagents,README.md,AGENTS.md] [-src .] [-allow scripts/doclint/allow.txt] [-json]
//
// Zero third-party dependencies.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type violation struct {
	File  string `json:"file"`
	Line  int    `json:"line"`
	Check string `json:"check"`
	Msg   string `json:"msg"`
}

// ---- ground truth scanned from *.go (non-test) ----

var (
	reFlag   = regexp.MustCompile(`fs\.(?:String|Bool|Int|Int64|Uint|Uint64|Duration|Float64|Var)\(\s*"([a-z0-9][a-z0-9-]*)"`)
	reEnvFn  = regexp.MustCompile(`\benv[A-Za-z0-9]*\(\s*"([A-Z0-9_]+)"`)
	reGetenv = regexp.MustCompile(`os\.Getenv\(\s*"([A-Z0-9_]+)"\)`)
	reRoute  = regexp.MustCompile(`\.Handle(?:Func)?\(\s*"(/[^"]*)"`)
)

type truth struct {
	flags    map[string]bool
	envs     map[string]bool
	exact    map[string]bool // registered routes, exact patterns
	prefixes []string        // registered routes ending in "/"
}

func scanTruth(srcRoots []string) (truth, error) {
	t := truth{flags: map[string]bool{}, envs: map[string]bool{}, exact: map[string]bool{}}
	for _, root := range srcRoots {
		err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if fi.IsDir() {
				base := fi.Name()
				if base == "vendor" || base == "node_modules" || base == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			s := string(b)
			for _, m := range reFlag.FindAllStringSubmatch(s, -1) {
				t.flags[m[1]] = true
			}
			for _, m := range reEnvFn.FindAllStringSubmatch(s, -1) {
				t.envs["LEATHER_"+m[1]] = true
			}
			for _, m := range reGetenv.FindAllStringSubmatch(s, -1) {
				t.envs[m[1]] = true
			}
			for _, m := range reRoute.FindAllStringSubmatch(s, -1) {
				r := m[1]
				t.exact[r] = true
				if strings.HasSuffix(r, "/") {
					t.prefixes = append(t.prefixes, r)
				}
			}
			return nil
		})
		if err != nil {
			return t, err
		}
	}
	return t, nil
}

func (t truth) routeCovered(p string) bool {
	if t.exact[p] {
		return true
	}
	for _, pre := range t.prefixes {
		if strings.HasPrefix(p, pre) {
			return true
		}
	}
	return false
}

// ---- doc token extraction ----

var (
	reEnvTok   = regexp.MustCompile(`(?:LEATHER|SHELL_MCP)_[A-Z0-9_]+`)
	reFlagTok  = regexp.MustCompile("`(--[a-z0-9][a-z0-9-]*)`")
	reHTTPVerb = regexp.MustCompile(`\b(?:GET|POST|PUT|DELETE|PATCH|HEAD)\b`)
	rePathTok  = regexp.MustCompile("(?:^|[^A-Za-z0-9.])(/[A-Za-z0-9_{][A-Za-z0-9_/{}.:*-]*)")
	reCSSVar   = regexp.MustCompile(`var\(\s*(--[a-z0-9-]+)`)
	reCSSDef   = regexp.MustCompile(`(?m)(--[a-z0-9-]+)\s*:\s*[^;\n|]`)
	reIgnore   = regexp.MustCompile(`doclint:allow\s+(\S+)`)
	rePlanned  = regexp.MustCompile(`(?i)\b(planned|future|not yet|when .* lands|roadmap|todo|n/a)\b`)
)

// env tokens that are legitimately external / not leather-owned.
func extractEnv(line string) []string {
	return reEnvTok.FindAllString(line, -1)
}

func extractFlags(line string) []string {
	var out []string
	for _, m := range reFlagTok.FindAllStringSubmatch(line, -1) {
		out = append(out, m[1])
	}
	return out
}

func extractEndpoints(line string) []string {
	if !reHTTPVerb.MatchString(line) {
		return nil
	}
	var out []string
	for _, m := range rePathTok.FindAllStringSubmatch(line, -1) {
		p := strings.TrimRight(m[1], "`.:,)")
		// external hosts / mime fragments / IP:port: contain "." or ":" or start with a digit
		if strings.ContainsAny(p, ".:") || (len(p) > 1 && p[1] >= '0' && p[1] <= '9') {
			continue
		}
		out = append(out, p)
	}
	return out
}

func normalizePath(p string) string { return p } // prefix check handles {params}

// external commands whose flags aren't leather's
var externalCmd = regexp.MustCompile(`\b(gh|git|curl|jq|docker|npm|node|go|make|bash|sh|kubectl|systemctl|sed|awk|grep)\b`)

// ---- shell-tools JSON check ----

func checkShellToolsBlock(block string) []string {
	var problems []string
	var v interface{}
	if err := json.Unmarshal([]byte(block), &v); err != nil {
		return nil // not valid JSON or not a config block; other checks/tests cover parse errors
	}
	var tools []interface{}
	switch tv := v.(type) {
	case map[string]interface{}:
		if arr, ok := tv["tools"].([]interface{}); ok {
			tools = arr
		} else if looksLikeTool(tv) {
			tools = []interface{}{tv}
		}
	}
	for _, it := range tools {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		if !looksLikeTool(m) {
			continue
		}
		if _, bad := m["exec"]; bad {
			problems = append(problems, fmt.Sprintf("tool %q uses removed exec.* form; shell-mcp expects command/args", name(m)))
			continue
		}
		if _, bad := m["argv"]; bad {
			problems = append(problems, fmt.Sprintf("tool %q uses removed argv form", name(m)))
			continue
		}
		if _, ok := m["command"]; !ok {
			problems = append(problems, fmt.Sprintf("tool %q missing required \"command\"", name(m)))
		}
	}
	return problems
}

func looksLikeTool(m map[string]interface{}) bool {
	for _, k := range []string{"command", "exec", "argv", "args"} {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func name(m map[string]interface{}) string {
	if s, ok := m["name"].(string); ok {
		return s
	}
	return "?"
}

// ---- main scan ----

func loadAllow(path string) map[string]bool {
	allow := map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		return allow
	}
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		allow[ln] = true
	}
	return allow
}

func collectMarkdown(roots []string) ([]string, error) {
	var files []string
	seen := map[string]bool{}
	for _, r := range roots {
		fi, err := os.Stat(r)
		if err != nil {
			return nil, fmt.Errorf("docs root %q: %w", r, err)
		}
		if fi.IsDir() {
			filepath.Walk(r, func(p string, f os.FileInfo, err error) error {
				if err == nil && !f.IsDir() && strings.HasSuffix(p, ".md") && !seen[p] {
					seen[p] = true
					files = append(files, p)
				}
				return nil
			})
		} else if strings.HasSuffix(r, ".md") && !seen[r] {
			seen[r] = true
			files = append(files, r)
		}
	}
	sort.Strings(files)
	return files, nil
}

func main() {
	docs := flag.String("docs", "docs,.subagents,README.md,AGENTS.md", "comma-separated doc dirs/files to lint")
	src := flag.String("src", "internal,cmd", "comma-separated source roots for ground truth")
	allowPath := flag.String("allow", "scripts/doclint/allow.txt", "allowlist file (one token per line)")
	jsonOut := flag.Bool("json", false, "emit violations as JSON")
	flag.Parse()

	t, err := scanTruth(strings.Split(*src, ","))
	if err != nil {
		fmt.Fprintln(os.Stderr, "scan error:", err)
		os.Exit(2)
	}
	allow := loadAllow(*allowPath)

	mdFiles, err := collectMarkdown(strings.Split(*docs, ","))
	if err != nil {
		fmt.Fprintln(os.Stderr, "docs error:", err)
		os.Exit(2)
	}

	var vs []violation
	for _, file := range mdFiles {
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(b)
		lines := strings.Split(content, "\n")
		cssVars := map[string]bool{}
		for _, m := range reCSSVar.FindAllStringSubmatch(content, -1) {
			cssVars[m[1]] = true
		}
		for _, m := range reCSSDef.FindAllStringSubmatch(content, -1) {
			cssVars[m[1]] = true
		}

		inFence := false
		fenceLang := ""
		var fenceBuf []string
		fenceStart := 0

		for i, line := range lines {
			ln := i + 1

			// fenced code blocks (for shell-tools JSON)
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				if !inFence {
					inFence = true
					fenceLang = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "```"))
					fenceBuf = nil
					fenceStart = ln
				} else {
					if fenceLang == "json" || fenceLang == "" {
						for _, prob := range checkShellToolsBlock(strings.Join(fenceBuf, "\n")) {
							vs = append(vs, violation{file, fenceStart, "shell-tools", prob})
						}
					}
					inFence = false
				}
				continue
			}
			if inFence {
				fenceBuf = append(fenceBuf, line)
				continue
			}

			// per-line allow directive
			lineAllow := map[string]bool{}
			for _, m := range reIgnore.FindAllStringSubmatch(line, -1) {
				lineAllow[m[1]] = true
			}
			skip := func(tok string) bool { return allow[tok] || lineAllow[tok] }

			// A. env vars
			for _, tok := range extractEnv(line) {
				if t.envs[tok] || skip(tok) {
					continue
				}
				vs = append(vs, violation{file, ln, "env", fmt.Sprintf("undocumented/undefined env var %q (no env read in %s)", tok, *src)})
			}

			// B. flags (backticked; skip lines that are clearly an external command)
			if !externalCmd.MatchString(line) {
				for _, tok := range extractFlags(line) {
					name := strings.TrimPrefix(tok, "--")
					if t.flags[name] || skip(tok) || cssVars[tok] {
						continue
					}
					vs = append(vs, violation{file, ln, "flag", fmt.Sprintf("flag %q not registered in any fs.*() call", tok)})
				}
			}

			// C. endpoints (HTTP-verb rows; exempt Planned)
			if !rePlanned.MatchString(line) {
				for _, p := range extractEndpoints(line) {
					if t.routeCovered(normalizePath(p)) || skip(p) {
						continue
					}
					vs = append(vs, violation{file, ln, "endpoint", fmt.Sprintf("endpoint %q not registered in serve mux", p)})
				}
			}
		}
	}

	sort.Slice(vs, func(a, b int) bool {
		if vs[a].File != vs[b].File {
			return vs[a].File < vs[b].File
		}
		if vs[a].Line != vs[b].Line {
			return vs[a].Line < vs[b].Line
		}
		return vs[a].Check < vs[b].Check
	})

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(vs)
	} else {
		for _, v := range vs {
			fmt.Printf("%s:%d: [%s] %s\n", v.File, v.Line, v.Check, v.Msg)
		}
		fmt.Printf("\nground truth: %d flags, %d env vars, %d routes (%d prefix)\n",
			len(t.flags), len(t.envs), len(t.exact), len(t.prefixes))
		fmt.Printf("%d violation(s)\n", len(vs))
	}
	if len(vs) > 0 {
		os.Exit(1)
	}
}
