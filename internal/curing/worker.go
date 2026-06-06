package curing

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tgpski/leather/internal/artifact"
	"github.com/tgpski/leather/internal/cache"
	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/mcp"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/notify"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/runner"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
)

// TanneryEvent describes a discrete state change in the tannery pipeline.
// EventFn receivers use this to drive pretty output, metrics, and alerting
// without coupling the worker to any display or transport layer.
type TanneryEvent struct {
	// Kind is one of: "webhook", "enqueue", "dequeue", "retry", "dlq".
	// "webhook" is emitted by the HTTP webhook handler on successful enqueue.
	Kind string
	// Curing is the curing definition name (w.def.Name).
	Curing string
	// Queue is the source queue name (w.def.Queue).
	Queue string
	// DestQueue is the destination queue for "enqueue" events.
	DestQueue string
	// HideID and HideKind identify the hide being processed.
	HideID   string
	HideKind string
	// ItemID is the QueueItem.ID.
	ItemID string
	// Attempt is the 1-based attempt count; 0 for non-failure events.
	Attempt int
	// Err is the error string for failure events; empty otherwise.
	Err string
	// Source is the intake source identifier (e.g. webhook HTTP path).
	// Set for "webhook" events; empty for worker-emitted events.
	Source string
	// WebhookName is the webhook configuration name (e.g. "demo").
	// Set for "webhook" events; empty for worker-emitted events.
	WebhookName string
}

// errAgentNotFound is a sentinel for configuration errors. Items that trigger
// this error are routed to the DLQ immediately, bypassing the retry counter.
var errAgentNotFound = errors.New("agent not found")

// errHideMissing is a sentinel for hides that have disappeared between dequeue
// and load (manual cleanup, external delete, corruption). Items that trigger
// this error are routed to the DLQ immediately rather than wasting retries on
// a hide that will never reappear.
var errHideMissing = errors.New("hide missing")

// RunnerDeps groups the dependencies needed to construct a runner.Runner per call.
// Owned by the Supervisor and shared (by pointer) across all Workers.
type RunnerDeps struct {
	Client        session.LLMClient
	ToolReg       *tool.Registry
	Log           *logging.Logger
	MaxToolRounds int
	Cache         *cache.FileCache
	QueueMgr      *queue.Manager
	Notifiers     map[string]notify.Notifier
	MCPReg        *mcp.Registry
	// Budget is the base token budget derived from the global config.
	// Per-agent MaxTokens overrides are applied at process time.
	Budget model.TokenBudget
	// ProgressFn, when non-nil, is forwarded to each runner.Runner so the caller
	// receives live progress events (tool calls, results, extracts) during a turn.
	ProgressFn func(runner.ProgressEvent)
	// ProgressWithAgent, when non-nil, is called for every runner progress event
	// with the curing and agent name included. Useful for callers that need to tag
	// events by agent (e.g. DevTools bus) without building a per-agent ProgressFn closure.
	ProgressWithAgent func(curingName, agentName string, ev runner.ProgressEvent)
	// DebugContextFn, when non-nil, is forwarded to each runner.Runner so the
	// caller can inspect the exact message window before each LLM completion.
	DebugContextFn func(runner.ContextSnapshot)
	// OnComplete, when non-nil, is called after a successful curing item — artifact
	// written, before hide cleanup. Callers use this to accumulate token statistics,
	// persist run records, or render pretty output. Called synchronously; must not
	// block for long (it runs inside the worker goroutine).
	// events holds all ProgressEvents fired by the runner during the run so callers
	// can replay them into a pretty printer without needing a real-time ProgressFn.
	OnComplete func(ag model.Agent, rec model.RunRecord, art model.Artifact, events []runner.ProgressEvent)
	// OnRunRecord, when non-nil, is called after every curing item run — success
	// or failure — with the resulting RunRecord and the error (nil on success).
	// Used to persist run history for curing-driven agents (which never reach
	// OnComplete on failure). Called synchronously on the worker goroutine.
	OnRunRecord func(ag model.Agent, rec model.RunRecord, runErr error)
	// EventFn, when non-nil, receives pipeline lifecycle events (dequeue, retry, dlq).
	// Called synchronously on the worker goroutine; must not block for long.
	// T4.8: by default the Worker serializes EventFn calls under a mutex so an
	// EventFn that mutates shared state without internal locking is safe.
	// Callers whose EventFn is itself thread-safe can set EventFnConcurrent: true
	// to opt out and let the worker fan out without serialization.
	EventFn func(TanneryEvent)
	// EventFnConcurrent disables the default EventFn serialization. Default false
	// (serialized).
	EventFnConcurrent bool
}

