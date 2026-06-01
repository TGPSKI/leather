package cli

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tgpski/leather/internal/artifact"
	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/curing"
	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/runner"
	"github.com/tgpski/leather/internal/session"
)

// tanneryDeps groups the tannery-specific runtime state used by handlers.
// Fields are populated by initTannery and are safe for concurrent read afterward.
type tanneryDeps struct {
	hideStore    *hide.Store
	artStore     *artifact.Store
	curingDefs   []model.CuringDefinition
	curingRouter *curing.Router
	curingSupv   *curing.Supervisor
	tannCfg      config.TanneryConfig
	lockFile     *os.File // held open for the lifetime of the process (A1 lock)
}

// initTannery loads tannery config and constructs all subsystems.
// Returns (nil, nil) when tanneryFile is empty — tannery mode is disabled.
// Returns a non-nil error when tanneryFile is set but loading fails.
// ctx is forwarded to curing.Supervisor.Start so workers honour cancellation.
func initTannery(ctx context.Context, tanneryFile string, deps *apiDeps) (*tanneryDeps, error) {
	if tanneryFile == "" {
		return nil, nil
	}

	tannCfg, err := config.LoadTannery(tanneryFile)
	if err != nil {
		return nil, fmt.Errorf("cli/initTannery: load config: %w", err)
	}

	// A1: acquire single-process flock on the hide directory.
	lockPath := filepath.Join(tannCfg.HideDir, "leather.lock")
	lf, err := acquireProcessLock(lockPath)
	if err != nil {
		return nil, fmt.Errorf("cli/initTannery: another tannery instance is running on this hide_dir: %w", err)
	}

	// On any error between here and the successful return, release the lock
	// so a retry can acquire it (T1.15: Gemini "Tannery partial-init leak").
	success := false
	defer func() {
		if !success {
			releaseProcessLock(lf)
		}
	}()

	hideStore := hide.NewStore(tannCfg.HideDir)
	artStore := artifact.NewStore(tannCfg.ArtifactDir)

	curingDefs, err := curing.LoadDir(tannCfg.CuringDir)
	if err != nil {
		return nil, fmt.Errorf("cli/initTannery: load curing defs: %w", err)
	}
	if len(curingDefs) == 0 {
		deps.log.Warn("api_tannery/initTannery: curing_dir loaded zero definitions", "dir", tannCfg.CuringDir)
	}

	// Cross-validate routes against loaded definitions and declared queues (Open Q6).
	if err := config.ValidateTannery(tannCfg, curingDefs); err != nil {
		return nil, err
	}

	curingRouter := curing.NewRouter(tannCfg.Routes)

	runnerDeps := &curing.RunnerDeps{
		Client:         session.NewHTTPClient(deps.cfg.LLMEndpoint, deps.cfg.LLMAPIKey, deps.cfg.LLMTimeout),
		ToolReg:        deps.toolReg,
		Log:            deps.log,
		MaxToolRounds:  deps.cfg.MaxToolRounds,
		Cache:          deps.agentCache,
		QueueMgr:       deps.queueMgr,
		Notifiers:      deps.notifiers,
		MCPReg:         deps.mcpReg,
		Budget:         resolveTokenBudget(deps.cfg, model.Agent{}),
		DebugContextFn: func(runner.ContextSnapshot) {},
		OnComplete:     deps.onCuringJobDone,
		EventFn:        deps.onCuringEvent,
	}
	// Persist run records for curing-driven agents (both successes and failures)
	// when PersistRuns is enabled. Without this, DLQ-bound agents that never
	// reach OnComplete (e.g. example 08 always-timeout) leave .state/runs/ empty.
	if deps.cfg.PersistRuns {
		dir := deps.runHistDir
		maxBytes := deps.cfg.RunMaxBytes
		log := deps.log
		runnerDeps.OnRunRecord = func(_ model.Agent, rec model.RunRecord, _ error) {
			if err := persistRunRecord(dir, rec, maxBytes); err != nil {
				log.Warn("persist run record failed", "agent", rec.AgentName, "error", err)
			}
		}
	}
	// Wire real-time runner progress events into the DevTools bus so the trace
	// view populates live for curing-based agents (same as scheduled-agent path).
	if deps.devtoolsSrc != nil {
		devtoolsSrc := deps.devtoolsSrc
		runnerDeps.ProgressWithAgent = func(curingName, agentName string, ev runner.ProgressEvent) {
			devtoolsSrc.PublishRunner(curingName, agentName, ev)
		}
	}
	if !deps.cfg.ShowContext {
		runnerDeps.DebugContextFn = nil
	}

	sup, err := curing.NewSupervisor(
		curingDefs, agentsByName(deps.agents), tannCfg.Queues,
		hideStore, artStore, runnerDeps,
		deps.queueMgr, curingRouter, deps.log,
	)
	if err != nil {
		return nil, fmt.Errorf("cli/initTannery: new supervisor: %w", err)
	}
	sup.Start(ctx)

	success = true
	return &tanneryDeps{
		hideStore:    hideStore,
		artStore:     artStore,
		curingDefs:   curingDefs,
		curingRouter: curingRouter,
		curingSupv:   sup,
		tannCfg:      tannCfg,
		lockFile:     lf,
	}, nil
}

