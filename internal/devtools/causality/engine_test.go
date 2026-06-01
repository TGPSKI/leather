package causality

import (
	"context"
	"testing"

	"github.com/tgpski/leather/internal/devtools/bus"
)

func TestTraceIncludesParentCauseAndChildren(t *testing.T) {
	t.Helper()
	b := bus.New(32)
	a := b.Publish(bus.Event{Kind: "a"})
	bSeq := b.Publish(bus.Event{Kind: "b", ParentSeq: a})
	c := b.Publish(bus.Event{Kind: "c"})
	d := b.Publish(bus.Event{Kind: "d", ParentSeq: bSeq})
	_ = d
	_ = b.AppendCause(c, bSeq)

	eng := NewEngine()
	res := eng.Trace(context.Background(), b, bSeq, TraceOptions{Depth: 4, Breadth: 20})
	if res.RootSeq != bSeq {
		t.Fatalf("root = %d, want %d", res.RootSeq, bSeq)
	}
	seen := map[uint64]bool{}
	for _, n := range res.Nodes {
		seen[n.Event.Seq] = true
	}
	for _, want := range []uint64{a, bSeq, c, d} {
		if !seen[want] {
			t.Fatalf("missing seq %d in trace nodes", want)
		}
	}
}

func TestLinkForward(t *testing.T) {
	t.Helper()
	b := bus.New(8)
	parent := b.Publish(bus.Event{Kind: "parent"})
	child := b.Publish(bus.Event{Kind: "child"})
	eng := NewEngine()
	if ok := eng.LinkForward(b, parent, child); !ok {
		t.Fatal("LinkForward returned false")
	}
	snap := b.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	if len(snap[1].CausesSeq) != 1 || snap[1].CausesSeq[0] != parent {
		t.Fatalf("causes = %v, want [%d]", snap[1].CausesSeq, parent)
	}
}