// Worker consumes one named queue and runs the curing workflow for each item.
// Each Worker polls its queue at 1-second intervals. Concurrent item processing
// is bounded by the semaphore channel capacity (QueueConcurrencyConfig.Concurrency).
type Worker struct {
	def       model.CuringDefinition
	agents    map[string]model.Agent
	hideStore *hide.Store
	artStore  *artifact.Store
	deps      *RunnerDeps
	q         *queue.FileQueue // pre-fetched queue handle for this curing
	qmgr      *queue.Manager   // for output routing enqueues and DLQ
	log       *logging.Logger
	sem       chan struct{}    // bounded by QueueConcurrencyConfig.Concurrency
	inflight  sync.WaitGroup  // tracks goroutines spawned by Run; joined on shutdown
	active    atomic.Int32    // count of items currently being processed
	eventMu   sync.Mutex      // T4.8: serializes EventFn unless EventFnConcurrent is set
}

// NewWorker constructs a Worker for the given CuringDefinition.
// concurrency sets the semaphore size; values <= 0 default to 1.
// Returns an error when def.Agent does not appear in agents (fail-fast on
// misconfiguration rather than wasting an item on the first dequeue).
func NewWorker(
	def model.CuringDefinition,
	agents map[string]model.Agent,
	concurrency int,
	hideStore *hide.Store,
	artStore *artifact.Store,
	deps *RunnerDeps,
	qmgr *queue.Manager,
	_ *Router, // reserved for future content-based output routing
	log *logging.Logger,
) (*Worker, error) {
	if concurrency <= 0 {
		concurrency = 1
	}
	if _, ok := agents[def.Agent]; !ok {
		return nil, fmt.Errorf("curing/NewWorker %s: agent %q not found in loaded agents", def.Name, def.Agent)
	}
	w := &Worker{
		def:       def,
		agents:    agents,
		hideStore: hideStore,
		artStore:  artStore,
		deps:      deps,
		qmgr:      qmgr,
		log:       log,
		sem:       make(chan struct{}, concurrency),
	}
	// Prefix-based workers discover their queues dynamically; no static queue needed.
	if def.QueuePrefix == "" {
		q, err := qmgr.Get(def.Queue)
		if err != nil {
			return nil, fmt.Errorf("curing/NewWorker %s: get queue %s: %w", def.Name, def.Queue, err)
		}
		w.q = q
	}
	return w, nil
}

// Run blocks, polling the curing queue at 1-second intervals. Returns when
// ctx is cancelled. Goroutine panics are recovered and logged.
// In-flight item handlers spawned from this loop are tracked on w.inflight
// and must be drained (Worker.WaitInflight) before the Worker is considered
// shut down.
func (w *Worker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}

		// Prefix-based workers scan for single-use queues dynamically.
		if w.def.QueuePrefix != "" {
			w.runPrefixScan(ctx)
			continue
		}

		if w.def.CollectSize > 0 {
			w.runCollect(ctx)
			continue
		}

		item, ok, err := w.q.Dequeue()
		if err != nil {
			w.log.Warn("curing/Run: dequeue error", "curing", w.def.Name, "error", err)
			continue
		}
		if !ok {
			continue
		}

		// Emit dequeue event before semaphore acquisition so the caller
		// sees it immediately upon item pickup, not after concurrency throttling.
		w.emit(TanneryEvent{
			Kind:     "dequeue",
			Curing:   w.def.Name,
			Queue:    w.def.Queue,
			HideID:   item.HideID,
			HideKind: item.HideKind,
			ItemID:   item.ID,
		})

		// Acquire semaphore before spawning goroutine to bound concurrency.
		// The goroutine receives context.Background() so that cancelling the
		// Run loop (to stop polling) does not abort in-flight LLM calls.
		// Per-item timeouts are applied inside process() via TimeoutSeconds.
		w.sem <- struct{}{}
		w.inflight.Add(1)
		w.active.Add(1)
		go func(it model.QueueItem) {
			defer w.inflight.Done()
			defer w.active.Add(-1)
			defer func() { <-w.sem }()
			defer func() {
				if r := recover(); r != nil {
					w.log.Error("curing/Run: panic recovered", "curing", w.def.Name, "panic", r)
				}
			}()
			w.handleItem(context.Background(), it)
		}(item)
	}
}

