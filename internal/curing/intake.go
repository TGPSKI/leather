package curing

import (
	"fmt"
	"sort"
	"strings"

	"github.com/TGPSKI/leather/internal/model"
)

// Routing is the resolved destination of one intake: which curing owns the work
// and which queue carries it. A zero Routing means the intake named no
// destination and no route matched — the hide is stored and nothing is enqueued.
type Routing struct {
	// Curing is the curing that will process the item, "" when no curing owns
	// the destination queue.
	Curing string
	// Queue is the static destination queue, "" when QueuePattern applies.
	Queue string
	// QueuePattern is a single-use queue template; expand it with QueueFor once
	// the hide ID is known.
	QueuePattern string
}

// Routed reports whether the intake resolved to a destination to enqueue on.
func (r Routing) Routed() bool { return r.Queue != "" || r.QueuePattern != "" }

// QueueFor returns the destination queue name for a stored hide, expanding
// {{hide_id}} in a single-use queue pattern.
func (r Routing) QueueFor(hideID string) string {
	if r.QueuePattern != "" {
		return strings.ReplaceAll(r.QueuePattern, "{{hide_id}}", hideID)
	}
	return r.Queue
}

// RoutingRequest carries one intake's routing parameters plus the tannery state
// needed to resolve them.
type RoutingRequest struct {
	// Curing is the explicitly named curing (`curing=` / `--curing`).
	Curing string
	// Queue is the explicitly named queue (`queue=` / `--queue`).
	Queue string
	// Source and EventType feed the route table when nothing is named explicitly.
	Source    string
	EventType string
	// Router holds the tannery's route table; may be nil.
	Router *Router
	// Defs are the loaded curing definitions.
	Defs []model.CuringDefinition
	// Queues are the tannery's declared queues.
	Queues map[string]model.QueueConcurrencyConfig
}

// ResolveRouting applies leather's one intake routing rule, so `POST /intake`
// and `leather ingest` agree on what a given parameter set means.
//
// The rule, in order:
//
//   - A named curing routes to its declared queue: the curing definition
//     already carries it, so `curing=` alone is enough.
//   - A named queue routes to that queue, and picks up the name of the curing
//     that consumes it when exactly one does.
//   - With neither named, the route table decides from source and event type.
//   - With neither named and no route matching, the intake is hide-only:
//     nothing is enqueued and the zero Routing says so.
//
// A parameter set that names a destination which cannot route is an error, not
// a success with the routing lines missing from the output — an unrouted
// ingest is indistinguishable downstream from a working pipeline that has
// nothing to do (issue #75).
func ResolveRouting(req RoutingRequest) (Routing, error) {
	switch {
	case req.Curing != "":
		var def model.CuringDefinition
		found := false
		for _, d := range req.Defs {
			if d.Name == req.Curing {
				def, found = d, true
				break
			}
		}
		if !found {
			return Routing{}, fmt.Errorf("curing/ResolveRouting: unknown curing %q (loaded: %s)",
				req.Curing, nameList(curingNames(req.Defs)))
		}
		if req.Queue != "" {
			if err := checkQueue(req, req.Queue); err != nil {
				return Routing{}, err
			}
			return Routing{Curing: def.Name, Queue: req.Queue}, nil
		}
		if def.Queue != "" {
			return Routing{Curing: def.Name, Queue: def.Queue}, nil
		}
		return Routing{}, fmt.Errorf(
			"curing/ResolveRouting: curing %q consumes single-use queues (queue_prefix %q) and declares no static queue: name the queue explicitly",
			def.Name, def.QueuePrefix)

	case req.Queue != "":
		if err := checkQueue(req, req.Queue); err != nil {
			return Routing{}, err
		}
		// Carry the owning curing when exactly one consumes this queue, so an
		// item enqueued by name is indistinguishable from one enqueued by route.
		owner := ""
		matches := 0
		for _, d := range req.Defs {
			if d.Queue == req.Queue {
				owner = d.Name
				matches++
			}
		}
		if matches != 1 {
			owner = ""
		}
		return Routing{Curing: owner, Queue: req.Queue}, nil

	default:
		if req.Router != nil {
			if route, ok := req.Router.Match(req.Source, req.EventType); ok {
				return Routing{Curing: route.Curing, Queue: route.Queue, QueuePattern: route.QueuePattern}, nil
			}
		}
		return Routing{}, nil
	}
}

// checkQueue reports whether name is a queue some worker will actually poll:
// either declared under the tannery's queues:, or inside the single-use
// namespace of a curing's queue_prefix. Enqueuing anywhere else creates a file
// nothing reads, which looks exactly like a pipeline with no work to do.
func checkQueue(req RoutingRequest, name string) error {
	if _, ok := req.Queues[name]; ok {
		return nil
	}
	for _, d := range req.Defs {
		if d.QueuePrefix != "" && strings.HasPrefix(name, d.QueuePrefix) {
			return nil
		}
	}
	return fmt.Errorf("curing/ResolveRouting: queue %q is not declared in the tannery (declared: %s)",
		name, nameList(queueNames(req.Queues)))
}

func curingNames(defs []model.CuringDefinition) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}

func queueNames(queues map[string]model.QueueConcurrencyConfig) []string {
	out := make([]string, 0, len(queues))
	for name := range queues {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func nameList(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}
