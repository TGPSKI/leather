package main

import "testing"

func TestExtractFlags(t *testing.T) {
	cases := map[string][]string{
		"use `--no-shell` to harden":     {"--no-shell"},
		"`--api` and `--log-level` here": {"--api", "--log-level"},
		"no backticks --api ignored":     nil,
		"plain prose without flags":      nil,
	}
	for in, want := range cases {
		got := extractFlags(in)
		if len(got) != len(want) {
			t.Errorf("extractFlags(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("extractFlags(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

func TestExtractEndpoints(t *testing.T) {
	cases := map[string][]string{
		"| `GET` | `/status/agents` |":       {"/status/agents"},
		"POST /replay/run/{id}":              {"/replay/run/{id}"},
		"returns text/plain here":            nil, // mime, not a path (preceded by 't')
		"base http://127.0.0.1:8080 GET x":   nil, // host:port stripped
		"no http verb `/status` mentioned":   nil, // no verb -> not an endpoint claim
		"GET /api.telegram.org/bot external": nil, // external host (contains '.')
	}
	for in, want := range cases {
		got := extractEndpoints(in)
		if len(got) != len(want) {
			t.Errorf("extractEndpoints(%q) = %v, want %v", in, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("extractEndpoints(%q)[%d] = %q, want %q", in, i, got[i], want[i])
			}
		}
	}
}

func TestCheckShellToolsBlock(t *testing.T) {
	execForm := `{"tools":[{"name":"x","exec":{"argv":["echo"]}}]}`
	if got := checkShellToolsBlock(execForm); len(got) != 1 {
		t.Errorf("exec form should yield 1 problem, got %v", got)
	}
	cmdForm := `{"tools":[{"name":"x","command":"bash","args":["-c","echo"]}]}`
	if got := checkShellToolsBlock(cmdForm); len(got) != 0 {
		t.Errorf("valid command form should be clean, got %v", got)
	}
	missing := `{"tools":[{"name":"x","args":["echo"]}]}`
	if got := checkShellToolsBlock(missing); len(got) != 1 {
		t.Errorf("missing command should yield 1 problem, got %v", got)
	}
	notATool := `{"model":"llama3","temperature":0.2}`
	if got := checkShellToolsBlock(notATool); len(got) != 0 {
		t.Errorf("non-tool JSON should be ignored, got %v", got)
	}
}

func TestCollectMarkdownMissingRoot(t *testing.T) {
	_, err := collectMarkdown([]string{"docs-typo-does-not-exist"})
	if err == nil {
		t.Fatal("collectMarkdown with a nonexistent root should return an error, got nil")
	}
}

func TestExtractFlagsMultipleAllowDirectives(t *testing.T) {
	line := "`--foo` and `--bar` here <!-- doclint:allow --foo doclint:allow --bar -->"
	m := reIgnore.FindAllStringSubmatch(line, -1)
	if len(m) != 2 {
		t.Fatalf("expected 2 doclint:allow directives on the line, got %d: %v", len(m), m)
	}
	lineAllow := map[string]bool{}
	for _, mm := range m {
		lineAllow[mm[1]] = true
	}
	for _, tok := range []string{"--foo", "--bar"} {
		if !lineAllow[tok] {
			t.Errorf("expected %q to be allowed, lineAllow = %v", tok, lineAllow)
		}
	}
}

func TestRouteCovered(t *testing.T) {
	tr := truth{
		exact:    map[string]bool{"/status": true, "/queues": true, "/queues/": true, "/replay/control": true},
		prefixes: []string{"/queues/"},
	}
	covered := []string{"/status", "/queues", "/queues/{name}/requeue"}
	uncovered := []string{"/status/agents", "/replay/runs", "/runs"}
	for _, p := range covered {
		if !tr.routeCovered(p) {
			t.Errorf("routeCovered(%q) = false, want true", p)
		}
	}
	for _, p := range uncovered {
		if tr.routeCovered(p) {
			t.Errorf("routeCovered(%q) = true, want false", p)
		}
	}
}
