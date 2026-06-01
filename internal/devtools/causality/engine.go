// Package causality derives and traverses event lineage for DevTools.
package causality

import (
	"context"

	"github.com/tgpski/leather/internal/devtools/bus"
)

// TraceOptions controls lineage expansion depth and breadth.
type TraceOptions struct {
	Depth   int
	Breadth int
}

// Node is a single traced event node.
type Node struct {
	Event bus.Event `json:"event"`
	Cold  bool      `json:"cold"`
}

// Result contains a traced causal neighborhood around a root event.
type Result struct {
	RootSeq uint64 `json:"root_seq"`
	Nodes   []Node `json:"nodes"`
}

// Engine provides causal edge linking and tracing.
type Engine struct{}

// NewEngine constructs an Engine.
func NewEngine() *Engine {
	return &Engine{}
}

// Annotate returns ev unchanged in Phase D; reserved for richer derivation rules.
func (e *Engine) Annotate(ev bus.Event) bus.Event {
	return ev
}

// LinkForward links parent as a cause for child on the bus.
func (e *Engine) LinkForward(b *bus.Bus, parent, child uint64) bool {
	if b == nil {
		return false
	}
	return b.AppendCause(parent, child)
}

// Trace walks parent/cause and child edges from root within opts limits.
func (e *Engine) Trace(ctx context.Context, b *bus.Bus, root uint64, opts TraceOptions) Result {
	if b == nil || root == 0 {
		return Result{RootSeq: root}
	}

	depth := opts.Depth
	if depth <= 0 {
		depth = 6
	}
	breadth := opts.Breadth
	if breadth <= 0 {
		breadth = 256
	}

	events := b.Snapshot()
	if len(events) == 0 {
		return Result{RootSeq: root}
	}

	bySeq := make(map[uint64]bus.Event, len(events))
	children := make(map[uint64][]uint64, len(events))
	for _, ev := range events {
		bySeq[ev.Seq] = ev
		if ev.ParentSeq != 0 {
			children[ev.ParentSeq] = append(children[ev.ParentSeq], ev.Seq)
		}
	}
	if _, ok := bySeq[root]; !ok {
		return Result{RootSeq: root}
	}

	seen := map[uint64]bool{root: true}
	frontier := []uint64{root}

	for i := 0; i < depth && len(frontier) > 0; i++ {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return buildResult(root, events, seen)
			default:
			}
		}
		next := make([]uint64, 0)
		for _, seq := range frontier {
			ev := bySeq[seq]

			if ev.ParentSeq != 0 && !seen[ev.ParentSeq] {
				seen[ev.ParentSeq] = true
				next = append(next, ev.ParentSeq)
			}
			for _, c := range ev.CausesSeq {
				if c != 0 && !seen[c] {
					seen[c] = true
					next = append(next, c)
				}
			}
			for _, child := range children[seq] {
				if !seen[child] {
					seen[child] = true
					next = append(next, child)
				}
			}
			if len(next) >= breadth {
				break
			}
		}
		if len(next) > breadth {
			next = next[:breadth]
		}
		frontier = next
	}

	return buildResult(root, events, seen)
}

func buildResult(root uint64, ordered []bus.Event, seen map[uint64]bool) Result {
	out := make([]Node, 0, len(seen))
	for _, ev := range ordered {
		if seen[ev.Seq] {
			out = append(out, Node{Event: ev, Cold: false})
		}
	}
	return Result{RootSeq: root, Nodes: out}
}