// runCollect handles one poll tick when CollectSize > 0.
// It scans the queue for the first group of CollectSize items that share the
// same key (CollectBy field), dequeues them atomically, and processes them as
// a single combined agent invocation. Items from other correlation groups
// remain in the queue for subsequent ticks.
func (w *Worker) runCollect(ctx context.Context) {
	collectBy := w.def.CollectBy
	if collectBy == "" {
		collectBy = "hide_id"
	}
	n := w.def.CollectSize

	// Snapshot without dequeuing; find the first key that has >= n items.
	all := w.q.Scan()
	if len(all) < n {
		return // not enough items yet regardless of grouping
	}

	key := func(item model.QueueItem) string {
		switch collectBy {
		case "curing_name":
			return item.CuringName
		case "correlation_id":
			return item.CorrelationID
		default: // "hide_id"
			return item.HideID
		}
	}

	counts := make(map[string]int, len(all))
	for _, it := range all {
		counts[key(it)]++
	}
	var groupKey string
	for _, it := range all { // preserve queue order: pick earliest eligible group
		k := key(it)
		if counts[k] >= n {
			groupKey = k
			break
		}
	}
	if groupKey == "" {
		return // no complete group yet
	}

	// Collect exactly n matching IDs in queue order.
	var ids []string
	for _, it := range all {
		if key(it) == groupKey {
			ids = append(ids, it.ID)
			if len(ids) == n {
				break
			}
		}
	}

	matched, err := w.q.DequeueByIDs(ids)
	if err != nil {
		w.log.Warn("curing/runCollect: DequeueByIDs failed", "curing", w.def.Name, "error", err)
		return
	}
	if len(matched) == 0 {
		return
	}

	for _, it := range matched {
		w.emit(TanneryEvent{
			Kind:     "dequeue",
			Curing:   w.def.Name,
			Queue:    w.def.Queue,
			HideID:   it.HideID,
			HideKind: it.HideKind,
			ItemID:   it.ID,
		})
	}

	w.sem <- struct{}{}
	w.inflight.Add(1)
	w.active.Add(1)
	go func(items []model.QueueItem) {
		defer w.inflight.Done()
		defer w.active.Add(-1)
		defer func() { <-w.sem }()
		defer func() {
			if r := recover(); r != nil {
				w.log.Error("curing/runCollect: panic recovered", "curing", w.def.Name, "panic", r)
			}
		}()
		w.handleCollected(context.Background(), items)
	}(matched)
}

// ActiveCount returns the number of item-handler goroutines currently running.
// Used by waitForQuiescence to detect in-flight work even when queue depth is 0.
func (w *Worker) ActiveCount() int {
	return int(w.active.Load())
}

// WaitInflight blocks until every handleItem goroutine spawned by Run has
// finished. Call this after the Run loop has returned (i.e. after ctx is
// cancelled) to ensure no in-flight artifact write outlives the lock release.
func (w *Worker) WaitInflight() {
	w.inflight.Wait()
}

// runPrefixScan handles one poll tick for prefix-based workers (QueuePrefix != "").
// It discovers all single-use queues matching the prefix, then for each queue:
//   - When CollectSize > 0: waits until CollectSize items are present, dequeues
//     them atomically, and processes them in one combined agent invocation.
//   - When CollectSize == 0: dequeues one item and processes it individually,
//     applying retry/DLQ logic. The source queue is GC'd when it empties.
func (w *Worker) runPrefixScan(ctx context.Context) {
	names, err := w.qmgr.NamesWithPrefix(w.def.QueuePrefix)
	if err != nil {
		w.log.Warn("curing/runPrefixScan: scan failed",
			"curing", w.def.Name, "prefix", w.def.QueuePrefix, "error", err)
		return
	}
	for _, name := range names {
		// Skip DLQ queues created by prior failed attempts. They are not
		// reprocessed by the prefix scanner — they remain on disk for manual
		// inspection. Without this guard, a hide-missing DLQ cascades: the
		// scanner picks up the -dlq queue, fails again, creates -dlq-dlq, etc.
		if strings.HasSuffix(name, "-dlq") {
			continue
		}
		q, err := w.qmgr.Get(name)
		if err != nil {
			w.log.Warn("curing/runPrefixScan: get queue failed",
				"curing", w.def.Name, "queue", name, "error", err)
			continue
		}
		if w.def.CollectSize > 0 {
			w.runCollectFromQueue(ctx, name, q)
		} else {
			item, ok, err := q.Dequeue()
			if err != nil {
				w.log.Warn("curing/runPrefixScan: dequeue error",
					"curing", w.def.Name, "queue", name, "error", err)
				continue
			}
			if !ok {
				// Queue exists on disk but is empty — stale file; GC it.
				if err := w.qmgr.Delete(name); err != nil {
					w.log.Warn("curing/runPrefixScan: GC empty queue failed",
						"curing", w.def.Name, "queue", name, "error", err)
				}
				continue
			}
			w.emit(TanneryEvent{
				Kind:     "dequeue",
				Curing:   w.def.Name,
				Queue:    name,
				HideID:   item.HideID,
				HideKind: item.HideKind,
				ItemID:   item.ID,
			})
			w.sem <- struct{}{}
			w.inflight.Add(1)
			w.active.Add(1)
			go func(it model.QueueItem, queueName string) {
				defer w.inflight.Done()
				defer w.active.Add(-1)
				defer func() { <-w.sem }()
				defer func() {
					if r := recover(); r != nil {
						w.log.Error("curing/runPrefixScan: panic recovered",
							"curing", w.def.Name, "panic", r)
					}
				}()
				// handleItemFromQueue applies retry/DLQ using the actual queue name.
				w.handleItemFromQueue(context.Background(), it, queueName)
				// GC the single-use queue if it is now empty.
				sq, err := w.qmgr.Get(queueName)
				if err == nil && sq.Len() == 0 {
					if err := w.qmgr.Delete(queueName); err != nil {
						w.log.Warn("curing/runPrefixScan: GC failed",
							"curing", w.def.Name, "queue", queueName, "error", err)
					} else if it.HideID != "" {
						// Delete the hide only when no other queue on disk still
						// references it. Fan-out routes all embed the hide_id in
						// their queue name, so NamesContaining returns the
						// siblings that haven't finished yet.
						remaining, _ := w.qmgr.NamesContaining(it.HideID)
						if len(remaining) == 0 {
							if err := w.hideStore.Delete(it.HideID); err != nil {
								w.log.Warn("curing/runPrefixScan: hide delete failed",
									"curing", w.def.Name, "hide", it.HideID, "error", err)
							}
						}
					}
				}
			}(item, name)
		}
	}
}