// printStartupHierarchy emits a tannery → curing → agent tree at startup so
// the operator can verify wiring at a glance. The hierarchy is:
//
//	tannery <file>   (N curings, M routes, K queues)
//	  - curing <name>   queue=<q>
//	      · agent  <name>   <trigger>
//	  - route <name>    <source>[/<event>] -> <curing> via <queue>
//	  - queue <name>    concurrency=<c>  max_depth=<d>
//	agents (N standalone)        # only when there are agents not used by any curing
//	  - <name>  <trigger>
//
// When td is nil the tannery row is omitted and every loaded agent appears as
// a standalone entry. Trigger is one of: schedule=<cron>, queue=<name>
// (consumer), disabled, or "(no schedule / queue — manual run only)".
func printStartupHierarchy(w io.Writer, cfg model.Config, agents []model.Agent, td *tanneryDeps) {
	// Build a set of agents claimed by a curing so we can list orphan agents
	// separately below the tannery block.
	claimed := make(map[string]bool)
	var curings []model.CuringDefinition
	var routes []model.TanneryRoute
	var queues map[string]model.QueueConcurrencyConfig
	if td != nil {
		curings = td.curingDefs
		routes = td.tannCfg.Routes
		queues = td.tannCfg.Queues
		for _, c := range curings {
			claimed[c.Agent] = true
		}
	}

	// agentByName index for quick trigger lookup under a curing.
	agentByName := make(map[string]model.Agent, len(agents))
	for _, a := range agents {
		agentByName[a.Name] = a
	}

	if td != nil {
		summary := fmt.Sprintf("%s — %d curings, %d routes, %d queues",
			cfg.TanneryFile, len(curings), len(routes), len(queues))
		if cfg.Pretty {
			lines := []string{summary}
			for _, c := range curings {
				q := c.Queue
				if c.QueuePrefix != "" {
					q = c.QueuePrefix + "* (single-use)"
				}
				lines = append(lines, fmt.Sprintf("- curing %s  %s",
					c.Name, dim(fmt.Sprintf("queue=%s", q))))
				if a, ok := agentByName[c.Agent]; ok {
					lines = append(lines, fmt.Sprintf("    · agent  %s  %s",
						c.Agent, dim(agentTrigger(a))))
				} else {
					lines = append(lines, fmt.Sprintf("    · agent  %s  %s",
						c.Agent, dim("(not loaded)")))
				}
			}
			for _, r := range routes {
				q := r.Queue
				if r.QueuePattern != "" {
					q = r.QueuePattern
				}
				match := r.Match.Source
				if r.Match.EventType != "" {
					match += "/" + r.Match.EventType
				}
				lines = append(lines, fmt.Sprintf("- route  %s  %s",
					r.Name, dim(fmt.Sprintf("%s -> %s via %s", match, r.Curing, q))))
			}
			for name, qc := range queues {
				lines = append(lines, fmt.Sprintf("- queue  %s  %s",
					name, dim(fmt.Sprintf("concurrency=%d  max_depth=%d", qc.Concurrency, qc.MaxDepth))))
			}
			if len(curings) == 0 {
				lines = append(lines, dim("no curing definitions loaded — tannery will idle"))
			}
			prettyWriteEntry(w, time.Now().Format("15:04:05"),
				boldCyan(prettyPadLabel("tannery")), lines)
		} else {
			fmt.Fprintln(w, "leather: tannery "+summary)
			for _, c := range curings {
				q := c.Queue
				if c.QueuePrefix != "" {
					q = c.QueuePrefix + "* (single-use)"
				}
				fmt.Fprintf(w, "  - curing %s  queue=%s\n", c.Name, q)
				if a, ok := agentByName[c.Agent]; ok {
					fmt.Fprintf(w, "      · agent  %s  %s\n", c.Agent, agentTrigger(a))
				} else {
					fmt.Fprintf(w, "      · agent  %s  (not loaded)\n", c.Agent)
				}
			}
			for _, r := range routes {
				q := r.Queue
				if r.QueuePattern != "" {
					q = r.QueuePattern
				}
				match := r.Match.Source
				if r.Match.EventType != "" {
					match += "/" + r.Match.EventType
				}
				fmt.Fprintf(w, "  - route  %s  %s -> %s via %s\n", r.Name, match, r.Curing, q)
			}
			for name, qc := range queues {
				fmt.Fprintf(w, "  - queue  %s  concurrency=%d  max_depth=%d\n", name, qc.Concurrency, qc.MaxDepth)
			}
		}
	}

	// Standalone agents: those not bound to any curing. When there is no
	// tannery at all, every agent is standalone.
	var standalone []model.Agent
	for _, a := range agents {
		if !claimed[a.Name] {
			standalone = append(standalone, a)
		}
	}
	if len(standalone) == 0 {
		return
	}
	enabled, disabled := 0, 0
	for _, a := range standalone {
		if a.Enabled {
			enabled++
		} else {
			disabled++
		}
	}
	header := fmt.Sprintf("%d standalone agents (%d enabled, %d disabled)",
		len(standalone), enabled, disabled)
	if cfg.Pretty {
		lines := []string{header}
		for _, a := range standalone {
			lines = append(lines, fmt.Sprintf("- %s  %s", a.Name, dim(agentTrigger(a))))
		}
		if enabled == 0 {
			lines = append(lines, dim("no agents enabled — scheduler will idle"))
		}
		prettyWriteEntry(w, time.Now().Format("15:04:05"),
			boldCyan(prettyPadLabel("agents")), lines)
	} else {
		fmt.Fprintf(w, "leather: %s\n", header)
		for _, a := range standalone {
			fmt.Fprintf(w, "  - %s  %s\n", a.Name, agentTrigger(a))
		}
		if enabled == 0 {
			fmt.Fprintln(w, "leather: no agents enabled — scheduler will idle")
		}
	}
}

