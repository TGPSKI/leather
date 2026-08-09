package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeRoutedTannery writes a tannery with one curing that consumes one queue,
// and returns the tannery path. Ingests naming that curing route successfully.
func writeRoutedTannery(t *testing.T, base string) string {
	t.Helper()
	hideDir := filepath.Join(base, "hides")
	curingDir := filepath.Join(base, "curings")
	artDir := filepath.Join(base, "artifacts")
	for _, d := range []string{hideDir, curingDir, artDir} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	curingYAML := "name: summarize\nagent: sum\nqueue: work-q\n"
	if err := os.WriteFile(filepath.Join(curingDir, "summarize.curing.yaml"), []byte(curingYAML), 0600); err != nil {
		t.Fatal(err)
	}
	tannery := "hide_dir: " + hideDir + "\ncuring_dir: " + curingDir + "\nartifact_dir: " + artDir +
		"\nqueues:\n  work-q:\n    concurrency: 1\n"
	path := filepath.Join(base, "tannery.yaml")
	if err := os.WriteFile(path, []byte(tannery), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func ingestStdin(t *testing.T, payload string) func() {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	_, _ = w.WriteString(payload)
	_ = w.Close()
	return func() { os.Stdin = old }
}

// --- issue #75: routing symmetry ---

// `--curing` alone resolves through the curing's declared queue, matching
// `POST /intake?curing=`. It used to store the hide and route nothing.
func TestRunIngest_CuringAloneRoutes(t *testing.T) {
	dir := t.TempDir()
	tannPath := writeRoutedTannery(t, dir)
	defer ingestStdin(t, "payload")()

	var stdout, stderr bytes.Buffer
	code := RunIngest([]string{
		"--tannery", tannPath, "--kind", "test.raw",
		"--state-dir", filepath.Join(dir, "state"),
		"--curing", "summarize",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "curing    summarize") || !strings.Contains(out, "queue     work-q") {
		t.Errorf("output does not report the resolved route:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "state", "queues", "work-q.jsonl")); err != nil {
		t.Errorf("queue file not written: %v", err)
	}
}

// An ingest that names a destination which cannot route must fail, not exit 0
// with the routing lines quietly missing.
func TestRunIngest_UnroutableParamsFail(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown curing", []string{"--curing", "nope"}, "unknown curing"},
		{"undeclared queue", []string{"--queue", "typo-q"}, "not declared"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tannPath := writeRoutedTannery(t, dir)
			defer ingestStdin(t, "payload")()

			args := append([]string{
				"--tannery", tannPath, "--kind", "test.raw",
				"--state-dir", filepath.Join(dir, "state"),
			}, tc.args...)
			var stdout, stderr bytes.Buffer
			if code := RunIngest(args, &stdout, &stderr); code == 0 {
				t.Fatalf("exit 0 on an unroutable ingest; stdout:\n%s", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Errorf("stderr %q does not contain %q", stderr.String(), tc.want)
			}
			// The hide must not be stored: the ingest failed before writing.
			entries, _ := os.ReadDir(filepath.Join(dir, "hides"))
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".lock") {
					t.Errorf("hide written despite the routing failure: %s", e.Name())
				}
			}
		})
	}
}

// A hide-only ingest still succeeds, and now says so instead of leaving the
// reader to infer it from two absent lines.
func TestRunIngest_HideOnlySaysSo(t *testing.T) {
	dir := t.TempDir()
	tannPath := writeRoutedTannery(t, dir)
	defer ingestStdin(t, "payload")()

	var stdout, stderr bytes.Buffer
	code := RunIngest([]string{
		"--tannery", tannPath, "--kind", "test.raw",
		"--state-dir", filepath.Join(dir, "state"),
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "routing   none") {
		t.Errorf("hide-only ingest does not report its routing:\n%s", stdout.String())
	}
}

// --- issue #76: CLI ingest against a live serve ---

func TestRunIngest_RefusesWhenServeHoldsTheLock(t *testing.T) {
	dir := t.TempDir()
	tannPath := writeRoutedTannery(t, dir)
	stateDir := filepath.Join(dir, "state")

	// Stand in for a running serve.
	lf, err := acquireProcessLock(filepath.Join(stateDir, "leather.lock"))
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer releaseProcessLock(lf)

	defer ingestStdin(t, "payload")()
	var stdout, stderr bytes.Buffer
	code := RunIngest([]string{
		"--tannery", tannPath, "--kind", "test.raw",
		"--state-dir", stateDir, "--curing", "summarize",
	}, &stdout, &stderr)

	if code == 0 {
		t.Fatal("ingest succeeded while a serve held the state-dir lock")
	}
	msg := stderr.String()
	if !strings.Contains(msg, "serve is running") {
		t.Errorf("stderr does not name the cause:\n%s", msg)
	}
	if !strings.Contains(msg, "/intake") {
		t.Errorf("stderr does not point at the supported path:\n%s", msg)
	}
	// The queue must be untouched: the enqueue would have been overwritten.
	if _, err := os.Stat(filepath.Join(stateDir, "queues", "work-q.jsonl")); err == nil {
		t.Error("queue file written despite the live serve")
	}
}

// A hide-only ingest touches no queue, so a live serve is no obstacle.
func TestRunIngest_HideOnlyAllowedWhileServeRuns(t *testing.T) {
	dir := t.TempDir()
	tannPath := writeRoutedTannery(t, dir)
	stateDir := filepath.Join(dir, "state")
	lf, err := acquireProcessLock(filepath.Join(stateDir, "leather.lock"))
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer releaseProcessLock(lf)

	defer ingestStdin(t, "payload")()
	var stdout, stderr bytes.Buffer
	if code := RunIngest([]string{
		"--tannery", tannPath, "--kind", "test.raw", "--state-dir", stateDir,
	}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
}

// --- issue #77: scheduler liveness ---

func TestRunStatus_ReportsServeLiveness(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	var out bytes.Buffer
	if code := RunStatus([]string{"--state-dir", stateDir}, &out, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "serve:      not running") {
		t.Errorf("status does not report a dead scheduler:\n%s", out.String())
	}

	lf, err := acquireProcessLock(filepath.Join(stateDir, "leather.lock"))
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer releaseProcessLock(lf)

	out.Reset()
	if code := RunStatus([]string{"--state-dir", stateDir}, &out, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "serve:      running") {
		t.Errorf("status does not report the live scheduler:\n%s", got)
	}
	// The lock names its holder, so the message is actionable.
	if !strings.Contains(got, "pid ") {
		t.Errorf("status does not name the lock holder:\n%s", got)
	}
}

// The probe must not keep the lock it acquired: a serve started right after
// `status` has to be able to take it.
func TestRunStatus_ReleasesTheLockItProbesWith(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")
	var out bytes.Buffer
	if code := RunStatus([]string{"--state-dir", stateDir}, &out, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	lf, err := acquireProcessLock(filepath.Join(stateDir, "leather.lock"))
	if err != nil {
		t.Fatalf("lock still held after status: %v", err)
	}
	releaseProcessLock(lf)
}

// --- issues #71 / #72: validate sees what the runtime sees ---

// writeValidateFixture writes an agent dir and a tool dir and returns both paths.
func writeValidateFixture(t *testing.T, agentFrontMatter string, toolFiles map[string]string) (agentDir, toolDir string) {
	t.Helper()
	base := t.TempDir()
	agentDir = filepath.Join(base, "agents")
	toolDir = filepath.Join(base, "tools")
	for _, d := range []string{agentDir, toolDir} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}
	agent := "---\nname: probe\nmodel: test-model\n" + agentFrontMatter + "---\n\nDo the thing.\n"
	if err := os.WriteFile(filepath.Join(agentDir, "probe.agent.md"), []byte(agent), 0600); err != nil {
		t.Fatal(err)
	}
	for name, body := range toolFiles {
		if err := os.WriteFile(filepath.Join(toolDir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return agentDir, toolDir
}

func runValidate(t *testing.T, agentDir, toolDir string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := RunValidate([]string{
		"--agent-dir", agentDir, "--tool-dir", toolDir, "--config", filepath.Join(t.TempDir(), "none.yaml"),
	}, &stdout, &stderr)
	return code, stdout.String() + stderr.String()
}

// The reproducer from issue #71: two skills declaring the same tool name. Each
// file is well-formed on its own, so only a whole-registry load can see it.
func TestRunValidate_DuplicateToolNameAcrossSkills(t *testing.T) {
	agentDir, toolDir := writeValidateFixture(t, "skills: [one]\n", map[string]string{
		"one.skill.yaml": "name: one\ntools:\n  - name: shared\n    type: mcp\n    mcp: { server: s, tool: a }\n",
		"two.skill.yaml": "name: two\ntools:\n  - name: shared\n    type: mcp\n    mcp: { server: s, tool: b }\n",
	})
	code, out := runValidate(t, agentDir, toolDir)
	if code == 0 {
		t.Fatalf("validate passed on a duplicate tool name:\n%s", out)
	}
	if !strings.Contains(out, "shared") {
		t.Errorf("output does not name the colliding tool:\n%s", out)
	}
}

// An agent naming a skill nobody defines is a run that produces invented tool
// results, so validate has to catch it.
func TestRunValidate_AgentNamesUnknownSkill(t *testing.T) {
	agentDir, toolDir := writeValidateFixture(t, "skills: [ghost]\n", map[string]string{
		"real.skill.yaml": "name: real\ntools:\n  - name: t\n    type: mcp\n    mcp: { server: s, tool: u }\n",
	})
	code, out := runValidate(t, agentDir, toolDir)
	if code == 0 {
		t.Fatalf("validate passed on an unknown skill reference:\n%s", out)
	}
	if !strings.Contains(out, "ghost") {
		t.Errorf("output does not name the dangling reference:\n%s", out)
	}
}

// The one-edit-from-correct case: a skill file listed under toolsets:.
func TestRunValidate_SkillListedAsToolset(t *testing.T) {
	agentDir, toolDir := writeValidateFixture(t, "toolsets: [real]\n", map[string]string{
		"real.skill.yaml": "name: real\ntools:\n  - name: t\n    type: mcp\n    mcp: { server: s, tool: u }\n",
	})
	code, out := runValidate(t, agentDir, toolDir)
	if code == 0 {
		t.Fatalf("validate passed on a skill named under toolsets::\n%s", out)
	}
	if !strings.Contains(out, "loaded under skills:") {
		t.Errorf("output does not point at the near miss:\n%s", out)
	}
}

func TestRunValidate_CleanRegistryPasses(t *testing.T) {
	agentDir, toolDir := writeValidateFixture(t, "skills: [real]\n", map[string]string{
		"real.skill.yaml": "name: real\ntools:\n  - name: t\n    type: mcp\n    mcp: { server: s, tool: u }\n",
	})
	code, out := runValidate(t, agentDir, toolDir)
	if code != 0 {
		t.Fatalf("validate failed on a clean registry:\n%s", out)
	}
	if !strings.Contains(out, "tool registry") {
		t.Errorf("output does not report the registry phase:\n%s", out)
	}
}