// runCollectFromQueue handles one poll tick for a specific single-use queue
// when CollectSize > 0. It waits until CollectSize items are present, dequeues
// them atomically, and processes them as one combined agent invocation. GCs the
// queue after a successful collect.
func (w *Worker) runCollectFromQueue(ctx context.Context, queueName string, q *queue.FileQueue) {
	n := w.def.CollectSize
	all := q.Scan()
	if len(all) < n {
		return // not enough items yet
	}

	// Collect exactly n items in queue order.
	var ids []string
	for _, it := range all {
		ids = append(ids, it.ID)
		if len(ids) == n {
			break
		}
	}

	matched, err := q.DequeueByIDs(ids)
	if err != nil {
		w.log.Warn("curing/runCollectFromQueue: DequeueByIDs failed",
			"curing", w.def.Name, "queue", queueName, "error", err)
		return
	}
	if len(matched) == 0 {
		return
	}

	for _, it := range matched {
		w.emit(TanneryEvent{
			Kind:     "dequeue",
			Curing:   w.def.Name,
			Queue:    queueName,
			HideID:   it.HideID,
			HideKind: it.HideKind,
			ItemID:   it.ID,
		})
	}

	w.sem <- struct{}{}
	w.inflight.Add(1)
	w.active.Add(1)
	go func(items []model.QueueItem, qn string) {
		defer w.inflight.Done()
		defer w.active.Add(-1)
		defer func() { <-w.sem }()
		defer func() {
			if r := recover(); r != nil {
				w.log.Error("curing/runCollectFromQueue: panic recovered",
					"curing", w.def.Name, "panic", r)
			}
		}()
		w.handleCollected(context.Background(), items)
		// GC the single-use queue; handleCollected owns success semantics.
		if err := w.qmgr.Delete(qn); err != nil {
			w.log.Warn("curing/runCollectFromQueue: GC failed",
				"curing", w.def.Name, "queue", qn, "error", err)
		}
	}(matched, queueName)
}

// emit fires ev on deps.EventFn when it is set. It is a no-op otherwise.
// Called on the worker goroutine; EventFn must not block for long. Calls are
// serialized via w.eventMu by default; callers can opt out by setting
// RunnerDeps.EventFnConcurrent (T4.8).
func (w *Worker) emit(ev TanneryEvent) {
	if w.deps.EventFn == nil {
		return
	}
	if w.deps.EventFnConcurrent {
		w.deps.EventFn(ev)
		return
	}
	w.eventMu.Lock()
	defer w.eventMu.Unlock()
	w.deps.EventFn(ev)
}