// agentTrigger returns a short human-readable description of what fires an
// agent: a cron schedule, a queue consumer binding, "disabled", or a
// fallback indicating the agent only runs on manual invocation.
func agentTrigger(a model.Agent) string {
	switch {
	case !a.Enabled:
		return "disabled (enabled: false)"
	case a.QueueInput != "":
		return fmt.Sprintf("queue=%s (consumer)", a.QueueInput)
	case a.Schedule != "":
		return fmt.Sprintf("schedule=%s", a.Schedule)
	default:
		return "(no schedule / queue — manual run only)"
	}
}

// acquireProcessLock creates path (and its parent dir) and acquires an exclusive
// non-blocking flock. The returned *os.File must remain open for the lock to be
// held; the OS releases it automatically on process exit.
func acquireProcessLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("acquireProcessLock: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("acquireProcessLock: open: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("acquireProcessLock: flock %s: %w", path, err)
	}
	return f, nil
}

// releaseProcessLock releases the flock and closes the lock file.
// Exported for use in tests that need to release locks explicitly.
func releaseProcessLock(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}

// registerTanneryHandlers installs all /webhooks/*, /hides, /artifacts,
// /curings, and /intake handlers on mux.
// No-op when td is nil (tannery mode disabled).
func registerTanneryHandlers(mux *http.ServeMux, td *tanneryDeps, deps *apiDeps) {
	if td == nil {
		return
	}
	seenPaths := make(map[string]string, len(td.tannCfg.Webhooks))
	for _, wh := range td.tannCfg.Webhooks {
		wh := wh // capture loop var
		path := wh.Path
		if path == "" {
			path = "/webhooks/" + wh.Name
		}
		if prev, ok := seenPaths[path]; ok {
			deps.log.Error("api_tannery/registerTanneryHandlers: duplicate webhook path; later webhook ignored",
				"path", path, "prev_webhook", prev, "ignored_webhook", wh.Name)
			continue
		}
		if wh.Secret == "" {
			deps.log.Warn("api_tannery/registerTanneryHandlers: webhook has no secret; signature validation disabled",
				"webhook", wh.Name, "path", path)
		}
		seenPaths[path] = wh.Name
		mux.HandleFunc(path, makeWebhookHandler(wh, td, deps))
	}
	mux.HandleFunc("/hides", handleHidesCollection(td))
	mux.HandleFunc("/hides/", dispatchHide(td))
	mux.HandleFunc("/artifacts", handleArtifactsCollection(td))
	mux.HandleFunc("/artifacts/", dispatchArtifact(td))
	mux.HandleFunc("/curings", handleCurings(td))
	mux.HandleFunc("/intake", handleIntake(td, deps))
}

