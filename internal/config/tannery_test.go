package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgpski/leather/internal/model"
)

const validTanneryYAML = `
hide_dir:     ./hides
curing_dir:   ./curings
artifact_dir: ./artifacts

routes:
  - name: github-pr-review
    match:
      source: github
      event_type: pull_request_review_comment
    hide_kind: github.pr_review_thread
    curing: review-thread
    queue: default

queues:
  default:
    concurrency: 2
    max_attempts: 3
    max_depth: 1000

webhooks:
  - name: github
    path: /webhooks/github
    source: github
    secret: "raw-secret"
    max_body_bytes: 5242880
`

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "tannery.yaml")
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}
	return p
}

func TestLoadTannery_Valid(t *testing.T) {
	path := writeTempYAML(t, validTanneryYAML)
	dir := filepath.Dir(path)

	cfg, err := LoadTannery(path)
	if err != nil {
		t.Fatalf("LoadTannery: %v", err)
	}

	// Paths are resolved relative to config file directory.
	if cfg.HideDir != filepath.Join(dir, "hides") {
		t.Errorf("HideDir = %q, want %q", cfg.HideDir, filepath.Join(dir, "hides"))
	}
	if cfg.CuringDir != filepath.Join(dir, "curings") {
		t.Errorf("CuringDir = %q, want %q", cfg.CuringDir, filepath.Join(dir, "curings"))
	}
	if cfg.ArtifactDir != filepath.Join(dir, "artifacts") {
		t.Errorf("ArtifactDir = %q, want %q", cfg.ArtifactDir, filepath.Join(dir, "artifacts"))
	}

	// Route.
	if len(cfg.Routes) != 1 {
		t.Fatalf("Routes count = %d, want 1", len(cfg.Routes))
	}
	r := cfg.Routes[0]
	if r.Name != "github-pr-review" {
		t.Errorf("Routes[0].Name = %q", r.Name)
	}
	if r.Match.Source != "github" {
		t.Errorf("Routes[0].Match.Source = %q", r.Match.Source)
	}
	if r.Match.EventType != "pull_request_review_comment" {
		t.Errorf("Routes[0].Match.EventType = %q", r.Match.EventType)
	}
	if r.HideKind != "github.pr_review_thread" {
		t.Errorf("Routes[0].HideKind = %q", r.HideKind)
	}
	if r.Curing != "review-thread" {
		t.Errorf("Routes[0].Curing = %q", r.Curing)
	}
	if r.Queue != "default" {
		t.Errorf("Routes[0].Queue = %q", r.Queue)
	}

	// Queue.
	q, ok := cfg.Queues["default"]
	if !ok {
		t.Fatal("Queues[default] missing")
	}
	if q.Concurrency != 2 {
		t.Errorf("Queues[default].Concurrency = %d", q.Concurrency)
	}
	if q.MaxAttempts != 3 {
		t.Errorf("Queues[default].MaxAttempts = %d", q.MaxAttempts)
	}
	if q.MaxDepth != 1000 {
		t.Errorf("Queues[default].MaxDepth = %d", q.MaxDepth)
	}

	// Webhook.
	if len(cfg.Webhooks) != 1 {
		t.Fatalf("Webhooks count = %d, want 1", len(cfg.Webhooks))
	}
	wh := cfg.Webhooks[0]
	if wh.Name != "github" {
		t.Errorf("Webhooks[0].Name = %q", wh.Name)
	}
	if wh.Path != "/webhooks/github" {
		t.Errorf("Webhooks[0].Path = %q", wh.Path)
	}
	if wh.Source != "github" {
		t.Errorf("Webhooks[0].Source = %q", wh.Source)
	}
	if wh.MaxBodyBytes != 5242880 {
		t.Errorf("Webhooks[0].MaxBodyBytes = %d", wh.MaxBodyBytes)
	}
}

func TestLoadTannery_NotExist(t *testing.T) {
	cfg, err := LoadTannery("/tmp/does_not_exist_tannery_xyz.yaml")
	if err != nil {
		t.Errorf("expected nil error for missing file, got %v", err)
	}
	if cfg.HideDir != "" || cfg.CuringDir != "" || len(cfg.Routes) != 0 {
		t.Errorf("expected zero-value config for missing file, got %+v", cfg)
	}
}

func TestLoadTannery_EmptyPath(t *testing.T) {
	cfg, err := LoadTannery("")
	if err != nil {
		t.Errorf("expected nil error for empty path, got %v", err)
	}
	if cfg.HideDir != "" {
		t.Errorf("expected zero-value config for empty path")
	}
}

func TestLoadTannery_SecretExpansion(t *testing.T) {
	t.Setenv("MY_WEBHOOK_SECRET", "supersecret")
	yaml := strings.ReplaceAll(validTanneryYAML, `"raw-secret"`, `"{{env:MY_WEBHOOK_SECRET}}"`)
	path := writeTempYAML(t, yaml)

	cfg, err := LoadTannery(path)
	if err != nil {
		t.Fatalf("LoadTannery: %v", err)
	}
	if cfg.Webhooks[0].Secret != "supersecret" {
		t.Errorf("Secret = %q, want supersecret", cfg.Webhooks[0].Secret)
	}
}