// handleCollected processes a correlated batch of items as one agent invocation.
// Each item's hide content is loaded and concatenated into a single user prompt
// separated by clear delimiters. The first item's hide_id is used for the artifact.
func (w *Worker) handleCollected(ctx context.Context, items []model.QueueItem) {
	if len(items) == 0 {
		return
	}
	ag, ok := w.agents[w.def.Agent]
	if !ok {
		w.log.Error("curing/handleCollected: agent not found",
			"curing", w.def.Name, "agent", w.def.Agent)
		return
	}

	// Load each hide and concatenate content with delimiters.
	var parts []string
	for i, item := range items {
		buf, err := w.hideStore.LoadIntoBuffer(item.HideID, w.def.PageSizeBytes)
		if err != nil {
			w.log.Error("curing/handleCollected: hide load failed",
				"curing", w.def.Name, "hide", item.HideID, "error", err)
			return
		}
		cut, cutErr := buf.FirstCut()
		if cutErr != nil {
			w.log.Warn("curing/handleCollected: hide buffer empty",
				"curing", w.def.Name, "hide", item.HideID, "error", cutErr)
			continue
		}
		parts = append(parts, fmt.Sprintf("--- ANALYSIS %d (from: %s) ---\n%s", i+1, item.CuringName, cut.Format()))
	}

	combined := strings.Join(parts, "\n\n")

	// Inject combined content into agent.
	if len(ag.UserPrompts) > 0 {
		ag.UserPrompts = append([]string(nil), ag.UserPrompts...)
		ag.UserPrompts[0] = combined + "\n\n" + ag.UserPrompts[0]
	} else if ag.UserPrompt != "" {
		ag.UserPrompt = combined + "\n\n" + ag.UserPrompt
	} else {
		ag.UserPrompt = combined
	}

	// Use a nil-buffer runner (no hide paging needed; content already combined).
	r := w.buildRunner(nil)

	var progressEvents []runner.ProgressEvent
	baseFn := r.ProgressFn
	agentName := ag.Name
	r.ProgressFn = func(ev runner.ProgressEvent) {
		progressEvents = append(progressEvents, ev)
		if baseFn != nil {
			baseFn(ev)
		}
		if w.deps.ProgressWithAgent != nil {
			w.deps.ProgressWithAgent(w.def.Name, agentName, ev)
		}
	}

	budget := w.deps.Budget
	if ag.MaxTokens > 0 {
		budget.MaxTokens = ag.MaxTokens
	}

	rec, err := r.Run(ctx, ag, budget)
	if w.deps.OnRunRecord != nil {
		w.deps.OnRunRecord(ag, rec, err)
	}
	if err != nil {
		w.log.Error("curing/handleCollected: runner failed", "curing", w.def.Name, "error", err)
		return
	}

	lastResponse := rec.LastResponse
	if lastResponse == "" && len(rec.Turns) > 0 {
		lastResponse = rec.Turns[len(rec.Turns)-1].Response
	}

	art := model.Artifact{
		ID:         artifact.GenerateArtifactID(),
		HideID:     items[0].HideID,
		HideKind:   items[0].HideKind,
		CuringName: w.def.Name,
		AgentName:  w.def.Agent,
		Queue:      w.def.Queue,
		Content:    lastResponse,
		CreatedAt:  time.Now().Unix(),
	}
	if err := w.artStore.Write(art); err != nil {
		w.log.Error("curing/handleCollected: artifact write failed", "curing", w.def.Name, "error", err)
		return
	}

	if w.deps.OnComplete != nil {
		w.deps.OnComplete(ag, rec, art, progressEvents)
	}

	if w.def.Output.Notify != "" {
		if err := w.dispatchNotify(ctx, w.def.Output.Notify, art); err != nil {
			w.log.Warn("curing/handleCollected: notify failed",
				"artifact", art.ID, "backend", w.def.Output.Notify, "error", err)
		}
	}
	if w.def.Output.Queue != "" {
		if err := w.dispatchQueue(w.def.Output.Queue, art, items[0].CorrelationID); err != nil {
			w.log.Warn("curing/handleCollected: output queue enqueue failed",
				"artifact", art.ID, "queue", w.def.Output.Queue, "error", err)
		}
	}

	// Clean up all collected hides on success.
	for _, item := range items {
		if err := w.hideStore.Delete(item.HideID); err != nil {
			w.log.Warn("curing/handleCollected: hide delete failed (orphan)",
				"hide", item.HideID, "error", err)
		}
	}
}

// ProcessItem processes one QueueItem end-to-end, returning any error.
// Unlike handleItem it does not apply retry/DLQ logic — callers are responsible
// for re-enqueuing on failure. Intended for testing and one-shot invocations.
func (w *Worker) ProcessItem(ctx context.Context, item model.QueueItem) error {
	return w.process(ctx, item)
}

// handleItem processes one item and applies retry/DLQ logic on failure.
func (w *Worker) handleItem(ctx context.Context, item model.QueueItem) {
	w.handleItemFromQueue(ctx, item, w.def.Queue)
}