// defaultMaxBodyBytes is the default per-webhook body size cap (5 MiB).
const defaultMaxBodyBytes int64 = 5 * 1024 * 1024

// makeWebhookHandler returns an http.HandlerFunc for the named webhook.
// Flow: body read → HMAC validation → router match → backpressure → hide write → enqueue → 202.
func makeWebhookHandler(wh model.WebhookConfig, td *tanneryDeps, deps *apiDeps) http.HandlerFunc {
	maxBody := wh.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBodyBytes
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		if err != nil {
			http.Error(w, `{"error":"read body"}`, http.StatusInternalServerError)
			return
		}
		if int64(len(body)) > maxBody {
			http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
			return
		}
		if !validateWebhookSignature(r, body, wh.Secret) {
			http.Error(w, `{"error":"invalid signature"}`, http.StatusUnauthorized)
			return
		}
		eventType := r.Header.Get("X-GitHub-Event")
		if eventType == "" {
			eventType = r.URL.Query().Get("event_type")
		}
		// Fan-out: match ALL routes for this source+eventType.
		routes := td.curingRouter.MatchAll(wh.Source, eventType)
		if len(routes) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// A2 backpressure: check all destination queues before any disk write.
		// Routes with queue_pattern create single-use queues dynamically and are
		// not backpressure-checked (no static queue config exists for them).
		if deps.queueMgr != nil {
			for _, route := range routes {
				if route.QueuePattern != "" {
					continue // single-use queue; skip backpressure check
				}
				if queueCfg, exists := td.tannCfg.Queues[route.Queue]; exists &&
					queueCfg.MaxDepth > 0 && deps.queueMgr.Depth(route.Queue) >= queueCfg.MaxDepth {
					w.Header().Set("Retry-After", "30")
					writeJSONError(w, "queue full: "+route.Queue, http.StatusServiceUnavailable)
					return
				}
			}
		}
		// Write one hide shared across all matched routes. All fan-out items carry
		// the same hide_id so downstream correlated-collect curings can group by it.
		meta := map[string]string{"event_type": eventType, "webhook": wh.Name}
		entry, err := td.hideStore.Put(routes[0].HideKind, wh.Source, body, meta)
		if err != nil {
			deps.log.Error("webhook: hide put failed", "webhook", wh.Name, "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		// Enqueue one item per matched route; all items reference the same hide.
		type routeResult struct {
			Curing string `json:"curing"`
			Queue  string `json:"queue"`
		}
		// Emit one webhook event for this HTTP request before the per-route fan-out.
		// devtoolsSrc publishes the raw intake; onCuringEvent publishes per-route
		// queue.enqueue events inside the loop via PublishTannery.
		if deps.devtoolsSrc != nil {
			deps.devtoolsSrc.PublishHTTP("webhook.received", map[string]any{
				"webhook_name": wh.Name,
				"path":         wh.Path,
				"hide_id":      entry.ID,
				"hide_kind":    entry.Kind,
				"route_count":  len(routes),
			})
		}
		if deps.onCuringEvent != nil {
			deps.onCuringEvent(curing.TanneryEvent{
				Kind:        "webhook",
				Curing:      entry.Kind,
				HideID:      entry.ID,
				HideKind:    entry.Kind,
				Source:      wh.Path,
				WebhookName: wh.Name,
			})
		}
		results := make([]routeResult, 0, len(routes))
		// Extract a delivery ID from the standard GitHub header for idempotent enqueues.
		// When present, item IDs are deterministic so retried deliveries are de-duplicated.
		deliveryID := r.Header.Get("X-GitHub-Delivery")
		enqueueErr := false
		for i, route := range routes {
			// Resolve the destination queue name. Routes with queue_pattern expand
			// {{hide_id}} using the shared hide ID to create a single-use queue.
			queueName := route.Queue
			if route.QueuePattern != "" {
				queueName = strings.ReplaceAll(route.QueuePattern, "{{hide_id}}", entry.ID)
			}
			// Build a deterministic item ID when a delivery ID is available so that
			// retried webhook deliveries are idempotent (EnqueueIfAbsent skips duplicates).
			itemID := queue.GenerateItemID()
			if deliveryID != "" {
				itemID = fmt.Sprintf("%s-%d", deliveryID, i)
			}
			item := model.QueueItem{
				ID:            itemID,
				CuringName:    route.Curing,
				HideID:        entry.ID,
				HideKind:      entry.Kind,
				CorrelationID: entry.ID, // shared across all fan-out items for this event
				EnqueuedAt:    time.Now().Unix(),
				Payload:       map[string]any{"hide_id": entry.ID, "curing": route.Curing},
			}
			if deps.queueMgr != nil {
				var err error
				if deliveryID != "" {
					// Idempotent path: skip if already enqueued (e.g. retried delivery).
					_, err = deps.queueMgr.EnqueueIfAbsent(queueName, item)
				} else {
					err = deps.queueMgr.Enqueue(queueName, item)
				}
				if err != nil {
					deps.log.Error("webhook: enqueue failed", "queue", queueName, "error", err)
					enqueueErr = true
					break
				}
			}
			results = append(results, routeResult{Curing: route.Curing, Queue: queueName})
			// Per-route enqueue event — published via onCuringEvent→PublishTannery below.
			// devtoolsSrc does NOT publish a separate per-route event here; that would
			// duplicate the queue.enqueue already emitted by PublishTannery.
			if deps.onCuringEvent != nil {
				deps.onCuringEvent(curing.TanneryEvent{
					Kind:      "enqueue",
					Curing:    route.Curing,
					DestQueue: queueName,
					HideID:    entry.ID,
					HideKind:  entry.Kind,
					ItemID:    item.ID,
				})
			}
		}
		if enqueueErr {
			// Roll back the hide on total enqueue failure to prevent orphaned hides.
			_ = td.hideStore.Delete(entry.ID)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		// Backward-compat: top-level curing/queue fields reflect the first route.
		writeJSON(w, http.StatusAccepted, map[string]any{
			"hide_id": entry.ID,
			"curing":  results[0].Curing,
			"queue":   results[0].Queue,
			"routes":  results,
		})
	}
}

// validateWebhookSignature returns true when the X-Hub-Signature-256 header matches
// the HMAC-SHA256 of body using secret. When secret is empty, always returns false
// (fail-closed: unconfigured webhooks are not accepted without a valid signature).
func validateWebhookSignature(r *http.Request, body []byte, secret string) bool {
	if secret == "" {
		return false
	}
	sig := strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=")
	if sig == "" {
		return false
	}
	want, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), want)
}

