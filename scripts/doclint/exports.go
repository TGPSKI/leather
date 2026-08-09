package main

// Forward-direction check: every exported symbol in internal/ must appear in
// its package's module doc under docs/modules/.
//
// main.go checks the reverse direction — that documented tokens resolve to real
// code. That catches phantom docs but not silent rot: an export added without a
// doc row is invisible to it. Both directions are needed.
//
// Existing gaps live in a baseline file (scripts/doclint/undocumented.txt),
// validated in BOTH directions so it can only shrink:
//
//   - undocumented and not in the baseline -> new drift, fail.
//   - in the baseline but now documented (or deleted) -> stale entry, fail with
//     "remove it". Documenting a symbol therefore forces the entry out; the
//     baseline can never quietly retain debt that is already paid.
//
// A "# max: N" header caps the entry count. Lowering N is a one-line commit
// made while cleaning up; raising it is a conspicuous one-line diff that a
// reviewer will question. That is the growth ratchet — rule 2 alone still
// permits additions, it just makes them visible.
//
// A package with no module doc at all is never baselineable: that is a missing
// file, not N missing symbols, and filing it as N symbol entries would bury it.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// export is one exported symbol and where it is declared.
type export struct {
	Pkg  string // package dir relative to repo root, e.g. "internal/devtools/bus"
	Name string // symbol name; methods are recorded under their own name
	File string
	Line int
}

// Key is the baseline entry form: "<pkg>.<Name>".
func (e export) Key() string { return e.Pkg + "." + e.Name }

// scanExports parses every non-test .go file under root and returns the
// exported top-level declarations plus exported methods on exported types.
func scanExports(root string) ([]export, error) {
	byDir := map[string][]export{}
	fset := token.NewFileSet()

	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			// A file that does not parse is a build problem, not a doc problem.
			// Skip it rather than reporting bogus doc violations.
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(p))
		for _, d := range f.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if !decl.Name.IsExported() {
					continue
				}
				// Skip methods on unexported types: they are unreachable from
				// outside the package, so they are not public API.
				if decl.Recv != nil && !exportedReceiver(decl.Recv) {
					continue
				}
				byDir[dir] = append(byDir[dir], export{
					Pkg: dir, Name: decl.Name.Name,
					File: filepath.ToSlash(p), Line: fset.Position(decl.Name.Pos()).Line,
				})
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							byDir[dir] = append(byDir[dir], export{
								Pkg: dir, Name: s.Name.Name,
								File: filepath.ToSlash(p), Line: fset.Position(s.Name.Pos()).Line,
							})
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								byDir[dir] = append(byDir[dir], export{
									Pkg: dir, Name: n.Name,
									File: filepath.ToSlash(p), Line: fset.Position(n.Pos()).Line,
								})
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var out []export
	for _, es := range byDir {
		out = append(out, es...)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Pkg != out[b].Pkg {
			return out[a].Pkg < out[b].Pkg
		}
		return out[a].Name < out[b].Name
	})
	return out, nil
}

func exportedReceiver(recv *ast.FieldList) bool {
	if len(recv.List) == 0 {
		return false
	}
	t := recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	// Generic receivers appear as IndexExpr / IndexListExpr; unwrap to the base.
	switch idx := t.(type) {
	case *ast.IndexExpr:
		t = idx.X
	case *ast.IndexListExpr:
		t = idx.X
	}
	id, ok := t.(*ast.Ident)
	return ok && id.IsExported()
}

