package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TGPSKI/leather/internal/model"
	"github.com/TGPSKI/leather/internal/session"
)

func TestBuildLLMClient(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "f.jsonl")
	if err := os.WriteFile(fixture, []byte(`{"content":"x","finish_reason":"stop"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("fixture and record are mutually exclusive", func(t *testing.T) {
		_, err := buildLLMClient(model.Config{LLMFixture: fixture, LLMRecord: "out.jsonl"})
		if err == nil {
			t.Fatal("want error for fixture+record, got nil")
		}
	})

	t.Run("fixture selects FixtureClient", func(t *testing.T) {
		c, err := buildLLMClient(model.Config{LLMFixture: fixture})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := c.(*session.FixtureClient); !ok {
			t.Fatalf("got %T, want *session.FixtureClient", c)
		}
	})

	t.Run("record wraps live client", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "rec.jsonl")
		c, err := buildLLMClient(model.Config{LLMRecord: out})
		if err != nil {
			t.Fatal(err)
		}
		rec, ok := c.(*session.RecordingClient)
		if !ok {
			t.Fatalf("got %T, want *session.RecordingClient", c)
		}
		rec.Close() //nolint:errcheck
	})

	t.Run("default is HTTPClient", func(t *testing.T) {
		c, err := buildLLMClient(model.Config{LLMEndpoint: "http://127.0.0.1:1"})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := c.(*session.HTTPClient); !ok {
			t.Fatalf("got %T, want *session.HTTPClient", c)
		}
	})

	t.Run("missing fixture file fails", func(t *testing.T) {
		if _, err := buildLLMClient(model.Config{LLMFixture: "/does/not/exist.jsonl"}); err == nil {
			t.Fatal("want error for missing fixture file")
		}
	})
}
