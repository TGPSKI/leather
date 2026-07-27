package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// End-to-end gate test: write a tiny corpus + predictions and assert the gate's
// verdict and key metrics. Runs sigeval.go as a subprocess (it's package main).
func writeTmp(t *testing.T, name, body string) string {
	t.Helper()
	p := t.TempDir() + "/" + name
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func run(t *testing.T, corpus, pred string, args ...string) (string, int) {
	t.Helper()
	// Empty -overrides/-split keeps these tests scoring raw gold unless a case
	// opts in; otherwise they would pick up the repo's real overlay paths.
	base := []string{"run", "sigeval.go", "-gold", corpus, "-pred", pred,
		"-overrides", "", "-split", ""}
	cmd := exec.Command("go", append(base, args...)...)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	return string(out), code
}

func TestGatePasses(t *testing.T) {
	corpus := writeTmp(t, "c.jsonl", strings.Join([]string{
		`{"number":1,"sig":"sig-network"}`,
		`{"number":2,"sig":"sig-node"}`,
		`{"number":3,"sig":"sig-storage","accept":["sig-storage","sig-node"]}`,
		`{"number":4,"sig":"unknown","accept":["sig-network","unknown"]}`,
	}, "\n"))
	pred := writeTmp(t, "p.jsonl", strings.Join([]string{
		`{"number":1,"predicted":"sig-network"}`, // correct
		`{"number":2,"predicted":"sig-node"}`,    // correct
		`{"number":3,"predicted":"sig-node"}`,    // correct via accept-set
		`{"number":4,"predicted":"unknown"}`,     // abstain, allowed -> correct
	}, "\n"))
	out, code := run(t, corpus, pred, "-min-accuracy", "0.90", "-max-abstain", "0.50",
		"-core", "sig-network,sig-node,sig-storage")
	if code != 0 {
		t.Errorf("expected gate pass (0), got %d\n%s", code, out)
	}
	if !strings.Contains(out, "GATE PASSED") {
		t.Errorf("missing GATE PASSED\n%s", out)
	}
}

func TestSlashFormPredictionsNormalized(t *testing.T) {
	// The match agent should emit catalog form (sig-network) but sometimes emits
	// the label form (sig/network). The scorer must treat them as equal.
	corpus := writeTmp(t, "c.jsonl", strings.Join([]string{
		`{"number":1,"sig":"sig-network"}`,
		`{"number":2,"sig":"sig-storage"}`,
	}, "\n"))
	pred := writeTmp(t, "p.jsonl", strings.Join([]string{
		`{"number":1,"predicted":"sig/network"}`,  // label form -> should count correct
		`{"number":2,"predicted":"SIG-STORAGE "}`, // upper + trailing space -> correct
	}, "\n"))
	out, code := run(t, corpus, pred, "-min-accuracy", "0.90", "-min-core-recall", "0.90",
		"-core", "sig-network,sig-storage")
	if code != 0 {
		t.Errorf("expected gate pass (0) after normalization, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "100.0%") {
		t.Errorf("expected 100%% accuracy after slash/case normalization\n%s", out)
	}
	if strings.Contains(out, "sig/network") {
		t.Errorf("report should not contain un-normalized slash form\n%s", out)
	}
}

func TestGateFailsOnCoreRecall(t *testing.T) {
	corpus := writeTmp(t, "c.jsonl", strings.Join([]string{
		`{"number":1,"sig":"sig-network"}`,
		`{"number":2,"sig":"sig-network"}`,
	}, "\n"))
	pred := writeTmp(t, "p.jsonl", strings.Join([]string{
		`{"number":1,"predicted":"sig-network"}`, // correct
		`{"number":2,"predicted":"sig-node"}`,    // wrong -> network recall 50%
	}, "\n"))
	// -min-class-support 0 forces the per-class floor to apply at any support, so
	// this case still exercises the core-recall check itself; -min-macro-recall 0
	// isolates it from the macro gate.
	out, code := run(t, corpus, pred, "-min-core-recall", "0.90", "-core", "sig-network",
		"-min-class-support", "0", "-min-macro-recall", "0")
	if code != 1 {
		t.Errorf("expected gate fail (1), got %d\n%s", code, out)
	}
	if !strings.Contains(out, "GATE FAILED") {
		t.Errorf("missing GATE FAILED\n%s", out)
	}
	if !strings.Contains(out, "[FAIL] core-recall:sig-network") {
		t.Errorf("expected the core-recall check to be the failing one\n%s", out)
	}
}

// The gold overrides overlay must win over gold.jsonl on load, so relabels are a
// diffable layer instead of a hand-edit of the pristine answer key.
func TestGoldOverridesApplied(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Raw gold demands a concrete SIG on #1; the model abstained, so without the
	// overlay that row scores wrong.
	corpus := write("c.jsonl", strings.Join([]string{
		`{"number":1,"sig":"sig-api-machinery","accept":["sig-api-machinery","sig-node"]}`,
		`{"number":2,"sig":"sig-network"}`,
	}, "\n"))
	pred := write("p.jsonl", strings.Join([]string{
		`{"number":1,"predicted":"unknown"}`,
		`{"number":2,"predicted":"sig-network"}`,
	}, "\n"))
	ovr := write("o.jsonl", `{"number":1,"sig":"unknown","accept":["sig-api-machinery","sig-node"],"reason":"gold-sanity: content-free"}`)

	out, _ := run(t, corpus, pred, "-min-macro-recall", "0")
	if !strings.Contains(out, "50.0% (1/2)") {
		t.Errorf("without overrides the abstention should score wrong\n%s", out)
	}
	out, _ = run(t, corpus, pred, "-overrides", ovr, "-min-macro-recall", "0")
	if !strings.Contains(out, "100.0% (2/2)") {
		t.Errorf("override should relabel #1 to unknown and make the abstention correct\n%s", out)
	}
	if !strings.Contains(out, "gold-overrides-applied=1") {
		t.Errorf("report should state how many overrides were applied\n%s", out)
	}
}

// The split manifest drives the dev / held-out report — the anti-overfitting
// evidence. Tuning only sees `dev`; held-out tracking full means the rules
// generalize rather than memorize.
func TestSplitReport(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	corpus := write("c.jsonl", strings.Join([]string{
		`{"number":1,"sig":"sig-network"}`,
		`{"number":2,"sig":"sig-network"}`,
		`{"number":3,"sig":"sig-node"}`,
		`{"number":4,"sig":"sig-node"}`,
	}, "\n"))
	pred := write("p.jsonl", strings.Join([]string{
		`{"number":1,"predicted":"sig-network"}`, // dev, correct
		`{"number":2,"predicted":"sig-network"}`, // dev, correct
		`{"number":3,"predicted":"sig-node"}`,    // held-out, correct
		`{"number":4,"predicted":"sig-network"}`, // held-out, wrong
	}, "\n"))
	sp := write("s.jsonl", strings.Join([]string{
		`{"number":1,"tier":"dev"}`,
		`{"number":2,"tier":"dev"}`,
		`{"number":3,"tier":"holdout"}`,
		`{"number":4,"tier":"holdout"}`,
	}, "\n"))
	out, _ := run(t, corpus, pred, "-split", sp, "-min-macro-recall", "0")
	for _, want := range []string{"dev-slice: 100.0% (2/2)", "held-out:   50.0% (1/2)"} {
		if !strings.Contains(out, want) {
			t.Errorf("split report missing %q\n%s", want, out)
		}
	}
}

// Below -min-class-support a per-class recall floor gates on sampling noise
// (SE ~11 points at n=10), so the class is reported but excluded from the gate;
// macro-recall carries per-class health instead.
func TestMinClassSupportGuardAndMacroRecall(t *testing.T) {
	corpus := writeTmp(t, "c.jsonl", strings.Join([]string{
		`{"number":1,"sig":"sig-network"}`,
		`{"number":2,"sig":"sig-network"}`,
		`{"number":3,"sig":"sig-node"}`,
		`{"number":4,"sig":"sig-node"}`,
	}, "\n"))
	pred := writeTmp(t, "p.jsonl", strings.Join([]string{
		`{"number":1,"predicted":"sig-network"}`,
		`{"number":2,"predicted":"sig-node"}`, // network recall 50%
		`{"number":3,"predicted":"sig-node"}`,
		`{"number":4,"predicted":"sig-node"}`,
	}, "\n"))
	// Support 2 per class is far below the guard: the 90% floor must not gate.
	out, code := run(t, corpus, pred, "-core", "sig-network,sig-node",
		"-min-core-recall", "0.90", "-min-class-support", "20",
		"-min-accuracy", "0.70", "-min-macro-recall", "0.70")
	if code != 0 {
		t.Errorf("expected pass: tiny-support classes must not be gated on recall\n%s", out)
	}
	if !strings.Contains(out, "not gated: support 2 < 20") {
		t.Errorf("expected the skipped class to be reported for information\n%s", out)
	}
	// Macro-recall is (50% + 100%) / 2 = 75%, so it still catches real per-class
	// rot even when every class is too small for its own floor.
	out, code = run(t, corpus, pred, "-core", "sig-network,sig-node",
		"-min-class-support", "20", "-min-accuracy", "0.70", "-min-macro-recall", "0.85")
	if code != 1 {
		t.Errorf("expected macro-recall (75%%) to fail the 85%% gate\n%s", out)
	}
	if !strings.Contains(out, "[FAIL] macro-recall") {
		t.Errorf("expected macro-recall to be the failing check\n%s", out)
	}
}