// handleHidesCollection handles GET /hides — returns a JSON list of StoreEntry.
func handleHidesCollection(td *tanneryDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		entries, err := td.hideStore.List()
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		if entries == nil {
			entries = []hide.StoreEntry{}
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

// parseHidePath parses /hides/<id>[/cuts/<page>] into (id, sub, page).
// Returns ("", "", 0) when no ID is present.
func parseHidePath(p string) (id, sub string, page int) {
	p = strings.TrimPrefix(p, "/hides/")
	parts := strings.SplitN(p, "/", 3)
	id = parts[0]
	if id == "" {
		return "", "", 0
	}
	if len(parts) >= 2 {
		sub = parts[1]
	}
	if len(parts) >= 3 && sub == "cuts" {
		page, _ = strconv.Atoi(parts[2])
	}
	return id, sub, page
}

// dispatchHide handles /hides/{id} and /hides/{id}/cuts/{page}.
func dispatchHide(td *tanneryDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, sub, page := parseHidePath(r.URL.Path)
		if id == "" {
			http.NotFound(w, r)
			return
		}
		switch {
		case sub == "" && r.Method == http.MethodGet:
			entry, _, err := td.hideStore.Get(id)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					http.NotFound(w, r)
					return
				}
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, entry)
		case sub == "" && r.Method == http.MethodDelete:
			if err := td.hideStore.Delete(id); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					http.NotFound(w, r)
					return
				}
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case sub == "cuts":
			cut, err := td.hideStore.Cut(id, page, 0)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					http.NotFound(w, r)
					return
				}
				http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, cut)
		default:
			http.NotFound(w, r)
		}
	}
}

