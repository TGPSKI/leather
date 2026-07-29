package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShellToolsPaths(t *testing.T) {
	dir := t.TempDir()
	stPath := filepath.Join(dir, "shell-tools.json")
	if err := os.WriteFile(stPath, []byte(`{"tools":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	yaml := `servers:
  - name: shell
    command: shell-mcp shell-tools.json
    transport: stdio
  - name: quoted
    command: ["shell-mcp", "shell-tools.json"]
  - name: missing
    command: shell-mcp /does/not/exist/tools.json
  - name: nojson
    command: some-other-server --flag
`
	got := shellToolsPaths(yaml, dir)
	if len(got) != 1 || got[0] != stPath {
		t.Fatalf("shellToolsPaths = %v, want [%s] (existing file deduped, missing skipped)", got, stPath)
	}
}