func TestLoadTannery_EmptySecret(t *testing.T) {
	// An unset/empty env var in a {{env:VAR}} secret reference must now be an error
	// (fail-closed: leather refuses to start with an unresolved webhook secret).
	t.Setenv("MISSING_SECRET", "")
	yaml := strings.ReplaceAll(validTanneryYAML, `"raw-secret"`, `"{{env:MISSING_SECRET}}"`)
	path := writeTempYAML(t, yaml)

	_, err := LoadTannery(path)
	if err == nil {
		t.Fatal("LoadTannery: expected error for unset {{env:MISSING_SECRET}}, got nil")
	}
}

func TestLoadTannery_Defaults(t *testing.T) {
	// A minimal YAML with no queues block.
	yaml := "hide_dir: ./hides\ncuring_dir: ./curings\nartifact_dir: ./artifacts\n"
	path := writeTempYAML(t, yaml)

	cfg, err := LoadTannery(path)
	if err != nil {
		t.Fatalf("LoadTannery: %v", err)
	}
	if cfg.Queues != nil {
		t.Errorf("Queues should be nil when omitted, got %v", cfg.Queues)
	}
	if len(cfg.Routes) != 0 {
		t.Errorf("Routes should be empty when omitted, got %v", cfg.Routes)
	}
}

func TestLoadTannery_MaxDepth(t *testing.T) {
	yaml := validTanneryYAML
	path := writeTempYAML(t, yaml)

	cfg, err := LoadTannery(path)
	if err != nil {
		t.Fatalf("LoadTannery: %v", err)
	}
	if cfg.Queues["default"].MaxDepth != 1000 {
		t.Errorf("MaxDepth = %d, want 1000", cfg.Queues["default"].MaxDepth)
	}
}

func TestLoadTannery_MaxBodyBytes(t *testing.T) {
	path := writeTempYAML(t, validTanneryYAML)
	cfg, err := LoadTannery(path)
	if err != nil {
		t.Fatalf("LoadTannery: %v", err)
	}
	if cfg.Webhooks[0].MaxBodyBytes != 5242880 {
		t.Errorf("MaxBodyBytes = %d, want 5242880", cfg.Webhooks[0].MaxBodyBytes)
	}
}

func TestValidateTannery_OK(t *testing.T) {
	cfg := TanneryConfig{
		Routes: []model.TanneryRoute{
			{Name: "r1", Curing: "review-thread", Queue: "default"},
		},
		Queues: map[string]model.QueueConcurrencyConfig{
			"default": {},
		},
	}
	defs := []model.CuringDefinition{{Name: "review-thread"}}
	if err := ValidateTannery(cfg, defs); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestValidateTannery_RouteRefsUnknownCuring(t *testing.T) {
	cfg := TanneryConfig{
		Routes: []model.TanneryRoute{
			{Name: "r1", Curing: "nonexistent", Queue: "default"},
		},
		Queues: map[string]model.QueueConcurrencyConfig{"default": {}},
	}
	defs := []model.CuringDefinition{{Name: "review-thread"}}
	if err := ValidateTannery(cfg, defs); err == nil {
		t.Error("expected error for unknown curing reference")
	}
}

func TestValidateTannery_RouteRefsUnknownQueue(t *testing.T) {
	cfg := TanneryConfig{
		Routes: []model.TanneryRoute{
			{Name: "r1", Curing: "review-thread", Queue: "missing-queue"},
		},
		Queues: map[string]model.QueueConcurrencyConfig{"default": {}},
	}
	defs := []model.CuringDefinition{{Name: "review-thread"}}
	if err := ValidateTannery(cfg, defs); err == nil {
		t.Error("expected error for undeclared queue")
	}
}

func TestValidateTannery_RoutesButNoCurings(t *testing.T) {
	cfg := TanneryConfig{
		Routes: []model.TanneryRoute{
			{Name: "r1", Curing: "x", Queue: "default"},
		},
		Queues: map[string]model.QueueConcurrencyConfig{"default": {}},
	}
	if err := ValidateTannery(cfg, nil); err == nil {
		t.Error("expected error when routes configured but no curings loaded")
	}
}

func FuzzLoadTannery(f *testing.F) {
	// Seed with known-good, missing-block, and edge-case configs.
	f.Add(validTanneryYAML)
	f.Add("hide_dir: ./hides\n")
	f.Add("routes:\n  - name: x\n    match:\n      source: y\n")
	f.Add("queues:\n  default:\n    concurrency: 1\n")
	f.Add("webhooks:\n  - name: gh\n    path: /x\n")
	f.Add("")
	f.Add("   \n\n\n---\n")

	f.Fuzz(func(t *testing.T, src string) {
		// Must never panic.
		cfg, _ := parseTanneryYAML(src)
		// Basic sanity: all collected routes must have been initialized.
		for _, r := range cfg.Routes {
			_ = r.Name
		}
	})
}
