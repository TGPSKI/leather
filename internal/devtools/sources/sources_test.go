package sources

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/devtools/bus"
	"github.com/tgpski/leather/internal/model"
)

func fixedNow() time.Time { return time.Unix(1000000, 0) }

func newTestWiring() (*Wiring, *bus.Bus) {
	b := bus.New(64)
	w := Wire(b, Deps{Now: fixedNow})
	return w, b
}

func TestPublishQueueRun_EventShape(t *testing.T) {
	w, b := newTestWiring()
	item := model.QueueItem{
		ID:           "item-abc",
		HideID:       "hide-xyz",
		HideKind:     "github.pr",
		AttemptCount: 2,
		Payload: map[string]any{
			"repo":  "leather",
			"issue": 42,
		},
	}

	seq := w.PublishQueueRun("my-agent", "work-queue", item)
	if seq == 0 {
		t.Fatal("expected non-zero seq")
	}

	events := b.Snapshot()
	var ev bus.Event
	for _, e := range events {
		if e.Seq == seq {
			ev = e
			break
		}
	}
	if ev.Seq == 0 {
		t.Fatal("event not found on bus")
	}

	if ev.Kind != "queue.run" {
		t.Errorf("kind: got %q, want %q", ev.Kind, "queue.run")
	}
	if ev.Source != "scheduler" {
		t.Errorf("source: got %q, want %q", ev.Source, "scheduler")
	}
	if ev.EntityKind != "queue_item" {
		t.Errorf("entity_kind: got %q, want %q", ev.EntityKind, "queue_item")
	}
	if ev.EntityID != "item-abc" {
		t.Errorf("entity_id: got %q, want %q", ev.EntityID, "item-abc")
	}

	var p map[string]any
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if p["agent"] != "my-agent" {
		t.Errorf("payload.agent: got %v", p["agent"])
	}
	if p["queue"] != "work-queue" {
		t.Errorf("payload.queue: got %v", p["queue"])
	}
	if p["item_id"] != "item-abc" {
		t.Errorf("payload.item_id: got %v", p["item_id"])
	}
	if p["hide_id"] != "hide-xyz" {
		t.Errorf("payload.hide_id: got %v", p["hide_id"])
	}
	if p["hide_kind"] != "github.pr" {
		t.Errorf("payload.hide_kind: got %v", p["hide_kind"])
	}
	// attempt is serialized as float64 by JSON
	if p["attempt"] != float64(2) {
		t.Errorf("payload.attempt: got %v", p["attempt"])
	}

	// payload_keys must contain key names but NOT values
	rawKeys, ok := p["payload_keys"].([]any)
	if !ok {
		t.Fatalf("payload_keys missing or wrong type: %T", p["payload_keys"])
	}
	keys := make(map[string]bool, len(rawKeys))
	for _, k := range rawKeys {
		keys[k.(string)] = true
	}
	if !keys["repo"] || !keys["issue"] {
		t.Errorf("payload_keys missing expected keys: %v", rawKeys)
	}

	// Payload values must NOT appear anywhere in the serialized payload
	raw := string(ev.Payload)
	if containsSubstr(raw, "leather") || containsSubstr(raw, "42") {
		// "leather" is the repo value, "42" is the issue value
		// NOTE: "42" could appear as attempt count (2) but not as issue value
		// We only check for "leather" since that's unambiguous
		if containsSubstr(raw, `"leather"`) {
			t.Errorf("payload value %q found in serialized event payload", "leather")
		}
	}
}

func TestPublishQueueRun_NilSafe(t *testing.T) {
	var w *Wiring
	// Should not panic
	seq := w.PublishQueueRun("agent", "queue", model.QueueItem{ID: "x"})
	if seq != 0 {
		t.Errorf("expected 0 from nil wiring, got %d", seq)
	}
}

func TestPublishQueueRun_EmptyPayload(t *testing.T) {
	w, _ := newTestWiring()
	item := model.QueueItem{ID: "empty", Payload: nil}
	seq := w.PublishQueueRun("a", "q", item)
	if seq == 0 {
		t.Fatal("expected non-zero seq")
	}
}

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
