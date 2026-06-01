package bus

import (
	"context"
	"testing"
	"time"
)

func TestBusPublishSnapshotEviction(t *testing.T) {
	t.Helper()
	b := New(2)

	seq1 := b.Publish(Event{Kind: "one"})
	seq2 := b.Publish(Event{Kind: "two"})
	seq3 := b.Publish(Event{Kind: "three"})

	if seq1 != 1 || seq2 != 2 || seq3 != 3 {
		t.Fatalf("unexpected seq values: got [%d %d %d]", seq1, seq2, seq3)
	}

	snap := b.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	if snap[0].Seq != 2 || snap[1].Seq != 3 {
		t.Fatalf("snapshot seqs = [%d %d], want [2 3]", snap[0].Seq, snap[1].Seq)
	}

	stats := b.Stats()
	if stats.Evicted != 1 {
		t.Fatalf("evicted = %d, want 1", stats.Evicted)
	}
	if stats.Published != 3 {
		t.Fatalf("published = %d, want 3", stats.Published)
	}
	if stats.OldestSeq != 2 || stats.NewestSeq != 3 {
		t.Fatalf("oldest/newest = [%d %d], want [2 3]", stats.OldestSeq, stats.NewestSeq)
	}
}

func TestBusSubscribeReplayAndLive(t *testing.T) {
	t.Helper()
	b := New(8)

	b.Publish(Event{Kind: "one"})
	b.Publish(Event{Kind: "two"})
	b.Publish(Event{Kind: "three"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := b.Subscribe(ctx, 2)

	gotReplay := readEvent(t, ch)
	if gotReplay.Seq != 3 {
		t.Fatalf("replay seq = %d, want 3", gotReplay.Seq)
	}

	seq4 := b.Publish(Event{Kind: "four"})
	gotLive := readEvent(t, ch)
	if gotLive.Seq != seq4 {
		t.Fatalf("live seq = %d, want %d", gotLive.Seq, seq4)
	}
}

func TestBusAppendCause(t *testing.T) {
	t.Helper()
	b := New(8)
	parent := b.Publish(Event{Kind: "parent"})
	child := b.Publish(Event{Kind: "child"})

	if ok := b.AppendCause(parent, child); !ok {
		t.Fatal("AppendCause returned false, want true")
	}
	if ok := b.AppendCause(parent, child); !ok {
		t.Fatal("second AppendCause returned false, want true")
	}

	snap := b.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}
	if len(snap[1].CausesSeq) != 1 || snap[1].CausesSeq[0] != parent {
		t.Fatalf("causes = %v, want [%d]", snap[1].CausesSeq, parent)
	}
}

func TestBusSubscriberDropIncrementsStats(t *testing.T) {
	t.Helper()
	b := New(32)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = b.Subscribe(ctx, 0)

	for i := 0; i < 200; i++ {
		b.Publish(Event{Kind: "burst"})
	}

	stats := b.Stats()
	if stats.Dropped == 0 {
		t.Fatal("dropped = 0, want > 0")
	}
}

func readEvent(t *testing.T, ch <-chan Event) Event {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
		return Event{}
	}
}