// docPathFor maps a package dir to its module doc, walking up so that nested
// packages documented by a parent's doc resolve correctly — internal/devtools/bus
// is covered by docs/modules/devtools.md. Returns "" when no doc exists.
func docPathFor(pkgDir, modulesDir string) string {
	rel := strings.TrimPrefix(filepath.ToSlash(pkgDir), "internal/")
	parts := strings.Split(rel, "/")
	for i := len(parts); i > 0; i-- {
		p := filepath.Join(modulesDir, strings.Join(parts[:i], "-")+".md")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		// Also accept the nested layout docs/modules/<a>/<b>.md.
		p = filepath.Join(modulesDir, filepath.Join(parts[:i]...)+".md")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// documented reports whether name appears as a whole word in doc text.
func documented(docText, name string) bool {
	for i := 0; ; {
		j := strings.Index(docText[i:], name)
		if j < 0 {
			return false
		}
		start := i + j
		end := start + len(name)
		if !isIdentByte(docText, start-1) && !isIdentByte(docText, end) {
			return true
		}
		i = start + 1
		if i >= len(docText) {
			return false
		}
	}
}

func isIdentByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// baseline is the parsed undocumented.txt: known gaps plus the growth ceiling.
type baseline struct {
	max     int
	entries map[string]int // key -> line number in the baseline file
	path    string
	hasMax  bool
}

func loadBaseline(path string) (baseline, error) {
	b := baseline{entries: map[string]int{}, path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return b, err
	}
	for i, ln := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if comment, isComment := strings.CutPrefix(t, "#"); isComment {
			if rest, ok := strings.CutPrefix(strings.TrimSpace(comment), "max:"); ok {
				n, cerr := strconv.Atoi(strings.TrimSpace(rest))
				if cerr != nil {
					return b, fmt.Errorf("%s:%d: bad max header %q", path, i+1, t)
				}
				b.max, b.hasMax = n, true
			}
			continue
		}
		b.entries[t] = i + 1
	}
	return b, nil
}

const baselineHeader = `# doclint export baseline — exported symbols with no row in their module doc.
#
# This is a DEBT LEDGER, not an exceptions list. Curated exceptions belong in
# allow.txt. Every line here is a doc row someone still owes.
#
# It can only shrink. Documenting a symbol makes its entry stale, and a stale
# entry is a hard failure telling you to delete the line — so paid debt cannot
# sit here looking unpaid. Adding a line requires raising "max" below, which is
# a visible one-line diff. Regenerate with:
#
#	go run ./scripts/doclint -write-baseline
#
# max: %d
`

func writeBaseline(path string, keys []string, max int) error {
	sort.Strings(keys)
	var sb strings.Builder
	fmt.Fprintf(&sb, baselineHeader, max)
	lastPkg := ""
	for _, k := range keys {
		pkg := k[:strings.LastIndex(k, ".")]
		if pkg != lastPkg {
			sb.WriteString("\n")
			lastPkg = pkg
		}
		sb.WriteString(k)
		sb.WriteString("\n")
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// checkExports runs the forward check and returns violations.
func checkExports(srcRoot, modulesDir, baselinePath string, write bool) ([]violation, error) {
	exports, err := scanExports(srcRoot)
	if err != nil {
		return nil, err
	}
	b, err := loadBaseline(baselinePath)
	if err != nil {
		return nil, err
	}

	docCache := map[string]string{}
	var vs []violation
	undoc := map[string]export{}
	missingDoc := map[string]export{} // pkg -> first export, for packages with no doc

	for _, e := range exports {
		doc := docPathFor(e.Pkg, modulesDir)
		if doc == "" {
			if _, seen := missingDoc[e.Pkg]; !seen {
				missingDoc[e.Pkg] = e
			}
			continue
		}
		text, ok := docCache[doc]
		if !ok {
			raw, rerr := os.ReadFile(doc)
			if rerr != nil {
				return nil, rerr
			}
			text = string(raw)
			docCache[doc] = text
		}
		if !documented(text, e.Name) {
			undoc[e.Key()] = e
		}
	}

	// A package with no module doc is a missing file, not N missing symbols.
	// Never baselineable.
	for pkg, e := range missingDoc {
		vs = append(vs, violation{e.File, 1, "module-doc",
			fmt.Sprintf("package %s has no module doc; expected %s/%s.md",
				pkg, modulesDir, strings.ReplaceAll(strings.TrimPrefix(pkg, "internal/"), "/", "-"))})
	}

	if write {
		keys := make([]string, 0, len(undoc))
		for k := range undoc {
			keys = append(keys, k)
		}
		max := b.max
		if !b.hasMax || len(keys) < max {
			max = len(keys)
		}
		if len(keys) > max {
			return nil, fmt.Errorf(
				"refusing to write baseline: %d undocumented exports exceeds max %d — "+
					"document the new symbols, or deliberately raise the max header in %s",
				len(keys), max, baselinePath)
		}
		return vs, writeBaseline(baselinePath, keys, max)
	}

	// 1. New drift: undocumented and not baselined.
	for k, e := range undoc {
		if _, ok := b.entries[k]; !ok {
			vs = append(vs, violation{e.File, e.Line, "export",
				fmt.Sprintf("exported %s has no row in %s (add one, or baseline it in %s)",
					e.Name, docPathFor(e.Pkg, modulesDir), baselinePath)})
		}
	}

	// 2. Stale baseline: documented or deleted since the entry was written.
	// This is what forces the ledger to shrink.
	live := map[string]bool{}
	for _, e := range exports {
		live[e.Key()] = true
	}
	for k, ln := range b.entries {
		if _, still := undoc[k]; still {
			continue
		}
		reason := "is now documented"
		if !live[k] {
			reason = "no longer exists"
		}
		vs = append(vs, violation{baselinePath, ln, "baseline",
			fmt.Sprintf("stale entry %q %s — delete this line and lower the max header", k, reason)})
	}

	// 3. Growth ceiling.
	if b.hasMax && len(b.entries) > b.max {
		vs = append(vs, violation{baselinePath, 1, "baseline",
			fmt.Sprintf("baseline has %d entries, exceeding max %d", len(b.entries), b.max)})
	}

	return vs, nil
}
