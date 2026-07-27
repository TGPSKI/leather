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
	base := []string{"run", "sigeval.go", "-gold", corpus, "-pred", pred}
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
		`{"number":2,"predicted":"sig-node"}`,     // correct
		`{"number":3,"predicted":"sig-node"}`,     // correct via accept-set
		`{"number":4,"predicted":"unknown"}`,      // abstain, allowed -> correct
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

func TestGateFailsOnCoreRecall(t *testing.T) {
	corpus := writeTmp(t, "c.jsonl", strings.Join([]string{
		`{"number":1,"sig":"sig-network"}`,
		`{"number":2,"sig":"sig-network"}`,
	}, "\n"))
	pred := writeTmp(t, "p.jsonl", strings.Join([]string{
		`{"number":1,"predicted":"sig-network"}`, // correct
		`{"number":2,"predicted":"sig-node"}`,     // wrong -> network recall 50%
	}, "\n"))
	out, code := run(t, corpus, pred, "-min-core-recall", "0.90", "-core", "sig-network")
	if code != 1 {
		t.Errorf("expected gate fail (1), got %d\n%s", code, out)
	}
	if !strings.Contains(out, "GATE FAILED") {
		t.Errorf("missing GATE FAILED\n%s", out)
	}
}
