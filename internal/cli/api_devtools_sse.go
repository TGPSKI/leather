package cli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/devtools/bus"
)

const devtoolsSSEHeartbeatInterval = 15 * time.Second

func makeDevtoolsSSEHandler(eventBus *bus.Bus) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Leather-Devtools-Version", devtoolsVersion)

		lastID := parseLastEventID(r)
		stats := eventBus.Stats()
		if lastID > 0 && stats.OldestSeq > 0 && lastID < stats.OldestSeq {
			_ = writeSSEFrame(w, "gap", 0, map[string]any{"from": lastID, "oldest": stats.OldestSeq, "newest": stats.NewestSeq})
		}

		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		ch := eventBus.Subscribe(ctx, lastID)

		heartbeat := time.NewTicker(devtoolsSSEHeartbeatInterval)
		defer heartbeat.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-heartbeat.C:
				if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
					return
				}
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			case ev, ok := <-ch:
				if !ok {
					return
				}
				if err := writeSSEFrame(w, ev.Kind, ev.Seq, ev); err != nil {
					return
				}
			}
		}
	})
}

func parseLastEventID(r *http.Request) uint64 {
	if raw := strings.TrimSpace(r.Header.Get("Last-Event-ID")); raw != "" {
		if id, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return id
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		if id, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return id
		}
	}
	return 0
}