// handleItemFromQueue applies retry/DLQ logic against queueName (which may
// differ from w.def.Queue for prefix-based single-use queue workers).
func (w *Worker) handleItemFromQueue(ctx context.Context, item model.QueueItem, queueName string) {
	err := w.process(ctx, item)
	if err == nil {
		return
	}

	item.AttemptCount++
	target := queueName + "-dlq"
	// Sentinel errors (config-level: agent missing, hide missing) skip retry.
	isSentinel := errors.Is(err, errAgentNotFound) || errors.Is(err, errHideMissing)
	if !isSentinel && item.AttemptCount < w.def.MaxAttempts {
		target = queueName
	}
	w.log.Warn("curing/handleItemFromQueue: process failed",
		"curing", w.def.Name, "item", item.ID, "attempt", item.AttemptCount,
		"target", target, "error", err)

	// Emit retry or dlq event.
	evKind := "dlq"
	if target == queueName {
		evKind = "retry"
	}
	w.emit(TanneryEvent{
		Kind:     evKind,
		Curing:   w.def.Name,
		Queue:    queueName,
		HideID:   item.HideID,
		HideKind: item.HideKind,
		ItemID:   item.ID,
		Attempt:  item.AttemptCount,
		Err:      err.Error(),
	})
	if enqErr := w.qmgr.Enqueue(target, item); enqErr != nil {
		// FAILURE OF LAST RESORT: item is lost from this process.
		w.log.Error("curing/handleItemFromQueue: enqueue failed; item dropped",
			"curing", w.def.Name, "queue", target, "item", item.ID, "error", enqErr)
	}
}