// handleArtifactsCollection handles GET /artifacts[?curing=<name>].
func handleArtifactsCollection(td *tanneryDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var arts []model.Artifact
		var err error
		if name := r.URL.Query().Get("curing"); name != "" {
			arts, err = td.artStore.ListByCuring(name)
		} else {
			arts, err = td.artStore.List()
		}
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		if arts == nil {
			arts = []model.Artifact{}
		}
		writeJSON(w, http.StatusOK, arts)
	}
}

// dispatchArtifact handles GET /artifacts/{id}.
func dispatchArtifact(td *tanneryDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/artifacts/")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		art, err := td.artStore.Get(id)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, art)
	}
}

// handleCurings handles GET /curings — returns the loaded curing definitions.
func handleCurings(td *tanneryDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		defs := td.curingDefs
		if defs == nil {
			defs = []model.CuringDefinition{}
		}
		writeJSON(w, http.StatusOK, defs)
	}
}

// handleIntake handles POST /intake — directly ingests raw bytes as a hide.
func handleIntake(td *tanneryDeps, deps *apiDeps) http.HandlerFunc {
	const maxIntakeBytes int64 = 50 * 1024 * 1024 // 50 MiB
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		kind := r.URL.Query().Get("kind")
		if kind == "" {
			writeJSONError(w, "missing required query param: kind", http.StatusBadRequest)
			return
		}
		source := r.URL.Query().Get("source")
		if source == "" {
			source = "api"
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxIntakeBytes+1))
		if err != nil {
			http.Error(w, `{"error":"read body"}`, http.StatusInternalServerError)
			return
		}
		if int64(len(body)) > maxIntakeBytes {
			http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
			return
		}
		// Resolve queue: explicit curing+queue params, then router, then no routing.
		curingName := r.URL.Query().Get("curing")
		queueName := r.URL.Query().Get("queue")
		if curingName == "" {
			eventType := r.URL.Query().Get("event_type")
			if route, ok := td.curingRouter.Match(source, eventType); ok {
				curingName = route.Curing
				queueName = route.Queue
			}
		}
		// A2 backpressure (when routing resolved a queue).
		if queueName != "" && deps.queueMgr != nil {
			if queueCfg, exists := td.tannCfg.Queues[queueName]; exists &&
				queueCfg.MaxDepth > 0 && deps.queueMgr.Depth(queueName) >= queueCfg.MaxDepth {
				w.Header().Set("Retry-After", "30")
				writeJSONError(w, "queue full", http.StatusServiceUnavailable)
				return // hide NOT stored — caller retries
			}
		}
		entry, err := td.hideStore.Put(kind, source, body, nil)
		if err != nil {
			deps.log.Error("intake: hide put failed", "error", err)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		resp := map[string]string{"hide_id": entry.ID}
		if queueName != "" && deps.queueMgr != nil {
			item := model.QueueItem{
				ID:         queue.GenerateItemID(),
				CuringName: curingName,
				HideID:     entry.ID,
				HideKind:   entry.Kind,
				EnqueuedAt: time.Now().Unix(),
				Payload:    map[string]any{"hide_id": entry.ID, "curing": curingName},
			}
			if err := deps.queueMgr.Enqueue(queueName, item); err != nil {
				deps.log.Error("intake: enqueue failed", "queue", queueName, "error", err)
			} else {
				resp["queue"] = queueName
				resp["curing"] = curingName
			}
		}
		if deps.devtoolsSrc != nil {
			deps.devtoolsSrc.PublishHTTP("intake.received", map[string]any{
				"kind":    kind,
				"source":  source,
				"queue":   queueName,
				"curing":  curingName,
				"hide_id": entry.ID,
			})
		}
		writeJSON(w, http.StatusAccepted, resp)
	}
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a JSON {"error":"<msg>"} response with the given status.
func writeJSONError(w http.ResponseWriter, msg string, status int) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// drainTannery stops all curing workers and waits for in-flight work to finish.
// Safe to call when td is nil.
func drainTannery(td *tanneryDeps) {
	if td == nil {
		return
	}
	if td.curingSupv != nil {
		td.curingSupv.Drain()
	}
	if td.lockFile != nil {
		releaseProcessLock(td.lockFile)
	}
}

// agentsByName converts a slice of agents into a map keyed by Name.
func agentsByName(agents []model.Agent) map[string]model.Agent {
	m := make(map[string]model.Agent, len(agents))
	for _, a := range agents {
		m[a.Name] = a
	}
	return m
}
