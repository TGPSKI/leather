package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/devtools/bus"
	"github.com/tgpski/leather/internal/devtools/causality"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/worker"
)

const devtoolsVersion = "1"

// DevtoolsHandlerDeps provides data dependencies for DevTools handlers.
type DevtoolsHandlerDeps struct {
	Bus          *bus.Bus
	Causality    *causality.Engine
	QueueMgr     *queue.Manager
	GetWorkerSup func() *worker.Supervisor
	StartedAt    time.Time
	Version      string
	Commit       string
}

// NewDevtoolsHandler builds all /api/devtools routes.
func NewDevtoolsHandler(deps DevtoolsHandlerDeps) http.Handler {
	if deps.Causality == nil {
		deps.Causality = causality.NewEngine()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/devtools/snapshot", func(w http.ResponseWriter, r *http.Request) {
		setDevtoolsHeaders(w)
		recent := parsePositiveInt(r.URL.Query().Get("recent"), 100)
		if recent > 1000 {
			recent = 1000
		}
		events := deps.Bus.Snapshot()
		if len(events) > recent {
			events = events[len(events)-recent:]
		}

		body := map[string]any{
			"version":       deps.Version,
			"commit":        deps.Commit,
			"started_at":    deps.StartedAt.Unix(),
			"captured_at":   time.Now().Unix(),
			"bus_stats":     deps.Bus.Stats(),
			"recent_events": events,
		}
		writeJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("/api/devtools/inspect/", func(w http.ResponseWriter, r *http.Request) {
		setDevtoolsHeaders(w)
		rest := strings.TrimPrefix(r.URL.Path, "/api/devtools/inspect/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			writeJSONError(w, "expected /api/devtools/inspect/{kind}/{id}", http.StatusBadRequest)
			return
		}
		kind := parts[0]
		id := parts[1]

		payload, found := inspectEntity(deps, kind, id)
		if !found {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "not_found", "message": "entity not found"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "id": id, "payload": payload})
	})

	mux.HandleFunc("/api/devtools/trace/", func(w http.ResponseWriter, r *http.Request) {
		setDevtoolsHeaders(w)
		seqStr := strings.TrimPrefix(r.URL.Path, "/api/devtools/trace/")
		seq, err := strconv.ParseUint(seqStr, 10, 64)
		if err != nil || seq == 0 {
			writeJSONError(w, "invalid seq", http.StatusBadRequest)
			return
		}
		depth := parsePositiveInt(r.URL.Query().Get("depth"), 6)
		if depth > 64 {
			depth = 64
		}
		result := deps.Causality.Trace(context.Background(), deps.Bus, seq, causality.TraceOptions{Depth: depth, Breadth: 512})
		if len(result.Nodes) == 0 {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]string{"code": "not_found", "message": "seq not present in ring"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"root_seq": result.RootSeq, "depth": depth, "nodes": result.Nodes})
	})

	mux.Handle("/api/devtools/events", makeDevtoolsSSEHandler(deps.Bus))
	return mux
}

func setDevtoolsHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Leather-Devtools-Version", devtoolsVersion)
	w.Header().Set("Content-Type", "application/json")
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func inspectEntity(deps DevtoolsHandlerDeps, kind, id string) (any, bool) {
	switch kind {
	case "event":
		seq, err := strconv.ParseUint(id, 10, 64)
		if err != nil {
			return nil, false
		}
		for _, ev := range deps.Bus.Snapshot() {
			if ev.Seq == seq {
				return ev, true
			}
		}
		return nil, false
	case "queue":
		if deps.QueueMgr == nil {
			return nil, false
		}
		q, err := deps.QueueMgr.Get(id)
		if err != nil {
			return nil, false
		}
		body := map[string]any{"name": id, "len": q.Len()}
		if head, ok := q.Peek(); ok {
			body["head"] = head
		}
		return body, true
	case "worker":
		if deps.GetWorkerSup == nil {
			return nil, false
		}
		sup := deps.GetWorkerSup()
		if sup == nil {
			return nil, false
		}
		for _, ws := range sup.Workers() {
			if ws.Name == id {
				return ws, true
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

func writeSSEFrame(w http.ResponseWriter, event string, id uint64, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if id > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", sanitizeSSEField(event)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", encoded); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// sanitizeSSEField strips CR and LF from an SSE field value so that
// callers cannot inject extra SSE fields (data:, id:, event:) by smuggling
// newlines through an event Kind or similar caller-controlled string.
func sanitizeSSEField(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