// process handles one QueueItem end-to-end. Returns nil on success.
// Non-nil errors trigger the retry/DLQ logic in handleItem.
func (w *Worker) process(ctx context.Context, item model.QueueItem) error {
	// 1. Per-run timeout.
	if w.def.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(w.def.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	// 2. Lookup agent definition.
	ag, ok := w.agents[w.def.Agent]
	if !ok {
		w.log.Error("curing/process: agent not found",
			"curing", w.def.Name, "agent", w.def.Agent)
		return errAgentNotFound
	}

	// 3. Load hide into HideBuffer.
	buf, err := w.hideStore.LoadIntoBuffer(item.HideID, w.def.PageSizeBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			w.log.Error("curing/process: hide missing (DLQ immediately)",
				"curing", w.def.Name, "hide", item.HideID)
			return errHideMissing
		}
		return fmt.Errorf("curing/process: load hide %s: %w", item.HideID, err)
	}

	// 3a. Inject hide content as user prompt(s).
	//
	// Three cases:
	//   (a) Agent has no user prompts AND hide has >1 page → auto-generate one
	//       reflection turn per page so the model reads and reflects on each
	//       page before producing output. Hide navigation tools are included
	//       automatically (runner gates on NeedsPaging).
	//   (b) Agent has no user prompts AND hide is single-page → set UserPrompt
	//       directly; no paging tools are needed.
	//   (c) Agent already declares a UserPrompts chain → prepend page 1 to
	//       UserPrompts[0] and let the agent's own turns drive navigation.
	var reflectionMode bool
	if ag.UserPrompt == "" && len(ag.UserPrompts) == 0 {
		cut, cutErr := buf.FirstCut()
		if cutErr == nil {
			if cut.TotalPages > 1 {
				ag.UserPrompts = buildReflectionTurns(cut)
				reflectionMode = true
			} else {
				ag.UserPrompt = cut.Format()
			}
		} else {
			w.log.Warn("curing/process: hide buffer empty after load",
				"curing", w.def.Name, "hide", item.HideID, "error", cutErr)
		}
	} else if len(ag.UserPrompts) > 0 {
		cut, cutErr := buf.FirstCut()
		if cutErr == nil {
			ag.UserPrompts = append([]string(nil), ag.UserPrompts...)
			ag.UserPrompts[0] = cut.Format() + "\n\n" + ag.UserPrompts[0]
		} else {
			w.log.Warn("curing/process: hide buffer empty after load",
				"curing", w.def.Name, "hide", item.HideID, "error", cutErr)
		}
	}

	// 4. Build per-run runner (no value-copy of a shared base runner).
	r := w.buildRunner(buf)
	// In reflection mode: every non-final Cut header carries a reflection-only
	// instruction (never "call hide_next"). Paging is driven by alternating user
	// turns built in buildReflectionTurns. The page-content turn always ends in a
	// text reflection; the next-page request happens on the following user turn.
	if reflectionMode {
		// Prepend a paging preamble to the system prompt so the model does not
		// produce its final structured output before all pages have been read,
		// even when the original system prompt defines an output format or tells
		// the model to use hide_next autonomously.
		const pagingPreamble = "IMPORTANT: You will receive the content in multiple pages. " +
			"Follow a strict alternating protocol: when a turn contains page content, reply only with 3-5 key facts from that page and do not call hide_next, hide_jump, or produce final output. " +
			"Wait for a later user turn to explicitly request the next page. " +
			"Only when explicitly told that all pages have been read may you produce your final structured output."
		if ag.SystemPrompt != "" {
			ag.SystemPrompt = pagingPreamble + "\n\n" + ag.SystemPrompt
		} else {
			ag.SystemPrompt = pagingPreamble
		}
		buf.ReflectionHint = "After reading this page, list 3-5 key facts verbatim. Do not call hide_next or hide_jump yet, and do not produce your final structured output yet."
		r.ForceTextAfterHide = true
		r.NoToolsForFirstTurn = true
		r.NoToolsForLastTurn = true
	}

	// Wrap ProgressFn so every event is captured for delivery to OnComplete.
	// This lets callers replay events into a pretty printer after the run without
	// needing real-time display hooks inside the worker.
	var progressEvents []runner.ProgressEvent
	baseFn := r.ProgressFn
	agentName := ag.Name
	r.ProgressFn = func(ev runner.ProgressEvent) {
		progressEvents = append(progressEvents, ev)
		if baseFn != nil {
			baseFn(ev)
		}
		if w.deps.ProgressWithAgent != nil {
			w.deps.ProgressWithAgent(w.def.Name, agentName, ev)
		}
	}
	baseCtxFn := r.DebugContextFn
	if baseCtxFn != nil {
		r.DebugContextFn = func(snap runner.ContextSnapshot) {
			snapCopy := snap
			progressEvents = append(progressEvents, runner.ProgressEvent{Kind: "context", Context: &snapCopy})
			baseCtxFn(snap)
		}
	}

	// 5. Build TokenBudget: start from the config-derived base, apply per-agent override.
	budget := w.deps.Budget
	if ag.MaxTokens > 0 {
		budget.MaxTokens = ag.MaxTokens
	}

	// 6. Run agent.
	rec, err := r.Run(ctx, ag, budget)
	if w.deps.OnRunRecord != nil {
		w.deps.OnRunRecord(ag, rec, err)
	}
	if err != nil {
		return fmt.Errorf("curing/process: runner: %w", err)
	}

	// 7. Resolve final response (LastResponse is set by runner P0.3; keep fallback).
	lastResponse := rec.LastResponse
	if lastResponse == "" && len(rec.Turns) > 0 {
		lastResponse = rec.Turns[len(rec.Turns)-1].Response
	}

	// 8. Write artifact — SUCCESS BOUNDARY. Everything after is best-effort.
	art := model.Artifact{
		ID:         artifact.GenerateArtifactID(),
		HideID:     item.HideID,
		HideKind:   item.HideKind,
		CuringName: w.def.Name,
		AgentName:  w.def.Agent,
		Queue:      w.def.Queue,
		Content:    lastResponse,
		CreatedAt:  time.Now().Unix(),
	}
	if err := w.artStore.Write(art); err != nil {
		return fmt.Errorf("curing/process: artifact write: %w", err)
	}

	// 9. Notify caller of successful completion: token accounting, run persistence,
	// pretty output. Runs synchronously inside the worker goroutine; must not block.
	if w.deps.OnComplete != nil {
		w.deps.OnComplete(ag, rec, art, progressEvents)
	}

	// 10. Output routing — best-effort; failures are logged, not retried.
	if w.def.Output.Notify != "" {
		if err := w.dispatchNotify(ctx, w.def.Output.Notify, art); err != nil {
			w.log.Warn("curing/process: notify failed",
				"artifact", art.ID, "backend", w.def.Output.Notify, "error", err)
		}
	}
	if w.def.Output.Queue != "" {
		if err := w.dispatchQueue(w.def.Output.Queue, art, item.CorrelationID); err != nil {
			w.log.Warn("curing/process: output queue enqueue failed",
				"artifact", art.ID, "queue", w.def.Output.Queue, "error", err)
		}
	}

	// 10. Cleanup: delete hide after successful artifact write.
	// For prefix-scan workers (QueuePrefix != ""), the same hide may be
	// referenced by sibling single-use queues (fan-out). Deletion is deferred
	// to runPrefixScan, which deletes the hide only when the last referencing
	// queue is GC'd. For static-queue workers there is exactly one consumer, so
	// we delete immediately.
	// On DLQ the hide is retained for manual inspection.
	if w.def.QueuePrefix == "" {
		if err := w.hideStore.Delete(item.HideID); err != nil {
			w.log.Warn("curing/process: hide delete failed (orphan)",
				"hide", item.HideID, "error", err)
		}
	}

	return nil
}

// buildRunner constructs a fresh runner.Runner from deps and a per-run HideBuffer.
// Never share Runner instances across goroutines or calls.
func (w *Worker) buildRunner(buf *hide.HideBuffer) runner.Runner {
	return runner.Runner{
		Client:         w.deps.Client,
		Registry:       w.deps.ToolReg,
		Log:            w.deps.Log,
		MaxToolRounds:  w.deps.MaxToolRounds,
		Cache:          w.deps.Cache,
		QueueMgr:       w.deps.QueueMgr,
		Notifiers:      w.deps.Notifiers,
		MCPRegistry:    w.deps.MCPReg,
		HideBuffer:     buf,
		ProgressFn:     w.deps.ProgressFn,
		DebugContextFn: w.deps.DebugContextFn,
	}
}

// dispatchNotify sends the artifact content to the named notification backend.
func (w *Worker) dispatchNotify(ctx context.Context, backendName string, art model.Artifact) error {
	n, ok := w.deps.Notifiers[backendName]
	if !ok {
		return fmt.Errorf("notifier %q not configured", backendName)
	}
	return n.Send(ctx, notify.Message{
		AgentName: art.AgentName,
		Content:   art.Content,
		Timestamp: time.Unix(art.CreatedAt, 0),
	})
}

// dispatchQueue enqueues the artifact as a downstream work item.
// The artifact content is written as a new hide so downstream curings can
// load it via the standard hide-loading path (LoadIntoBuffer).
// correlationID is threaded from the source item so correlated-collect curings
// downstream can group items that originated from the same fan-out event.
// queueName supports {{correlation_id}} and {{hide_id}} template expansion so
// single-use output queues can be addressed per-event.
func (w *Worker) dispatchQueue(queueName string, art model.Artifact, correlationID string) error {
	// Expand single-use queue name templates.
	queueName = strings.ReplaceAll(queueName, "{{correlation_id}}", correlationID)
	queueName = strings.ReplaceAll(queueName, "{{hide_id}}", art.HideID)
	entry, err := w.hideStore.Put(art.CuringName, art.CuringName, []byte(art.Content), nil)
	if err != nil {
		return fmt.Errorf("curing/dispatchQueue: write downstream hide: %w", err)
	}
	item := model.QueueItem{
		ID:            art.ID,
		HideID:        entry.ID,
		HideKind:      entry.Kind,
		CorrelationID: correlationID,
		CuringName:    art.CuringName,
		Payload:       map[string]any{"artifact_id": art.ID, "curing_name": art.CuringName},
		EnqueuedAt:    time.Now().Unix(),
	}
	if err := w.qmgr.Enqueue(queueName, item); err != nil {
		return err
	}
	w.emit(TanneryEvent{
		Kind:      "enqueue",
		Curing:    w.def.Name,
		Queue:     w.def.Queue,
		DestQueue: queueName,
		HideID:    entry.ID,
		HideKind:  entry.Kind,
		ItemID:    item.ID,
	})
	return nil
}

// buildReflectionTurns generates an alternating UserPrompts chain for a
// multi-page hide. For an N-page hide it produces N+1 turns:
//
//	Turn 0:        page 1 content (Cut header instructs the model to list key facts).
//	Turn 1..N-1:   "Now call hide_next to retrieve page K." (one per remaining page).
//	Turn N:        "All pages read — produce final output."
//
// The page itself commands the summary-only response. Page transitions are
// driven by later user turns that explicitly ask for the next page. Between
// each call-next user turn the model executes hide_next; the runner's
// ForceTextAfterHide then strips tools so the model produces a text
// reflection on the newly delivered page before the next user turn arrives.
func buildReflectionTurns(firstCut hide.Cut) []string {
	n := firstCut.TotalPages
	turns := make([]string, 0, n+1)

	// Turn 0: deliver page 1 with explicit instruction to list facts only.
	turns = append(turns, firstCut.Format()+
		"\n\nPage 1 of "+fmt.Sprintf("%d", n)+" is above. "+
		"List 3-5 key facts verbatim from this page. Do not call hide_next yet, and do not produce your final structured output yet.")

	// Turns 1..N-1: one "call hide_next" prompt per remaining page.
	for k := 2; k <= n; k++ {
		turns = append(turns, fmt.Sprintf(
			"Now call hide_next to retrieve page %d. After the page is delivered, follow that page's instruction before doing anything else.",
			k,
		))
	}

	// Turn N: all pages read — produce final structured output.
	turns = append(turns, fmt.Sprintf(
		"You have now read all %d pages. Produce your complete structured output as specified in your system instructions.",
		n))

	return turns
}
