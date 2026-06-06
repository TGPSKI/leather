package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/tgpski/leather/internal/agent"
	"github.com/tgpski/leather/internal/cache"
	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/curing"
	"github.com/tgpski/leather/internal/devtools/bus"
	"github.com/tgpski/leather/internal/devtools/sources"
	"github.com/tgpski/leather/internal/httpx"
	"github.com/tgpski/leather/internal/ids"
	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/mcp"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/notify"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/runner"
	"github.com/tgpski/leather/internal/scheduler"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
	"github.com/tgpski/leather/internal/worker"
	"github.com/tgpski/leather/ui"
)

// runHistoryMax is the maximum number of RunRecords retained per agent.
const runHistoryMax = 200

const prettyLabelWidth = 9

const (
	prettyStatusInterval = 125 * time.Millisecond
	prettyStatusMaxWidth = 120
)

var prettyStatusFrames = []string{"◐", "◓", "◑", "◒"}

type prettyRunPrinter struct {
	out           io.Writer
	mode          string
	showStats     bool
	tokensPerTurn bool
	showVars      bool
	events        []prettyRecordedEvent
	status        *prettyStatusTicker
}

type prettyRecordedEvent struct {
	at    time.Time
	event runner.ProgressEvent
}

type prettyStatusTicker struct {
	out     io.Writer
	closer  io.Closer
	start   time.Time
	summary string
	mu      sync.Mutex
	done    chan struct{}
	wg      sync.WaitGroup
	enabled bool
	closed  bool
}

func newPrettyRunPrinter(out io.Writer, cfg model.Config) *prettyRunPrinter {
	printer := &prettyRunPrinter{
		out:           out,
		mode:          normalizedPrettyMode(cfg.PrettyMode),
		showStats:     cfg.Stats,
		tokensPerTurn: cfg.TokensPerTurn,
		showVars:      cfg.ShowVars,
	}
	if printer.mode == "all" {
		printer.status = newPrettyStatusTicker(out)
		printer.status.Start("starting run")
	}
	return printer
}

func configurePrettyRunner(base runner.Runner, out io.Writer, cfg model.Config) (runner.Runner, *prettyRunPrinter) {
	configured := base
	if !cfg.Pretty {
		if cfg.ShowContext {
			configured.DebugContextFn = func(s runner.ContextSnapshot) {
				printContextSnapshot(out, s)
			}
		}
		configured.ProgressFn = nil
		return configured, nil
	}
	printer := newPrettyRunPrinter(out, cfg)
	if cfg.ShowContext {
		configured.DebugContextFn = func(s runner.ContextSnapshot) { printer.Context(s) }
	}
	if shouldPrintPrettyProgress(cfg) || cfg.ShowVars {
		configured.ProgressFn = func(ev runner.ProgressEvent) { printer.Progress(ev) }
	} else {
		configured.ProgressFn = nil
	}
	return configured, printer
}

func (p *prettyRunPrinter) Progress(ev runner.ProgressEvent) {
	// extract events are only stored when --show-vars is active.
	if ev.Kind == "extract" && !p.showVars {
		return
	}
	p.events = append(p.events, prettyRecordedEvent{at: time.Now(), event: ev})
	if p.status != nil {
		p.status.Update(prettyStatusSummary(ev))
	}
}

func (p *prettyRunPrinter) Context(s runner.ContextSnapshot) {
	p.events = append(p.events, prettyRecordedEvent{
		at: time.Now(),
		event: runner.ProgressEvent{
			Kind:    "context",
			Round:   s.Round,
			Context: &s,
		},
	})
	if p.status != nil {
		p.status.Update(prettyStatusSummary(runner.ProgressEvent{Kind: "context", Context: &s}))
	}
}

func (p *prettyRunPrinter) Stop() {
	if p != nil && p.status != nil {
		p.status.Stop()
	}
}

func (p *prettyRunPrinter) Render(a model.Agent, rec model.RunRecord) {
	p.Stop()
	if p == nil {
		return
	}
	if p.mode == "all" {
		renderPrettyTimeline(p.out, a, rec, p.events, p.showStats, p.tokensPerTurn)
	} else {
		renderPrettyMessages(p.out, a, rec, p.mode, p.showStats, p.tokensPerTurn)
	}
	if p.showStats {
		fmt.Fprintf(p.out, "  %s\n", dim(fmt.Sprintf("duration=%dms", rec.Time.DurationMs)))
	}
	if len(rec.Turns) > 0 || len(p.events) > 0 {
		fmt.Fprintln(p.out)
	}
}

func newPrettyStatusTicker(out io.Writer) *prettyStatusTicker {
	statusOut, closer, enabled := resolvePrettyStatusWriter(out)
	return &prettyStatusTicker{
		out:     statusOut,
		closer:  closer,
		enabled: enabled,
		done:    make(chan struct{}),
	}
}

func (t *prettyStatusTicker) Start(initial string) {
	if t == nil || !t.enabled {
		return
	}
	t.mu.Lock()
	t.start = time.Now()
	t.summary = initial
	t.mu.Unlock()
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		ticker := time.NewTicker(prettyStatusInterval)
		defer ticker.Stop()
		frame := 0
		for {
			t.render(frame)
			select {
			case <-t.done:
				t.clear()
				return
			case <-ticker.C:
				frame = (frame + 1) % len(prettyStatusFrames)
			}
		}
	}()
}

func (t *prettyStatusTicker) Update(summary string) {
	if t == nil || !t.enabled {
		return
	}
	t.mu.Lock()
	t.summary = summary
	t.mu.Unlock()
}

func (t *prettyStatusTicker) Stop() {
	if t == nil || !t.enabled {
		return
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	close(t.done)
	t.mu.Unlock()
	t.wg.Wait()
	if t.closer != nil {
		_ = t.closer.Close()
	}
}

func resolvePrettyStatusWriter(fallback io.Writer) (io.Writer, io.Closer, bool) {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err == nil {
		return tty, tty, true
	}
	if isInteractiveWriter(fallback) {
		return fallback, nil, true
	}
	return fallback, nil, false
}

func (t *prettyStatusTicker) render(frame int) {
	t.mu.Lock()
	summary := t.summary
	start := t.start
	t.mu.Unlock()
	if summary == "" {
		summary = "running"
	}
	elapsed := formatPrettyElapsed(time.Since(start))
	prefixPlain := fmt.Sprintf("  [%s] %s ", elapsed, prettyStatusFrames[frame])
	available := prettyStatusWidth() - utf8.RuneCountInString(prefixPlain)
	if available < 12 {
		available = 12
	}
	line := fmt.Sprintf("  %s %s %s",
		dim("["+elapsed+"]"),
		boldCyan(prettyStatusFrames[frame]),
		truncateRunes(summary, available))
	fmt.Fprintf(t.out, "\r\033[2K%s", line)
}

func (t *prettyStatusTicker) clear() {
	fmt.Fprint(t.out, "\r\033[2K")
}

func isInteractiveWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func formatPrettyElapsed(d time.Duration) string {
	totalSeconds := int(d / time.Second)
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func prettyStatusWidth() int {
	if raw := strings.TrimSpace(os.Getenv("COLUMNS")); raw != "" {
		if width, err := strconv.Atoi(raw); err == nil && width > 20 {
			return width
		}
	}
	return prettyStatusMaxWidth
}

func prettyStatusSummary(ev runner.ProgressEvent) string {
	switch ev.Kind {
	case "skill_start":
		return "skill " + ev.Skill
	case "system":
		return "system " + compactStatusText(ev.Prompt)
	case "user":
		return "user " + compactStatusText(ev.Prompt)
	case "call":
		summary := ev.ToolType + " " + ev.Tool
		if args := compactStatusArgs(ev.Args); args != "" {
			summary += " " + args
		}
		return summary + fmt.Sprintf(" round=%d", ev.Round+1)
	case "context":
		if ev.Context != nil {
			return fmt.Sprintf("context turn=%d round=%d messages=%d tools=%d",
				ev.Context.Turn+1,
				ev.Context.Round+1,
				len(ev.Context.Messages),
				len(ev.Context.ToolNames),
			)
		}
		return "context"
	case "result":
		if ev.Error != "" {
			return fmt.Sprintf("result %s error=%s", ev.Tool, compactStatusText(ev.Error))
		}
		return fmt.Sprintf("result %s bytes=%d", ev.Tool, ev.ResultBytes)
	default:
		return ev.Kind
	}
}

func compactStatusText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	return truncateRunes(text, 80)
}

func compactStatusArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return ""
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return compactStatusText(raw)
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for i, key := range keys {
		if i == 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s=%s", key, compactStatusValue(value[key])))
	}
	return strings.Join(parts, " ")
}

func compactStatusValue(value any) string {
	switch typed := value.(type) {
	case string:
		return truncateRunes(strings.Join(strings.Fields(typed), " "), 24)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return truncateRunes(fmt.Sprintf("%v", typed), 24)
		}
		return truncateRunes(string(encoded), 24)
	}
}

func truncateRunes(text string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(text) <= max {
		return text
	}
	if max == 1 {
		return "…"
	}
	runes := []rune(text)
	return string(runes[:max-1]) + "…"
}

func renderPrettyMessages(w io.Writer, a model.Agent, rec model.RunRecord, mode string, showStats bool, tokensPerTurn bool) {
	for _, turn := range rec.Turns {
		prettyAgent := a
		prettyAgent.UserPrompt = turn.Prompt
		prettyResp := model.LLMResponse{
			Content:          turn.Response,
			PromptTokens:     turn.PromptTokens,
			CompletionTokens: turn.CompletionTokens,
			TotalTokens:      turn.TotalTokens,
		}
		printTurn(w, prettyAgent, prettyResp, mode, showStats, tokensPerTurn)
	}
}

func renderPrettyTimeline(w io.Writer, a model.Agent, rec model.RunRecord, events []prettyRecordedEvent, showStats bool, tokensPerTurn bool) {
	if len(events) == 0 {
		renderPrettyMessages(w, a, rec, "all", showStats, tokensPerTurn)
		return
	}
	eventIndex := 0
	lastAt := prettyRunEndTime(rec)
	printEvent := func(entry prettyRecordedEvent) {
		printProgressAt(w, entry.at, entry.event)
		lastAt = entry.at
	}
	for eventIndex < len(events) && events[eventIndex].event.Kind != "user" {
		printEvent(events[eventIndex])
		eventIndex++
	}
	for _, turn := range rec.Turns {
		turnAt := lastAt
		if turn.Prompt != "" && eventIndex < len(events) && events[eventIndex].event.Kind == "user" {
			printEvent(events[eventIndex])
			turnAt = events[eventIndex].at
			eventIndex++
		}
		for eventIndex < len(events) && events[eventIndex].event.Kind != "user" {
			printEvent(events[eventIndex])
			turnAt = events[eventIndex].at
			eventIndex++
		}
		prettyAgent := a
		prettyAgent.UserPrompt = turn.Prompt
		prettyResp := model.LLMResponse{
			Content:          turn.Response,
			PromptTokens:     turn.PromptTokens,
			CompletionTokens: turn.CompletionTokens,
			TotalTokens:      turn.TotalTokens,
		}
		printTurnAt(w, turnAt, prettyAgent, prettyResp, "all", showStats, tokensPerTurn)
		lastAt = turnAt
	}
	for eventIndex < len(events) {
		printEvent(events[eventIndex])
		eventIndex++
	}
}

func prettyRunEndTime(rec model.RunRecord) time.Time {
	return time.Unix(rec.Time.StartTs, 0).Add(time.Duration(rec.Time.DurationMs) * time.Millisecond)
}

// agentHistory is a per-agent ring buffer of recent run records (most-recent-first).
// All methods are safe for concurrent use.
type agentHistory struct {
	mu      sync.Mutex
	entries []model.RunRecord
	agent   model.Agent // captured at registration; immutable after init
}

func (h *agentHistory) record(r model.RunRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append([]model.RunRecord{r}, h.entries...)
	if len(h.entries) > runHistoryMax {
		h.entries = h.entries[:runHistoryMax]
	}
}

func (h *agentHistory) snapshot() []model.RunRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]model.RunRecord, len(h.entries))
	copy(out, h.entries)
	return out
}

// durations returns the DurationMs values from all non-empty ring slots.
func (h *agentHistory) durations() []int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]int64, 0, len(h.entries))
	for _, r := range h.entries {
		if r.Time.DurationMs != 0 {
			out = append(out, r.Time.DurationMs)
		}
	}
	return out
}

// runMetrics holds per-agent run history. All methods are safe for concurrent use.
type runMetrics struct {
	mu     sync.RWMutex
	agents map[string]*agentHistory
}

func newRunMetrics() *runMetrics {
	return &runMetrics{agents: make(map[string]*agentHistory)}
}

func (m *runMetrics) ensureAgent(name string) *agentHistory {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[name]; !ok {
		m.agents[name] = &agentHistory{}
	}
	return m.agents[name]
}

// registerAgent pre-creates the history slot with agent metadata.
// Safe to call multiple times; only the first call sets the fields.
func (m *runMetrics) registerAgent(a model.Agent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.agents[a.Name]; !ok {
		m.agents[a.Name] = &agentHistory{agent: a}
	}
}

func (m *runMetrics) record(r model.RunRecord) {
	if r.AgentName == "" {
		// Records without an agent name would create a phantom "" entry in
		// m.agents that pollutes every /metrics response. The caller should
		// always populate AgentName before recording (T4.7).
		return
	}
	m.ensureAgent(r.AgentName).record(r)
}

// agentMetricSummary is the JSON shape returned per agent by GET /metrics.
type agentMetricSummary struct {
	LifecycleFile         string            `json:"lifecycle_file,omitempty"`
	Schedule              string            `json:"schedule,omitempty"`
	Model                 string            `json:"model,omitempty"`
	SystemPrompt          string            `json:"system_prompt,omitempty"`
	UserPrompt            string            `json:"user_prompt,omitempty"`
	Tags                  []string          `json:"tags,omitempty"`
	MaxTokens             int               `json:"max_tokens,omitempty"`
	Temperature           float64           `json:"temperature,omitempty"`
	TimeoutMs             int64             `json:"timeout_ms,omitempty"`
	RunCount              int               `json:"run_count"`
	ErrorCount            int               `json:"error_count"`
	TotalPromptTokens     int               `json:"total_prompt_tokens"`
	TotalCompletionTokens int               `json:"total_completion_tokens"`
	AvgDurationMs         float64           `json:"avg_duration_ms"`
	P50Ms                 int64             `json:"p50_ms"`
	P95Ms                 int64             `json:"p95_ms"`
	P99Ms                 int64             `json:"p99_ms"`
	RecentRuns            []model.RunRecord `json:"recent_runs"`
}

// latencyPercentiles computes P50, P95, and P99 from a slice of millisecond durations.
// Returns zeros when durations is empty.
func latencyPercentiles(durationsMs []int64) (p50, p95, p99 int64) {
	if len(durationsMs) == 0 {
		return 0, 0, 0
	}
	cp := make([]int64, len(durationsMs))
	copy(cp, durationsMs)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	n := len(cp)
	pctIndex := func(pct int) int {
		idx := int(math.Ceil(float64(pct)/100.0*float64(n))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		return idx
	}
	return cp[pctIndex(50)], cp[pctIndex(95)], cp[pctIndex(99)]
}

// summaries computes per-agent stats from the ring buffers.
func (m *runMetrics) summaries() map[string]agentMetricSummary {
	// Snapshot both names AND history pointers under the lock so that a
	// concurrent registerAgent/ensureAgent cannot expose a stale view (T2.2).
	m.mu.RLock()
	histories := make(map[string]*agentHistory, len(m.agents))
	for name, h := range m.agents {
		histories[name] = h
	}
	m.mu.RUnlock()

	out := make(map[string]agentMetricSummary, len(histories))
	for name, h := range histories {
		recs := h.snapshot()
		var totalDur int64
		var errCount, totalPrompt, totalCompletion int
		for _, r := range recs {
			totalDur += r.Time.DurationMs
			totalPrompt += r.Tokens.Prompt
			totalCompletion += r.Tokens.Response
			if r.Status == model.JobStatusError {
				errCount++
			}
		}
		var avgDur float64
		if len(recs) > 0 {
			avgDur = float64(totalDur) / float64(len(recs))
		}
		p50, p95, p99 := latencyPercentiles(h.durations())
		out[name] = agentMetricSummary{
			LifecycleFile:         lifecycleBaseName(h.agent.LifecycleSourcePath),
			Schedule:              h.agent.Schedule,
			Model:                 h.agent.Model,
			SystemPrompt:          h.agent.SystemPrompt,
			UserPrompt:            h.agent.UserPrompt,
			Tags:                  h.agent.Tags,
			MaxTokens:             h.agent.MaxTokens,
			Temperature:           h.agent.Temperature,
			TimeoutMs:             h.agent.Timeout.Milliseconds(),
			RunCount:              len(recs),
			ErrorCount:            errCount,
			TotalPromptTokens:     totalPrompt,
			TotalCompletionTokens: totalCompletion,
			AvgDurationMs:         avgDur,
			P50Ms:                 p50,
			P95Ms:                 p95,
			P99Ms:                 p99,
			RecentRuns:            recs,
		}
	}
	return out
}

// allHistory returns up to limit records from all agents merged and sorted Time.StartTs desc.
func (m *runMetrics) allHistory(limit int) []model.RunRecord {
	// Snapshot history pointers under the lock (T2.2).
	m.mu.RLock()
	histories := make([]*agentHistory, 0, len(m.agents))
	for _, h := range m.agents {
		histories = append(histories, h)
	}
	m.mu.RUnlock()

	var merged []model.RunRecord
	for _, h := range histories {
		merged = append(merged, h.snapshot()...)
	}
	// Sort by StartedAt descending (simple insertion sort over small N).
	for i := 1; i < len(merged); i++ {
		for j := i; j > 0 && merged[j].Time.StartTs > merged[j-1].Time.StartTs; j-- {
			merged[j], merged[j-1] = merged[j-1], merged[j]
		}
	}
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

// apiDeps groups the dependencies required by apiMux.
type apiDeps struct {
	sched        *scheduler.Scheduler
	metrics      *runMetrics
	cfg          model.Config
	startedAt    time.Time
	version      string
	commit       string
	log          *logging.Logger
	replay       *snapshotResponse         // non-nil in --replay mode
	replayLive   *replayLiveState          // non-nil in --replay-live mode
	queueMgr     *queue.Manager            // for /queues API; may be nil
	getWorkerSup func() *worker.Supervisor // for /workers API; nil = not configured
	cacheDir     string                    // for /cache/stats API; empty = not configured
	tannery      *tanneryDeps              // non-nil when tannery mode is active
	runHistDir   string                    // run-history directory (used by curing persistence)
	devtoolsBus  *bus.Bus                  // for /api/devtools/* endpoints
	devtoolsSrc  *sources.Wiring           // publishes source events to devtools bus
	// devtoolsToken, when non-empty, gates every /api/devtools/* route. The
	// token is generated once at serve startup and written to a 0600 file in
	// the state directory (T1.10). Leaving it empty disables gating; tests
	// rely on that to drive the bus directly.
	devtoolsToken string
	// tannery-mode fields: used by initTannery to construct the curing supervisor.
	agents     []model.Agent
	toolReg    *tool.Registry
	agentCache *cache.FileCache
	notifiers  map[string]notify.Notifier
	mcpReg     *mcp.Registry
	// onCuringJobDone, when non-nil, is forwarded to curing.RunnerDeps.OnComplete.
	// Set in RunServe to accumulate token stats, persist run records, and render
	// pretty output for each successfully processed curing item.
	onCuringJobDone func(ag model.Agent, rec model.RunRecord, art model.Artifact, events []runner.ProgressEvent)
	// onCuringEvent, when non-nil, is forwarded to curing.RunnerDeps.EventFn.
	// Set in RunServe to render pipeline lifecycle events (dequeue, retry, dlq)
	// in pretty output so the operator sees what the tannery is doing in real time.
	onCuringEvent func(ev curing.TanneryEvent)
}

// RunServe starts the leather scheduler loop and blocks until signalled.
// It exits cleanly on SIGINT or SIGTERM, draining in-flight jobs first.
func RunServe(args []string, stdout, stderr io.Writer, version, commit string) int {
	fs := newFlagSet("serve", stderr)
	config.BindFlags(fs)
	if !parseFlags(fs, args) {
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather serve: %v\n", err)
		return 1
	}
	// Auto-disable pretty mode when stdout is not an interactive terminal.
	// Prevents ANSI escape codes from appearing in redirected log files.
	if cfg.Pretty && !isTTY(stdout) {
		cfg.Pretty = false
	}
	// --log-file tees full structured logs to a file.
	// --pretty routes structured logs away from the console (file only, or /dev/null).
	logDest := io.Writer(stderr)
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			fmt.Fprintf(stderr, "leather serve: open log file %s: %v\n", cfg.LogFile, err)
			return 1
		}
		defer f.Close()
		if cfg.Pretty {
			logDest = f // pretty mode: full logs only to file, not console
		} else {
			logDest = io.MultiWriter(stderr, f) // tee
		}
	} else if cfg.Pretty {
		logDest = io.Discard // suppress structured logs; turns go to stdout
		// Leading blank line separates leather's pretty output from any preceding
		// shell/make recipe echo (e.g. `cd 02-... && leather/leather serve ...`).
		fmt.Fprintln(stdout)
		prettyWriteEntry(stdout,
			time.Now().Format("15:04:05"),
			boldCyan(prettyPadLabel("leather")),
			[]string{dim("structured logs discarded (--pretty mode). Pass --log-file <path> to capture.")})
	}

	log := buildLogger(cfg, logDest)
	log.Info("leather starting", "agent_dir", cfg.AgentDir, "endpoint", cfg.LLMEndpoint)

	// Dispatch to replay modes before agent loading.
	if cfg.ReplayFile != "" {
		return runReplay(cfg, stderr, log, version, commit)
	}
	if cfg.ReplayLiveDir != "" {
		return runReplayLive(cfg, stderr, log, version, commit)
	}
	agents, errs := agent.LoadDir(cfg.AgentDir)
	for _, e := range errs {
		log.Warn("agent load error", "error", e)
	}
	log.Info("agents loaded", "count", len(agents))

	// Startup ordering (both pretty and plain branches):
	//   1. structured-logs notice  (emitted earlier, only in --pretty)
	//   2. model + endpoint        (here)
	//   3. tannery → curing → agent hierarchy  (after tannery init, below)
	//   4. devtools URL            (after the hierarchy, below)
	//   5. leather start banner    (just before the main loop, below)
	// The agent-count is omitted from line (2) on purpose — it appears
	// implicitly in the hierarchy block.
	if cfg.Pretty {
		ts := time.Now().Format("15:04:05")
		kvLine := dim(fmt.Sprintf("model=%s", cfg.Model)) + "  " +
			dim(fmt.Sprintf("endpoint=%s", cfg.LLMEndpoint))
		prettyWriteEntry(stdout, ts, boldCyan(prettyPadLabel("leather")), []string{kvLine})
	} else {
		fmt.Fprintf(stdout, "leather: model=%s  endpoint=%s\n", cfg.Model, cfg.LLMEndpoint)
	}

	sched := scheduler.New(scheduler.Options{
		MaxConcurrent: cfg.MaxConcurrentJobs,
		StateDir:      cfg.StateDir,
		TickInterval:  cfg.SchedulerTick,
	})

	// RunDuration: exit after a fixed wall-clock duration.
	var runTimer <-chan time.Time
	if cfg.RunDuration > 0 {
		runTimer = time.After(cfg.RunDuration)
	}

	// MaxJobs: exit after a fixed number of completed jobs.
	// jobsDone is nil (blocks forever) when MaxJobs == 0.
	var (
		jobCount  int64
		jobsDone  chan struct{}
		closeOnce sync.Once
	)
	if cfg.MaxJobs > 0 {
		jobsDone = make(chan struct{})
	}
	onJobDone := func() {
		if cfg.MaxJobs > 0 && int(atomic.AddInt64(&jobCount, 1)) >= cfg.MaxJobs {
			closeOnce.Do(func() { close(jobsDone) })
		}
	}

	// Token statistics accumulators (atomic; updated by all job goroutines).
	var (
		statJobs       int64
		statPrompt     int64
		statCompletion int64
	)

	metrics := newRunMetrics()
	serveStart := time.Now()

	// Acquire a process-wide lock on the state directory so two `leather
	// serve` invocations cannot share the same state and silently corrupt
	// each other's queues/caches/run history (T4.1). The lock is held for
	// the lifetime of this process; the kernel releases it on exit.
	serveLockPath := filepath.Join(cfg.StateDir, "leather.lock")
	serveLock, err := acquireProcessLock(serveLockPath)
	if err != nil {
		fmt.Fprintf(stderr, "leather serve: another process holds %s (or it cannot be created): %v\n", serveLockPath, err)
		return 2
	}
	defer releaseProcessLock(serveLock)

	// Resolve and prepare the run-history directory (Phase 1 persistence).
	runHistDir := cfg.RunHistoryDir
	if runHistDir == "" {
		runHistDir = filepath.Join(cfg.StateDir, "runs")
	}
	if cfg.PersistRuns {
		if err := os.MkdirAll(runHistDir, 0700); err != nil {
			log.Warn("failed to create run history dir", "dir", runHistDir, "error", err)
		}
	}

	// Load tool registry from the configured skill directory.
	toolReg, err := tool.Load(cfg.ToolDir)
	if err != nil {
		log.Warn("failed to load tool registry", "dir", cfg.ToolDir, "error", err)
		toolReg = tool.NewRegistry()
	}

	// Set up the queue manager for agents that pull from file queues.
	queueMgr := queue.NewManager(filepath.Join(cfg.StateDir, "queues"))

	// Set up the response cache.
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(cfg.StateDir, "cache")
	}
	agentCache, err := cache.NewFileCache(cacheDir)
	if err != nil {
		log.Warn("failed to init response cache", "dir", cacheDir, "error", err)
		agentCache = nil
	}

	// Load and start worker supervisor if a worker directory is configured.
	workerDefs, err := worker.LoadDir(cfg.WorkerDir)
	if err != nil {
		log.Warn("failed to load workers", "dir", cfg.WorkerDir, "error", err)
	}
	sup := worker.NewSupervisor(workerDefs, queueMgr, log)

	// Build messaging notifiers from config.
	notifiers, notifyErrs := notify.BuildMap(cfg.NotifyBackends)
	for _, e := range notifyErrs {
		log.Warn("notify backend init failed", "error", e)
	}
	if len(notifiers) > 0 {
		log.Info("notify backends loaded", "count", len(notifiers))
	}

	// Load and start MCP server registry.
	mcpServersFile := cfg.MCPServersFile
	if mcpServersFile == "" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			mcpServersFile = filepath.Join(home, ".leather", "mcp-servers.yaml")
		}
	}
	mcpConfigs, mcpLoadErr := mcp.LoadServers(mcpServersFile)
	if mcpLoadErr != nil {
		log.Warn("failed to load MCP servers", "file", mcpServersFile, "error", mcpLoadErr)
		mcpConfigs = nil
	}
	mcpReg := mcp.NewRegistry(mcpConfigs, log)
	if len(mcpConfigs) > 0 {
		startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
		if startErr := mcpReg.StartAll(startCtx); startErr != nil {
			log.Warn("some MCP servers failed to start", "error", startErr)
		}
		startCancel()
		log.Info("MCP servers started", "count", len(mcpConfigs))
		defer mcpReg.StopAll()
	}
	devtoolsBus := bus.New(4096)
	devtoolsSrc := sources.Wire(devtoolsBus, sources.Deps{})

	var toolLimiter *tool.HostLimiter
	if len(cfg.ToolRateLimits) > 0 {
		var limErr error
		toolLimiter, limErr = tool.NewHostLimiter(cfg.ToolRateLimits)
		if limErr != nil {
			log.Warn("tool rate limits: invalid config, rate limiting disabled", "error", limErr)
			toolLimiter = nil
		}
	}

	regDeps := agentRegDeps{
		sched:          sched,
		metrics:        metrics,
		toolReg:        toolReg,
		agentCache:     agentCache,
		queueMgr:       queueMgr,
		notifiers:      notifiers,
		mcpReg:         mcpReg,
		toolLimiter:    toolLimiter,
		cfg:            cfg,
		log:            log,
		stdout:         stdout,
		runHistDir:     runHistDir,
		statJobs:       &statJobs,
		statPrompt:     &statPrompt,
		statCompletion: &statCompletion,
		onJobDone:      onJobDone,
		devtoolsSrc:    devtoolsSrc,
		devtoolsBus:    devtoolsBus,
		agentHashes:    make(map[string]string),
	}
	for _, a := range agents {
		if !a.Enabled {
			log.Info("agent disabled, skipping", "agent", a.Name)
			continue
		}
		if err := registerAgentJob(regDeps, a); err != nil {
			log.Warn("failed to register agent", "agent", a.Name, "error", err)
			continue
		}
		regDeps.agentHashes[a.Name] = hashAgentFiles(a)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	schedErr := make(chan error, 1)
	go func() { schedErr <- sched.Start(ctx) }()

	// currentSup is the active worker supervisor, replaced on SIGHUP.
	var supMu sync.Mutex
	currentSup := sup

	// Start worker supervisor (no-op if no worker definitions were loaded).
	sup.Start(ctx)

	// T1.10: generate a per-launch DevTools bearer token, persist it to a
	// 0600 file under state-dir, and print the access URL so the operator
	// can copy it into their browser. Failing here is non-fatal — the
	// devtools UI simply remains unreachable until a token is provided.
	devtoolsToken, tokenErr := generateDevtoolsToken()
	// printDevtoolsBanner is invoked AFTER the tannery hierarchy below, so
	// the startup banner reads: hierarchy → devtools → start. Capturing it
	// here as a closure keeps the token-generation/persistence logic next to
	// its error handling while letting the print ordering live independently.
	printDevtoolsBanner := func() {}
	if tokenErr != nil {
		log.Warn("devtools: token generation failed; devtools UI will be unreachable", "error", tokenErr)
	} else {
		tokenPath := filepath.Join(cfg.StateDir, "devtools.token")
		if werr := os.WriteFile(tokenPath, []byte(devtoolsToken+"\n"), 0o600); werr != nil {
			log.Warn("devtools: writing token file failed", "path", tokenPath, "error", werr)
		}
		// Only print the Devtools URL when the API server will actually start.
		// Without --api / api: true, the HTTP listener is never bound and the
		// URL would 404/refuse-connect — misleading the operator.
		if cfg.API {
			url := fmt.Sprintf("http://%s/ui/devtools.html#token=%s", clickableHost(cfg.APIAddr), devtoolsToken)
			detail := dim(fmt.Sprintf("(also at %s)", tokenPath))
			if cfg.RunDuration > 0 {
				detail += dim(fmt.Sprintf("  exits in %s; use `make view EX=NN` to keep it open", cfg.RunDuration))
			}
			printDevtoolsBanner = func() {
				if cfg.Pretty {
					// URL must not be wrapped — a mid-line break would split the
					// access token and make the link non-functional on copy.
					prettyWriteEntryNoWrap(stderr,
						time.Now().Format("15:04:05"),
						boldCyan(prettyPadLabel("devtools")),
						[]string{url, detail})
				} else {
					durationHint := ""
					if cfg.RunDuration > 0 {
						durationHint = fmt.Sprintf(" — server exits in %s; use `make view EX=NN` to keep it open", cfg.RunDuration)
					}
					fmt.Fprintf(stderr, "Devtools: %s (also at %s)%s\n", url, tokenPath, durationHint)
				}
			}
		} else {
			printDevtoolsBanner = func() {
				if cfg.Pretty {
					prettyWriteEntry(stderr,
						time.Now().Format("15:04:05"),
						boldCyan(prettyPadLabel("devtools")),
						[]string{dim("disabled (HTTP API not enabled — pass --api or set api: true to enable)")})
				} else {
					fmt.Fprintf(stderr, "Devtools UI: disabled (HTTP API not enabled — pass --api or set api: true to enable)\n")
				}
			}
		}
	}

	deps := apiDeps{
		sched:     sched,
		metrics:   metrics,
		cfg:       cfg,
		startedAt: serveStart,
		version:   version,
		commit:    commit,
		log:       log,
		queueMgr:  queueMgr,
		getWorkerSup: func() *worker.Supervisor {
			supMu.Lock()
			defer supMu.Unlock()
			return currentSup
		},
		cacheDir:      cacheDir,
		runHistDir:    runHistDir,
		devtoolsBus:   devtoolsBus,
		devtoolsSrc:   devtoolsSrc,
		devtoolsToken: devtoolsToken,
		agents:        agents,
		toolReg:       toolReg,
		agentCache:    agentCache,
		notifiers:     notifiers,
		mcpReg:        mcpReg,
	}

	// Tannery mode is independent of the API server.
	// Start the curing supervisor whenever --tannery is configured.
	if cfg.TanneryFile != "" {
		// Serialize concurrent curing-job pretty output on stdout.
		var tanneryPrintMu sync.Mutex
		deps.onCuringJobDone = func(ag model.Agent, rec model.RunRecord, art model.Artifact, events []runner.ProgressEvent) {
			atomic.AddInt64(&statJobs, 1)
			atomic.AddInt64(&statPrompt, int64(rec.Tokens.Prompt))
			atomic.AddInt64(&statCompletion, int64(rec.Tokens.Response))
			onJobDone()
			// Run-record persistence is handled by RunnerDeps.OnRunRecord (set in
			// initTannery) so failures — which never reach OnComplete — are also
			// captured.
			if cfg.Pretty {
				tanneryPrintMu.Lock()
				defer tanneryPrintMu.Unlock()
				ts := time.Now().Format("15:04:05")
				// The captured events already include the system-prompt event fired
				// by the runner, so we do not print it again here.
				printer := newPrettyRunPrinter(stdout, cfg)
				for _, ev := range events {
					printer.Progress(ev)
				}
				printer.Render(ag, rec)
				// Footer: artifact produced by this curing run.
				prettyWriteEntry(stdout, ts, dim(prettyPadLabel("artifact")), []string{
					art.ID + "  " + dim("("+art.HideKind+")"),
				})
			}
		}
		deps.onCuringEvent = func(ev curing.TanneryEvent) {
			if deps.devtoolsSrc != nil {
				deps.devtoolsSrc.PublishTannery(ev)
			}
			if cfg.Pretty {
				tanneryPrintMu.Lock()
				printTanneryEvent(stdout, ev)
				tanneryPrintMu.Unlock()
			}
		}
		td, tannErr := initTannery(ctx, cfg.TanneryFile, &deps)
		if tannErr != nil {
			log.Error("tannery init failed", "error", tannErr)
			return 1
		}
		deps.tannery = td
		if td != nil {
			defer drainTannery(td)
		}
		// (3) tannery → curing → agent hierarchy.
		printStartupHierarchy(stdout, cfg, agents, td)
	} else {
		// No tannery: still emit the agents block so the operator sees what
		// is loaded. printStartupHierarchy handles td == nil by skipping the
		// tannery row and listing every agent as a standalone entry.
		printStartupHierarchy(stdout, cfg, agents, nil)
	}

	// (4) devtools URL.
	printDevtoolsBanner()

	var apiServer *http.Server
	if cfg.API {
		apiServer = startAPIServer(deps)
	}

	// (5) final start banner — the runtime is up and waiting for jobs.
	if cfg.Pretty {
		prettyWriteEntry(stdout,
			time.Now().Format("15:04:05"),
			boldCyan(prettyPadLabel("leather")),
			[]string{dim("start — scheduler running, awaiting jobs (Ctrl-C to stop)")})
	} else {
		fmt.Fprintln(stdout, "leather: start — scheduler running, awaiting jobs (Ctrl-C to stop)")
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	reloadCh := make(chan os.Signal, 1)
	signal.Notify(reloadCh, syscall.SIGHUP)

loop:
	for {
		select {
		case s := <-sig:
			log.Info("received signal, shutting down", "signal", s)
			break loop
		case err := <-schedErr:
			if err != nil {
				log.Error("scheduler error", "error", err)
			}
			break loop
		case <-jobsDone:
			log.Info("max jobs reached, shutting down", "max_jobs", cfg.MaxJobs)
			break loop
		case <-runTimer:
			log.Info("run duration elapsed, shutting down", "duration", cfg.RunDuration)
			break loop
		case <-reloadCh:
			reloadAgentsAndWorkers(ctx, regDeps, &supMu, &currentSup)
		}
	}

	// Drain in-flight work BEFORE cancelling the run context so jobs can
	// finish their current turn cleanly instead of aborting mid-LLM call
	// (T2.3). Use a bounded drain timeout; after that, fall through to the
	// hard cancel that follows.
	if err := sched.Drain(30 * time.Second); err != nil {
		log.Warn("drain timeout", "error", err)
	}
	// T4.11: snapshot currentSup under the same mutex that reload uses, so
	// shutdown does not race with a SIGHUP-driven swap.
	supMu.Lock()
	supToDrain := currentSup
	supMu.Unlock()
	supToDrain.Drain()
	cancel()
	if apiServer != nil {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = apiServer.Shutdown(shutCtx)
	}
	log.Info("shutdown complete")
	if cfg.Stats {
		pt := atomic.LoadInt64(&statPrompt)
		ct := atomic.LoadInt64(&statCompletion)
		fmt.Fprintf(stdout, "\n%s\n", dim("─── token statistics ────────────────────"))
		fmt.Fprintf(stdout, "  %s  %s\n", dim("jobs       "), bold(fmt.Sprintf("%d", atomic.LoadInt64(&statJobs))))
		fmt.Fprintf(stdout, "  %s  %s\n", dim("prompt     "), bold(fmt.Sprintf("%d", pt)))
		fmt.Fprintf(stdout, "  %s  %s\n", dim("completion "), bold(fmt.Sprintf("%d", ct)))
		fmt.Fprintf(stdout, "  %s  %s\n", dim("total      "), bold(fmt.Sprintf("%d", pt+ct)))
		fmt.Fprintf(stdout, "%s\n", dim("─────────────────────────────────────────"))
	}
	return 0
}

// resolveAgent applies global config defaults to agent fields not set in the lifecycle file.
//
// Priority: lifecycle value > config.yaml global > built-in default.
// Fields resolved here: Model, Temperature, Timeout.
func resolveAgent(cfg model.Config, a model.Agent) model.Agent {
	if a.Model == "" {
		a.Model = cfg.Model
	}
	if a.Temperature == 0 {
		a.Temperature = cfg.Temperature
	}
	if a.Timeout == 0 {
		a.Timeout = cfg.LLMTimeout
	}
	if len(cfg.DefaultToolsets) > 0 {
		seen := make(map[string]bool, len(cfg.DefaultToolsets)+len(a.Toolsets))
		merged := make([]string, 0, len(cfg.DefaultToolsets)+len(a.Toolsets))
		for _, name := range cfg.DefaultToolsets {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			merged = append(merged, name)
		}
		for _, name := range a.Toolsets {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			merged = append(merged, name)
		}
		a.Toolsets = merged
	}
	return a
}

// executeAgent runs a single agent turn: build a session, add the system prompt,
// send to the model, and log the result. Returns the raw LLMResponse for the caller
// to use for pretty printing and statistics.
func executeAgent(ctx context.Context, a model.Agent, budget model.TokenBudget, client session.LLMClient, log *logging.Logger) (model.LLMResponse, error) {
	log.Info("executing agent", "agent", a.Name)
	log.Debug("agent config", "agent", a.Name, "model", a.Model, "timeout", a.Timeout, "temperature", a.Temperature, "max_tokens", budget.MaxTokens, "completion_reserve", budget.CompletionReserve)
	sess := session.New(budget, a.Model, client)

	if a.SystemPrompt != "" {
		log.Debug("adding system prompt", "agent", a.Name, "chars", len(a.SystemPrompt))
		if err := sess.Add(ctx, model.Message{Role: "system", Content: a.SystemPrompt}); err != nil {
			return model.LLMResponse{}, fmt.Errorf("executeAgent %s: add system prompt: %w", a.Name, err)
		}
	}

	if a.UserPrompt != "" {
		log.Debug("adding user prompt", "agent", a.Name, "chars", len(a.UserPrompt))
		if err := sess.Add(ctx, model.Message{Role: "user", Content: a.UserPrompt}); err != nil {
			return model.LLMResponse{}, fmt.Errorf("executeAgent %s: add user prompt: %w", a.Name, err)
		}
	}

	callCtx, callCancel := context.WithTimeout(ctx, a.Timeout)
	defer callCancel()

	opts := session.CompletionOptions{
		MaxTokens:   budget.CompletionReserve,
		Temperature: a.Temperature,
	}
	log.Debug("calling LLM", "agent", a.Name, "model", a.Model, "messages", len(sess.Messages()), "timeout", a.Timeout)
	// Log full payload at debug level for API call verification.
	for i, m := range sess.Messages() {
		log.Debug("api payload", "agent", a.Name, "index", i, "role", m.Role, "tokens", m.Tokens, "content", m.Content)
	}
	resp, err := client.Complete(callCtx, a.Model, sess.Messages(), opts)
	if err != nil {
		return model.LLMResponse{}, fmt.Errorf("executeAgent %s: %w", a.Name, err)
	}
	log.Info("agent completed", "agent", a.Name, "tokens", resp.TotalTokens, "finish_reason", resp.FinishReason)
	log.Debug("agent response", "agent", a.Name, "prompt_tokens", resp.PromptTokens, "completion_tokens", resp.CompletionTokens)
	log.Info("agent response content", "agent", a.Name, "content", resp.Content)
	return resp, nil
}

// resolveTokenBudget returns a TokenBudget for a, overriding max_tokens if set.
func resolveTokenBudget(cfg model.Config, a model.Agent) model.TokenBudget {
	maxTokens := cfg.MaxTokens
	if a.MaxTokens > 0 {
		maxTokens = a.MaxTokens
	}
	return model.TokenBudget{
		MaxTokens:          maxTokens,
		CompletionReserve:  cfg.CompletionReserve,
		SummarizeThreshold: cfg.SummarizeThreshold,
	}
}

// agentRegDeps holds the shared resources required to register a scheduled agent job.
// All fields are set once at serve startup and are safe for concurrent use.
type agentRegDeps struct {
	sched          *scheduler.Scheduler
	metrics        *runMetrics
	toolReg        *tool.Registry
	agentCache     *cache.FileCache
	queueMgr       *queue.Manager
	notifiers      map[string]notify.Notifier
	mcpReg         *mcp.Registry
	toolLimiter    *tool.HostLimiter
	cfg            model.Config
	log            *logging.Logger
	stdout         io.Writer
	runHistDir     string
	statJobs       *int64
	statPrompt     *int64
	statCompletion *int64
	onJobDone      func()
	devtoolsSrc    *sources.Wiring
	devtoolsBus    *bus.Bus
	// agentHashes tracks the content hash of each currently registered agent's
	// source files so SIGHUP reload (T2.7) can detect in-place edits. Map is
	// mutated only from the serve loop's reload handler, which is serial.
	agentHashes map[string]string
}

// registerAgentJob resolves and registers a single agent with the scheduler.
// Returns an error if the agent has no model configured or if scheduler registration fails.
func registerAgentJob(deps agentRegDeps, a model.Agent) error {
	agentCopy := resolveAgent(deps.cfg, a)
	if agentCopy.Model == "" {
		return fmt.Errorf("agent %q has no model configured (set model in lifecycle or config.yaml)", agentCopy.Name)
	}
	budget := resolveTokenBudget(deps.cfg, agentCopy)
	agentRunner := &runner.Runner{
		Client:        session.NewHTTPClient(deps.cfg.LLMEndpoint, deps.cfg.LLMAPIKey, deps.cfg.LLMTimeout),
		Registry:      deps.toolReg,
		Log:           deps.log,
		MaxToolRounds: deps.cfg.MaxToolRounds,
		Cache:         deps.agentCache,
		QueueMgr:      deps.queueMgr,
		Notifiers:     deps.notifiers,
		MCPRegistry:   deps.mcpReg,
		ToolLimiter:   deps.toolLimiter,
	}
	deps.metrics.registerAgent(agentCopy)
	// Curing-driven agents have no schedule or queue input; the curing worker wakes
	// them directly. Skip scheduler registration — it would fail with an empty cron
	// expression and these agents are not dispatcher-scheduled.
	if agentCopy.Schedule == "" && agentCopy.QueueInput == "" {
		return nil
	}
	return deps.sched.Register(agentCopy.Name, agentCopy.Schedule, func(ctx context.Context, job model.Job) error {
		if agentCopy.QueueInput != "" {
			// Batch queue processing with optional dead-letter promotion.
			batchSize := agentCopy.QueueBatchSize
			if batchSize < 1 {
				batchSize = 1
			}
			q, qErr := deps.queueMgr.Get(agentCopy.QueueInput)
			if qErr != nil {
				deps.log.Warn("queue get failed, skipping tick", "agent", agentCopy.Name, "queue", agentCopy.QueueInput, "error", qErr)
				deps.onJobDone()
				return nil
			}
			for i := 0; i < batchSize; i++ {
				item, ok, qErr := q.Dequeue()
				if qErr != nil {
					deps.log.Warn("dequeue failed, skipping item", "agent", agentCopy.Name, "queue", agentCopy.QueueInput, "error", qErr)
					break
				}
				if !ok {
					break // queue empty
				}
				jobRunner, prettyPrinter := configurePrettyRunner(*agentRunner, deps.stdout, deps.cfg)
				if deps.devtoolsSrc != nil {
					queueRunSeq := deps.devtoolsSrc.PublishQueueRun(agentCopy.Name, agentCopy.QueueInput, item)
					baseProgress := jobRunner.ProgressFn
					jobRunner.ProgressFn = func(ev runner.ProgressEvent) {
						evSeq := deps.devtoolsSrc.PublishRunner("", agentCopy.Name, ev)
						if queueRunSeq != 0 && deps.devtoolsBus != nil {
							deps.devtoolsBus.AppendCause(queueRunSeq, evSeq)
						}
						if baseProgress != nil {
							baseProgress(ev)
						}
					}
				}
				runData := runner.BuildRunData(agentCopy)
				// Merge queue payload on top of built-in vars (payload wins on collision).
				for k, v := range item.Payload {
					runData[k] = v
				}
				expanded, expErr := runner.ExpandPromptPayload(agentCopy, runData)
				if expErr != nil {
					deps.log.Warn("prompt expansion failed, skipping item", "agent", agentCopy.Name, "error", expErr)
					continue
				}
				rec, runErr := jobRunner.Run(ctx, expanded, budget)
				if runErr != nil {
					if prettyPrinter != nil {
						prettyPrinter.Stop()
					}
					deps.log.Error("agent execution failed", "agent", agentCopy.Name, "error", runErr)
					item.AttemptCount++
					if agentCopy.QueueMaxAttempts > 0 && item.AttemptCount >= agentCopy.QueueMaxAttempts {
						dlqName := agentCopy.QueueInput + "-dlq"
						if reErr := deps.queueMgr.Enqueue(dlqName, item); reErr != nil {
							deps.log.Warn("DLQ enqueue failed", "agent", agentCopy.Name, "queue", dlqName, "error", reErr)
						} else {
							deps.log.Warn("agent exceeded max attempts, moved to DLQ", "agent", agentCopy.Name, "item", item.ID)
						}
					} else {
						if reErr := deps.queueMgr.Enqueue(agentCopy.QueueInput, item); reErr != nil {
							deps.log.Warn("re-enqueue failed", "agent", agentCopy.Name, "error", reErr)
						} else {
							deps.log.Warn("run failed, re-enqueuing item", "agent", agentCopy.Name, "item", item.ID, "attempt", item.AttemptCount)
						}
					}
				} else {
					if prettyPrinter != nil {
						prettyPrinter.Render(agentCopy, rec)
					}
					if deps.cfg.Stats {
						atomic.AddInt64(deps.statJobs, 1)
						atomic.AddInt64(deps.statPrompt, int64(rec.Tokens.Prompt))
						atomic.AddInt64(deps.statCompletion, int64(rec.Tokens.Response))
					}
				}
				deps.metrics.record(rec)
				if deps.cfg.PersistRuns {
					if err := persistRunRecord(deps.runHistDir, rec, deps.cfg.RunMaxBytes); err != nil {
						deps.log.Warn("persist run record failed", "agent", rec.AgentName, "error", err)
					}
				}
			}
			deps.onJobDone()
			return nil
		}
		// Non-queue path: run the agent with built-in template variables.
		runData := runner.BuildRunData(agentCopy)
		expanded, expErr := runner.ExpandPromptPayload(agentCopy, runData)
		if expErr != nil {
			deps.log.Warn("prompt expansion failed, skipping tick", "agent", agentCopy.Name, "error", expErr)
			deps.onJobDone()
			return nil
		}
		jobRunner, prettyPrinter := configurePrettyRunner(*agentRunner, deps.stdout, deps.cfg)
		if deps.devtoolsSrc != nil {
			deps.devtoolsSrc.PublishScheduleFire(agentCopy.Name, agentCopy.Schedule)
			baseProgress := jobRunner.ProgressFn
			jobRunner.ProgressFn = func(ev runner.ProgressEvent) {
				deps.devtoolsSrc.PublishRunner("", agentCopy.Name, ev)
				if baseProgress != nil {
					baseProgress(ev)
				}
			}
		}
		rec, err := jobRunner.Run(ctx, expanded, budget)
		if err != nil {
			if prettyPrinter != nil {
				prettyPrinter.Stop()
			}
			deps.log.Error("agent execution failed", "agent", agentCopy.Name, "error", err)
		} else {
			if prettyPrinter != nil {
				prettyPrinter.Render(agentCopy, rec)
			}
			if deps.cfg.Stats {
				atomic.AddInt64(deps.statJobs, 1)
				atomic.AddInt64(deps.statPrompt, int64(rec.Tokens.Prompt))
				atomic.AddInt64(deps.statCompletion, int64(rec.Tokens.Response))
			}
		}
		deps.metrics.record(rec)
		if deps.cfg.PersistRuns {
			if err := persistRunRecord(deps.runHistDir, rec, deps.cfg.RunMaxBytes); err != nil {
				deps.log.Warn("persist run record failed", "agent", rec.AgentName, "error", err)
			}
		}
		deps.onJobDone()
		return err
	})
}

// reloadAgentsAndWorkers re-scans the agent and worker directories and applies changes
// to the running scheduler and worker supervisor. Running jobs are not interrupted.
// Config file values are not reloaded.
func reloadAgentsAndWorkers(ctx context.Context, regDeps agentRegDeps, supMu *sync.Mutex, currentSup **worker.Supervisor) {
	log := regDeps.log
	cfg := regDeps.cfg

	// --- Agents ---
	newAgents, errs := agent.LoadDir(cfg.AgentDir)
	for _, e := range errs {
		log.Warn("reload: agent load error", "error", e)
	}

	// Build the current job set from the scheduler.
	currentJobs := make(map[string]bool)
	for _, j := range regDeps.sched.Jobs() {
		currentJobs[j.AgentName] = true
	}

	// Build the new enabled agent set.
	newAgentSet := make(map[string]model.Agent)
	for _, a := range newAgents {
		if a.Enabled {
			newAgentSet[a.Name] = a
		}
	}

	// Deregister agents that no longer exist.
	for name := range currentJobs {
		if _, ok := newAgentSet[name]; !ok {
			if err := regDeps.sched.Deregister(name); err != nil {
				log.Warn("reload: deregister failed", "agent", name, "error", err)
			} else {
				log.Info("reload: agent removed", "agent", name)
				delete(regDeps.agentHashes, name)
			}
		}
	}

	// Register new agents; re-register agents whose source-file content changed (T2.7).
	for name, a := range newAgentSet {
		newHash := hashAgentFiles(a)
		if currentJobs[name] {
			if regDeps.agentHashes[name] == newHash {
				log.Info("reload: agent unchanged", "agent", name)
				continue
			}
			if err := regDeps.sched.Deregister(name); err != nil {
				log.Warn("reload: deregister-for-update failed", "agent", name, "error", err)
				continue
			}
			if err := registerAgentJob(regDeps, a); err != nil {
				log.Warn("reload: re-register failed", "agent", name, "error", err)
				delete(regDeps.agentHashes, name)
				continue
			}
			regDeps.agentHashes[name] = newHash
			log.Info("reload: agent updated", "agent", name)
			continue
		}
		if err := registerAgentJob(regDeps, a); err != nil {
			log.Warn("reload: register failed", "agent", name, "error", err)
			continue
		}
		regDeps.agentHashes[name] = newHash
		log.Info("reload: agent added", "agent", name)
	}

	// --- Workers ---
	workerDefs, err := worker.LoadDir(cfg.WorkerDir)
	if err != nil {
		log.Warn("reload: worker load error", "error", err)
	}
	// T2.8: drain the old supervisor WITHOUT holding supMu, then take the
	// lock only to swap pointers. Draining can take ~seconds; holding supMu
	// during Drain blocks status endpoints and other reads of currentSup.
	supMu.Lock()
	old := *currentSup
	supMu.Unlock()
	if old != nil {
		old.Drain()
	}
	newSup := worker.NewSupervisor(workerDefs, regDeps.queueMgr, log)
	newSup.Start(ctx)
	supMu.Lock()
	*currentSup = newSup
	supMu.Unlock()
	log.Info("reload: workers restarted", "count", len(workerDefs))
}

// hashAgentFiles returns a hex sha256 hash of the agent's source files
// (the *.agent.md and *.lifecycle.yaml when present). Missing files are
// represented by a sentinel so a transition from "present" to "missing"
// reads as a change.
func hashAgentFiles(a model.Agent) string {
	h := sha256.New()
	for _, p := range []string{a.SourcePath, a.LifecycleSourcePath} {
		if p == "" {
			h.Write([]byte("\x00:missing\x00"))
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			h.Write([]byte("\x00:unreadable\x00"))
			continue
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// buildLogger constructs a Logger writing to w according to the cfg log format.
func buildLogger(cfg model.Config, w io.Writer) *logging.Logger {
	if cfg.LogFormat == "json" {
		return logging.NewWithWriter("leather", cfg.LogLevel, w, true)
	}
	return logging.NewWithWriter("leather", cfg.LogLevel, w, false)
}

// startAPIServer binds the HTTP status API and returns the server.
func startAPIServer(deps apiDeps) *http.Server {
	srv := &http.Server{
		Addr:    deps.cfg.APIAddr,
		Handler: apiMux(deps),
		// Conservative timeouts to mitigate slow-loris and resource-exhaustion
		// attacks against the local API (T4.5). WriteTimeout is generous to
		// accommodate the SSE stream on /api/devtools/events; that endpoint
		// runs on a sibling mux with its own (longer) timeout if needed.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	// Warn loudly when the API is bound to a non-loopback address (T1.12).
	// The local API has no authentication and assumes loopback; binding it
	// to 0.0.0.0 or a public address exposes admin endpoints to the network.
	warnIfNonLoopbackBind(deps.cfg.APIAddr, deps.log)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			deps.log.Error("API server error", "error", err)
		}
	}()
	deps.log.Info("HTTP API listening", "addr", deps.cfg.APIAddr)
	return srv
}

// warnIfNonLoopbackBind emits a SECURITY warning to the logger (and stderr)
// when addr binds to anything other than localhost. Best-effort host parsing;
// failure to parse is silently ignored.
func warnIfNonLoopbackBind(addr string, log *logging.Logger) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		emitInsecureBindWarning(addr, log)
		return
	}
	ip := net.ParseIP(host)
	if ip != nil && !ip.IsLoopback() {
		emitInsecureBindWarning(addr, log)
	}
}

func emitInsecureBindWarning(addr string, log *logging.Logger) {
	const msg = "[SECURITY] HTTP API is bound to a non-loopback address with no authentication; " +
		"anyone on the network can reach admin endpoints. Bind to 127.0.0.1 in production."
	log.Warn(msg, "addr", addr)
	fmt.Fprintf(os.Stderr, "%s addr=%s\n", msg, addr)
}

// statusResponse is the JSON shape returned by GET /status.
type statusResponse struct {
	StartedAt         int64   `json:"started_at"`
	UptimeSeconds     int64   `json:"uptime_seconds"`
	Version           string  `json:"version"`
	Commit            string  `json:"commit"`
	LLMEndpoint       string  `json:"llm_endpoint"`
	AgentCount        int     `json:"agent_count"`
	SchedulerTick     string  `json:"scheduler_tick"`
	MaxConcurrentJobs int     `json:"max_concurrent_jobs"`
	ReplayMode        bool    `json:"replay_mode,omitempty"`
	ReplayCapturedAt  int64   `json:"replay_captured_at,omitempty"`
	ReplayLiveMode    bool    `json:"replay_live_mode,omitempty"`
	ReplayClockAt     int64   `json:"replay_clock_at,omitempty"`
	ReplaySpeed       float64 `json:"replay_speed,omitempty"`
	ReplayPaused      bool    `json:"replay_paused,omitempty"`
}

// configResponse is the JSON shape returned by GET /config.
// It is an explicit allowlist — not a direct serialisation of model.Config.
type configResponse struct {
	AgentDir           string  `json:"agent_dir"`
	LogLevel           string  `json:"log_level"`
	LogFormat          string  `json:"log_format"`
	Model              string  `json:"model"`
	Temperature        float64 `json:"temperature"`
	MaxTokens          int     `json:"max_tokens"`
	CompletionReserve  int     `json:"completion_reserve"`
	SummarizeThreshold float64 `json:"summarize_threshold"`
	LLMEndpoint        string  `json:"llm_endpoint"`
	LLMTimeout         string  `json:"llm_timeout"`
	SchedulerTick      string  `json:"scheduler_tick"`
	MaxConcurrentJobs  int     `json:"max_concurrent_jobs"`
	APIAddr            string  `json:"api_addr"`
}

// metricsResponse is the JSON shape returned by GET /metrics.
type metricsResponse struct {
	Agents map[string]agentMetricSummary `json:"agents"`
	// Outbound tool resilience counters (issues #7–#9).
	ToolRetryTotal         int64 `json:"leather_tool_retry_total"`
	ToolBackoffTotal       int64 `json:"leather_tool_backoff_total"`
	ToolRateLimitWaitTotal int64 `json:"leather_tool_rate_limit_wait_total"`
	OutboundDLQDepth       int   `json:"leather_outbound_dlq_depth"`
}

// snapshotResponse is the JSON shape of GET /snapshot and of snapshot files on disk.
type snapshotResponse struct {
	Version    string                        `json:"version"`
	Commit     string                        `json:"commit"`
	CapturedAt int64                         `json:"captured_at"`
	Config     configResponse                `json:"config"`
	Jobs       []model.Job                   `json:"jobs"`
	Metrics    map[string]agentMetricSummary `json:"metrics"`
	History    []model.RunRecord             `json:"history"`
}

// buildConfigResponse extracts the allowlist of config fields for API responses.
func buildConfigResponse(c model.Config) configResponse {
	return configResponse{
		AgentDir:           c.AgentDir,
		LogLevel:           string(c.LogLevel),
		LogFormat:          c.LogFormat,
		Model:              c.Model,
		Temperature:        c.Temperature,
		MaxTokens:          c.MaxTokens,
		CompletionReserve:  c.CompletionReserve,
		SummarizeThreshold: c.SummarizeThreshold,
		LLMEndpoint:        c.LLMEndpoint,
		LLMTimeout:         c.LLMTimeout.String(),
		SchedulerTick:      c.SchedulerTick.String(),
		MaxConcurrentJobs:  c.MaxConcurrentJobs,
		APIAddr:            c.APIAddr,
	}
}

// corsMiddleware adds permissive CORS headers for local-development access.
// The API is bound to localhost by default; wildcard origin is acceptable here.
func corsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// generateDevtoolsToken returns 32 random bytes hex-encoded for use as a
// per-launch DevTools bearer token.
func generateDevtoolsToken() (string, error) {
	tok, err := ids.RandHex(32)
	if err != nil {
		return "", fmt.Errorf("devtools token: %w", err)
	}
	return tok, nil
}

// devtoolsTokenMiddleware gates the wrapped handler with a per-launch bearer
// token (T1.10). When token is empty, gating is disabled. Accepts either an
// `Authorization: Bearer <token>` header (preferred) or a `?token=<hex>` query
// parameter (necessary for EventSource which cannot set headers).
func devtoolsTokenMiddleware(token string, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			h.ServeHTTP(w, r)
			return
		}
		var got string
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			got = strings.TrimPrefix(auth, "Bearer ")
		} else {
			got = r.URL.Query().Get("token")
		}
		// Constant-time compare to avoid token-length side channels.
		if len(got) != len(token) || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="leather-devtools"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// apiMux builds and returns the HTTP handler for the leather API.
// Extracted for testability; startAPIServer wraps it with a real server.
func apiMux(deps apiDeps) http.Handler {
	log := deps.log
	mux := http.NewServeMux()
	if deps.devtoolsBus != nil {
		devtoolsHandler := NewDevtoolsHandler(DevtoolsHandlerDeps{
			Bus:          deps.devtoolsBus,
			QueueMgr:     deps.queueMgr,
			GetWorkerSup: deps.getWorkerSup,
			StartedAt:    deps.startedAt,
			Version:      deps.version,
			Commit:       deps.commit,
		})
		gated := devtoolsTokenMiddleware(deps.devtoolsToken, devtoolsHandler)
		mux.Handle("/api/devtools/", gated)
		mux.Handle("/api/devtools/events", gated)
	}

	// Serve the bundled DevTools / overview UI under /ui/. Assets are
	// embedded into the binary so the operator does not need to know where
	// the repository's ui/ directory lives, and so the URL advertised at
	// startup ("Devtools: http://<addr>/ui/devtools.html#token=...") always
	// resolves. The DevTools UI itself reaches /api/devtools/* on the same
	// origin, so a token-gated UI + token-gated API share one base URL.
	uiFS, err := fs.Sub(ui.Assets(), ".")
	if err == nil {
		mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(uiFS))))
	} else if log != nil {
		log.Warn("devtools UI mount failed", "error", err)
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		// Expanded readiness checks (T6.14):
		//   - state_dir: directory is writable (touch + remove)
		//   - llm_endpoint: configured (not empty); reachability is NOT probed
		//     here because we don't want /healthz itself to make outbound
		//     requests on every poll.
		type check struct {
			OK    bool   `json:"ok"`
			Error string `json:"error,omitempty"`
		}
		checks := map[string]check{}
		ok := true

		// state_dir writability
		if deps.cfg.StateDir == "" {
			checks["state_dir"] = check{OK: false, Error: "state_dir not configured"}
			ok = false
		} else {
			probe := filepath.Join(deps.cfg.StateDir, ".healthz-probe")
			if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
				checks["state_dir"] = check{OK: false, Error: err.Error()}
				ok = false
			} else {
				_ = os.Remove(probe)
				checks["state_dir"] = check{OK: true}
			}
		}

		// llm_endpoint configured
		if deps.cfg.LLMEndpoint == "" {
			checks["llm_endpoint"] = check{OK: false, Error: "llm_endpoint not configured"}
			ok = false
		} else {
			checks["llm_endpoint"] = check{OK: true}
		}

		status := http.StatusOK
		if !ok {
			status = http.StatusServiceUnavailable
		}
		httpx.WriteJSON(w, status, map[string]any{
			"status": map[bool]string{true: "ok", false: "degraded"}[ok],
			"checks": checks,
		})
	})

	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		var jobs []model.Job
		if deps.replay != nil {
			jobs = deps.replay.Jobs
		} else if deps.replayLive != nil {
			jobs = nil // no scheduler jobs in live replay
		} else {
			jobs = deps.sched.Jobs()
		}
		if jobs == nil {
			jobs = []model.Job{}
		}
		httpx.WriteJSON(w, http.StatusOK, jobs)
	})

	mux.HandleFunc("/jobs/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/jobs/")
		if name == "" {
			http.NotFound(w, r)
			return
		}
		var jobs []model.Job
		if deps.replay != nil {
			jobs = deps.replay.Jobs
		} else if deps.sched != nil {
			jobs = deps.sched.Jobs()
		}
		for _, j := range jobs {
			if j.AgentName == name {
				httpx.WriteJSON(w, http.StatusOK, j)
				return
			}
		}
		httpx.WriteError(w, http.StatusNotFound, "not found")
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		var body statusResponse
		switch {
		case deps.replay != nil:
			snap := deps.replay
			body = statusResponse{
				StartedAt:         snap.CapturedAt,
				UptimeSeconds:     int64(time.Since(deps.startedAt).Seconds()),
				Version:           snap.Version,
				Commit:            snap.Commit,
				LLMEndpoint:       snap.Config.LLMEndpoint,
				AgentCount:        len(snap.Jobs),
				SchedulerTick:     snap.Config.SchedulerTick,
				MaxConcurrentJobs: snap.Config.MaxConcurrentJobs,
				ReplayMode:        true,
				ReplayCapturedAt:  snap.CapturedAt,
			}
		case deps.replayLive != nil:
			sp, pa := deps.replayLive.speedAndPaused()
			clockAt := deps.replayLive.clock()
			body = statusResponse{
				StartedAt:         deps.startedAt.Unix(),
				UptimeSeconds:     int64(time.Since(deps.startedAt).Seconds()),
				Version:           deps.version,
				Commit:            deps.commit,
				LLMEndpoint:       deps.cfg.LLMEndpoint,
				AgentCount:        len(deps.replayLive.records),
				SchedulerTick:     deps.cfg.SchedulerTick.String(),
				MaxConcurrentJobs: deps.cfg.MaxConcurrentJobs,
				ReplayLiveMode:    true,
				ReplayClockAt:     clockAt,
				ReplaySpeed:       sp,
				ReplayPaused:      pa,
			}
		default:
			body = statusResponse{
				StartedAt:         deps.startedAt.Unix(),
				UptimeSeconds:     int64(time.Since(deps.startedAt).Seconds()),
				Version:           deps.version,
				Commit:            deps.commit,
				LLMEndpoint:       deps.cfg.LLMEndpoint,
				AgentCount:        len(deps.sched.Jobs()),
				SchedulerTick:     deps.cfg.SchedulerTick.String(),
				MaxConcurrentJobs: deps.cfg.MaxConcurrentJobs,
			}
		}
		httpx.WriteJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		var body configResponse
		if deps.replay != nil {
			body = deps.replay.Config
		} else {
			body = buildConfigResponse(deps.cfg)
		}
		httpx.WriteJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		retryTotal, backoffTotal, rateLimitWaitTotal := tool.MetricSnapshot()
		dlqDepth := 0
		if deps.queueMgr != nil {
			dlqDepth = deps.queueMgr.Depth("outbound-dlq")
		}
		var body metricsResponse
		if deps.replay != nil {
			body = metricsResponse{Agents: deps.replay.Metrics}
		} else if deps.replayLive != nil {
			body = metricsResponse{Agents: map[string]agentMetricSummary{}}
		} else {
			body = metricsResponse{Agents: deps.metrics.summaries()}
		}
		body.ToolRetryTotal = retryTotal
		body.ToolBackoffTotal = backoffTotal
		body.ToolRateLimitWaitTotal = rateLimitWaitTotal
		body.OutboundDLQDepth = dlqDepth
		httpx.WriteJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("/history", func(w http.ResponseWriter, r *http.Request) {
		var recs []model.RunRecord
		if deps.replay != nil {
			recs = deps.replay.History
		} else if deps.replayLive != nil {
			recs = deps.replayLive.visible()
		} else {
			recs = deps.metrics.allHistory(500)
		}
		if recs == nil {
			recs = []model.RunRecord{}
		}
		httpx.WriteJSON(w, http.StatusOK, recs)
	})

	mux.HandleFunc("/snapshot", func(w http.ResponseWriter, r *http.Request) {
		var body snapshotResponse
		if deps.replay != nil {
			body = *deps.replay
		} else if deps.replayLive != nil {
			recs := deps.replayLive.visible()
			if recs == nil {
				recs = []model.RunRecord{}
			}
			body = snapshotResponse{
				Version:    deps.version,
				Commit:     deps.commit,
				CapturedAt: time.Now().Unix(),
				Config:     buildConfigResponse(deps.cfg),
				Jobs:       []model.Job{},
				Metrics:    map[string]agentMetricSummary{},
				History:    recs,
			}
		} else {
			recs := deps.metrics.allHistory(0)
			if recs == nil {
				recs = []model.RunRecord{}
			}
			metrics := deps.metrics.summaries()
			// Strip RecentRuns: the full run history is already in History,
			// duplicating it per-agent roughly doubles the snapshot payload.
			for k, v := range metrics {
				v.RecentRuns = nil
				metrics[k] = v
			}
			body = snapshotResponse{
				Version:    deps.version,
				Commit:     deps.commit,
				CapturedAt: time.Now().Unix(),
				Config:     buildConfigResponse(deps.cfg),
				Jobs:       deps.sched.Jobs(),
				Metrics:    metrics,
				History:    recs,
			}
		}
		httpx.WriteJSON(w, http.StatusOK, body)
	})

	// /replay/control — pause, resume, or change speed in replay-live mode.
	mux.HandleFunc("/replay/control", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if deps.replayLive == nil {
			httpx.WriteError(w, http.StatusNotFound, "not in replay-live mode")
			return
		}
		switch r.URL.Query().Get("action") {
		case "pause":
			deps.replayLive.setPaused(true)
		case "resume":
			deps.replayLive.setPaused(false)
		case "speed":
			if s := r.URL.Query().Get("speed"); s != "" {
				if f, err := strconv.ParseFloat(s, 64); err == nil {
					deps.replayLive.setSpeed(f)
				}
			}
		}
		sp, pa := deps.replayLive.speedAndPaused()
		fmt.Fprintf(w, `{"speed":%g,"paused":%t,"clock_at":%d}`, sp, pa, deps.replayLive.clock())
	})

	// /queues — list all known queues with their current length.
	mux.HandleFunc("/queues", func(w http.ResponseWriter, r *http.Request) {
		if deps.queueMgr == nil {
			httpx.WriteJSON(w, http.StatusOK, []any{})
			return
		}
		type queueSummary struct {
			Name string `json:"name"`
			Len  int    `json:"len"`
		}
		names := deps.queueMgr.Names()
		out := make([]queueSummary, 0, len(names))
		for _, name := range names {
			q, err := deps.queueMgr.Get(name)
			if err != nil {
				continue
			}
			out = append(out, queueSummary{Name: name, Len: q.Len()})
		}
		httpx.WriteJSON(w, http.StatusOK, out)
	})

	// /queues/{name} — GET details; POST .../requeue moves DLQ back; DELETE drains.
	mux.HandleFunc("/queues/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/queues/")
		if name == "" {
			http.NotFound(w, r)
			return
		}
		if deps.queueMgr == nil {
			httpx.WriteError(w, http.StatusNotFound, "queue manager not configured")
			return
		}
		// POST /queues/{name}/requeue — move all DLQ items back to the named queue.
		if r.Method == http.MethodPost && strings.HasSuffix(name, "/requeue") {
			queueName := strings.TrimSuffix(name, "/requeue")
			dlqName := queueName + "-dlq"
			dlq, err := deps.queueMgr.Get(dlqName)
			if err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			items, err := dlq.Drain()
			if err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			// T2.4: propagate per-item enqueue errors; roll back failed items
			// back into the DLQ so they are not lost. Report accurate counts.
			succeeded := 0
			type failedItem struct {
				ItemID string `json:"item_id"`
				Error  string `json:"error"`
			}
			var failed []failedItem
			for _, item := range items {
				if reErr := deps.queueMgr.Enqueue(queueName, item); reErr != nil {
					failed = append(failed, failedItem{ItemID: item.ID, Error: reErr.Error()})
					if rbErr := deps.queueMgr.Enqueue(dlqName, item); rbErr != nil {
						log.Error("API /queues requeue rollback failed; item lost", "queue", dlqName, "item", item.ID, "error", rbErr)
					}
					continue
				}
				succeeded++
			}
			status := http.StatusOK
			if len(failed) > 0 {
				status = http.StatusMultiStatus
			}
			httpx.WriteJSON(w, status, map[string]any{
				"requeued": succeeded,
				"failed":   failed,
			})
			return
		}
		// DELETE /queues/{name} — drain the named queue (requires ?confirm=yes).
		if r.Method == http.MethodDelete {
			if r.URL.Query().Get("confirm") != "yes" {
				httpx.WriteError(w, http.StatusBadRequest, "add ?confirm=yes to confirm drain")
				return
			}
			q, err := deps.queueMgr.Get(name)
			if err != nil {
				httpx.WriteError(w, http.StatusNotFound, "not found")
				return
			}
			items, err := q.Drain()
			if err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, err.Error())
				return
			}
			httpx.WriteJSON(w, http.StatusOK, map[string]int{"drained": len(items)})
			return
		}
		// GET /queues/{name} — details for a specific queue including its head item.
		q, err := deps.queueMgr.Get(name)
		if err != nil {
			httpx.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		type queueDetail struct {
			Name string           `json:"name"`
			Len  int              `json:"len"`
			Head *model.QueueItem `json:"head,omitempty"`
		}
		detail := queueDetail{Name: name, Len: q.Len()}
		if item, ok := q.Peek(); ok {
			detail.Head = &item
		}
		httpx.WriteJSON(w, http.StatusOK, detail)
	})

	// cacheStatsResponse is the JSON shape for GET /cache/stats.
	type cacheStatsResponse struct {
		Dir          string `json:"dir"`
		EntryCount   int    `json:"entry_count"`
		ExpiredCount int    `json:"expired_count"`
		SizeBytes    int64  `json:"size_bytes"`
		Capped       bool   `json:"capped,omitempty"`
		Cached       bool   `json:"cached,omitempty"`
	}

	// T4.6: memoize stats for 10 s and cap directory walk at 1000 entries
	// to prevent a hot endpoint from hammering the disk on large caches.
	var (
		cacheStatsMu     sync.Mutex
		cacheStatsResult cacheStatsResponse
		cacheStatsAt     time.Time
	)
	const cacheStatsTTL = 10 * time.Second
	const cacheStatsMaxEntries = 1000

	mux.HandleFunc("/cache/stats", func(w http.ResponseWriter, r *http.Request) {
		if deps.cacheDir == "" {
			httpx.WriteError(w, http.StatusServiceUnavailable, "cache not configured")
			return
		}
		cacheStatsMu.Lock()
		if time.Since(cacheStatsAt) < cacheStatsTTL && !cacheStatsAt.IsZero() {
			cached := cacheStatsResult
			cached.Cached = true
			cacheStatsMu.Unlock()
			httpx.WriteJSON(w, http.StatusOK, cached)
			return
		}
		cacheStatsMu.Unlock()
		entries, err := os.ReadDir(deps.cacheDir)
		if err != nil {
			if os.IsNotExist(err) {
				httpx.WriteJSON(w, http.StatusOK, cacheStatsResponse{Dir: deps.cacheDir})
				return
			}
			httpx.WriteError(w, http.StatusInternalServerError, "could not read cache dir")
			return
		}
		var stats cacheStatsResponse
		stats.Dir = deps.cacheDir
		now := time.Now().Unix()
		visited := 0
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if visited >= cacheStatsMaxEntries {
				stats.Capped = true
				break
			}
			visited++
			info, infoErr := e.Info()
			if infoErr != nil {
				continue
			}
			stats.SizeBytes += info.Size()
			stats.EntryCount++
			data, readErr := os.ReadFile(filepath.Join(deps.cacheDir, e.Name()))
			if readErr != nil {
				continue
			}
			var entry struct {
				ExpiresAt int64 `json:"expires_at"`
			}
			if json.Unmarshal(data, &entry) == nil && entry.ExpiresAt != 0 && now >= entry.ExpiresAt {
				stats.ExpiredCount++
			}
		}
		cacheStatsMu.Lock()
		cacheStatsResult = stats
		cacheStatsAt = time.Now()
		cacheStatsMu.Unlock()
		httpx.WriteJSON(w, http.StatusOK, stats)
	})

	mux.HandleFunc("/workers", func(w http.ResponseWriter, r *http.Request) {
		statuses := []worker.WorkerStatus{}
		if deps.getWorkerSup != nil {
			if sup := deps.getWorkerSup(); sup != nil {
				if ws := sup.Workers(); ws != nil {
					statuses = ws
				}
			}
		}
		httpx.WriteJSON(w, http.StatusOK, statuses)
	})

	var handler http.Handler = mux
	if deps.replay != nil || deps.replayLive != nil {
		handler = replayHeader(mux)
	}
	registerTanneryHandlers(mux, deps.tannery, &deps)
	return corsMiddleware(handler)
}

// replayHeader adds X-Leather-Replay: true to every response.
func replayHeader(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Leather-Replay", "true")
		h.ServeHTTP(w, r)
	})
}

// printProgress writes a single tool-call activity line to w in pretty mode.
// ev.Kind is "system", "skill_start", "user", "call" (tool invoked), or "result" (tool returned).
func printProgress(w io.Writer, ev runner.ProgressEvent) {
	printProgressAt(w, time.Now(), ev)
}

func printProgressAt(w io.Writer, at time.Time, ev runner.ProgressEvent) {
	ts := at.Format("15:04:05")
	switch ev.Kind {
	case "system":
		lines := prettyTextLines(ev.Prompt)
		const maxSystemLines = 1
		shown := lines
		var extra []string
		if len(lines) > maxSystemLines {
			shown = lines[:maxSystemLines]
			extra = []string{dim(fmt.Sprintf("(+%d lines)", len(lines)-maxSystemLines))}
		}
		dimmed := make([]string, len(shown))
		for i, line := range shown {
			dimmed[i] = dim(line)
		}
		prettyWriteEntry(w, ts, dim(prettyPadLabel("system:")), append(dimmed, extra...))
	case "skill_start":
		prettyWriteEntry(w, ts, boldCyan(prettyPadLabel("◆ skill")), []string{ev.Skill})
	case "user":
		lines := prettyTextLines(ev.Prompt)
		const maxUserLines = 5
		if len(lines) > maxUserLines {
			prettyWriteEntry(w, ts, boldCyan(prettyPadLabel("user")), append(lines[:maxUserLines], dim(fmt.Sprintf("(+%d lines)", len(lines)-maxUserLines))))
		} else {
			prettyWriteEntry(w, ts, boldCyan(prettyPadLabel("user")), lines)
		}
	case "call":
		lines := []string{boldCyan(ev.Tool)}
		lines = append(lines, prettyJSONFieldLines(ev.Args)...)
		lines = append(lines, dim(fmt.Sprintf("round: %d", ev.Round+1)))
		prettyWriteEntry(w, ts, yellow(prettyPadLabel("⚙ "+ev.ToolType)), lines)
	case "context":
		if ev.Context != nil {
			prettyWriteEntry(w, ts, dim(prettyPadLabel("context")), formatContextSnapshot(ev.Context))
		}
	case "result":
		if ev.Error != "" {
			lines := []string{red("✗ fail") + " " + boldCyan(ev.Tool)}
			lines = append(lines, prettyFieldLines("error", ev.Error)...)
			prettyWriteEntry(w, ts, dim(prettyPadLabel("result")), lines)
		} else {
			lines := []string{boldGreen("✓ success") + " " + boldCyan(ev.Tool), dim(fmt.Sprintf("bytes: %d", ev.ResultBytes))}
			if ev.ResultPreview != "" {
				for _, ln := range prettyTextLines(ev.ResultPreview) {
					lines = append(lines, dim(ln))
				}
			}
			prettyWriteEntry(w, ts, dim(prettyPadLabel("result")), lines)
		}
	case "extract":
		lines := []string{boldCyan(ev.VarKey) + " = " + ev.VarVal, dim("from " + ev.Tool)}
		prettyWriteEntry(w, ts, dim(prettyPadLabel("vars")), lines)
	}
}

func printContextSnapshot(w io.Writer, snap runner.ContextSnapshot) {
	ts := time.Now().Format("15:04:05")
	prettyWriteEntry(w, ts, dim(prettyPadLabel("context")), formatContextSnapshot(&snap))
}

func formatContextSnapshot(snap *runner.ContextSnapshot) []string {
	if snap == nil {
		return nil
	}
	lines := []string{dim(fmt.Sprintf(
		"agent=%s turn=%d round=%d max_tokens=%d temperature=%.2f",
		snap.AgentName,
		snap.Turn+1,
		snap.Round+1,
		snap.MaxTokens,
		snap.Temperature,
	))}
	if len(snap.ToolNames) == 0 {
		lines = append(lines, dim("tools: none"))
	} else {
		lines = append(lines, dim("tools: "+strings.Join(snap.ToolNames, ", ")))
	}
	if len(snap.ExtraBody) > 0 {
		if raw, err := json.Marshal(snap.ExtraBody); err == nil {
			lines = append(lines, dim("extra_body: "+string(raw)))
		}
	}
	for i, msg := range snap.Messages {
		header := fmt.Sprintf("[%d] %s", i, msg.Role)
		if msg.ToolName != "" {
			header += " name=" + msg.ToolName
		}
		if msg.ToolCallID != "" {
			header += " call_id=" + msg.ToolCallID
		}
		if msg.Summarized {
			header += " summarized=true"
		}
		lines = append(lines, header)
		for _, line := range prettyTextLines(msg.Content) {
			lines = append(lines, "  "+line)
		}
		for _, tc := range msg.ToolCalls {
			callLine := "  tool_call " + tc.Name + " id=" + tc.ID
			if len(tc.Arguments) > 0 {
				if raw, err := json.Marshal(tc.Arguments); err == nil {
					callLine += " args=" + string(raw)
				}
			}
			lines = append(lines, dim(callLine))
		}
	}
	return lines
}

// clickableHost rewrites a bind address into a host:port a browser can open.
// Wildcards (0.0.0.0, ::, empty host) are rewritten to localhost so the URL
// printed at startup is clickable. Concrete addresses pass through unchanged.
func clickableHost(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}
	return net.JoinHostPort(host, port)
}

// printTanneryEvent renders a TanneryEvent in the same visual language as
// printProgressAt. The label encodes the event kind + direction icon; the
// curing name appears as the first token in the content line.
// Icons: → webhook, ↑ enqueue, ↓ dequeue, ↺ retry, ⊗ dead-letter.
func printTanneryEvent(w io.Writer, ev curing.TanneryEvent) {
	ts := time.Now().Format("15:04:05")
	switch ev.Kind {
	case "webhook":
		src := ev.Source
		if src == "" {
			src = "/webhooks/" + ev.WebhookName
		}
		lines := []string{ev.Curing + "  " + dim(src+" ← "+ev.WebhookName)}
		if ev.HideID != "" {
			lines = append(lines, dim(ev.HideID))
		}
		prettyWriteEntry(w, ts, green(prettyPadLabel("→ webhook")), lines)
	case "enqueue":
		lines := []string{ev.Curing + "  " + dim("queue="+ev.DestQueue+" ← "+ev.HideKind)}
		if ev.HideID != "" {
			lines = append(lines, dim(ev.HideID))
		}
		prettyWriteEntry(w, ts, green(prettyPadLabel("↑ enqueue")), lines)
	case "dequeue":
		lines := []string{ev.Curing + "  " + dim("trace="+ev.Queue+" ← "+ev.HideKind)}
		if ev.HideID != "" {
			lines = append(lines, dim(ev.HideID))
		}
		prettyWriteEntry(w, ts, yellow(prettyPadLabel("↓ dequeue")), lines)
	case "retry":
		lines := []string{ev.Curing + "  " + dim(fmt.Sprintf("attempt=%d", ev.Attempt))}
		if ev.Err != "" {
			lines = append(lines, dim(ev.Err))
		}
		prettyWriteEntry(w, ts, yellow(prettyPadLabel("↺ retry")), lines)
	case "dlq":
		lines := []string{ev.Curing + "  " + dim(fmt.Sprintf("attempt=%d", ev.Attempt))}
		if ev.Err != "" {
			lines = append(lines, dim(ev.Err))
		}
		prettyWriteEntry(w, ts, boldRed(prettyPadLabel("⊗ dlq")), lines)
	}
}

func prettyPadLabel(label string) string {
	return prettyPadLabelWidth(label, prettyLabelWidth)
}

func prettyPadLabelWidth(label string, width int) string {
	padding := prettyLabelWidth - utf8.RuneCountInString(label)
	if width > prettyLabelWidth {
		padding = width - utf8.RuneCountInString(label)
	}
	if padding < 0 {
		padding = 0
	}
	return strings.Repeat(" ", padding) + label
}

func prettyWriteBlock(w io.Writer, ts string, styledLabel string, lines []string) {
	if len(lines) == 0 {
		lines = []string{""}
	}
	blankTime := strings.Repeat(" ", len("["+ts+"]"))
	labelWidth := prettyVisibleWidth(styledLabel)
	if labelWidth < prettyLabelWidth {
		labelWidth = prettyLabelWidth
	}
	railLabel := dim(prettyPadLabelWidth("┆", labelWidth))
	bodyWidth := prettyBodyWidth(labelWidth)
	first := true
	for _, line := range lines {
		for _, wrapped := range prettyWrapANSI(line, bodyWidth) {
			if first {
				fmt.Fprintf(w, "  %s %s  %s\n", dim("["+ts+"]"), styledLabel, wrapped)
				first = false
				continue
			}
			fmt.Fprintf(w, "  %s %s  %s\n", blankTime, railLabel, wrapped)
		}
	}
}

func prettyBodyWidth(labelWidth int) int {
	prefixWidth := 2 + len("[15:04:05]") + 1 + labelWidth + 2
	width := prettyStatusWidth() - prefixWidth
	if width < 20 {
		return 20
	}
	return width
}

func prettyWrapANSI(line string, width int) []string {
	if width <= 0 || prettyVisibleWidth(line) <= width {
		return []string{line}
	}
	var out []string
	var b strings.Builder
	visible := 0
	active := ""
	for i := 0; i < len(line); {
		if seq, next, ok := prettyANSISeq(line, i); ok {
			b.WriteString(seq)
			if seq == ansiReset {
				active = ""
			} else if strings.HasSuffix(seq, "m") {
				active = seq
			}
			i = next
			continue
		}
		if visible >= width {
			if active != "" {
				b.WriteString(ansiReset)
			}
			out = append(out, b.String())
			b.Reset()
			if active != "" {
				b.WriteString(active)
			}
			visible = 0
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		b.WriteRune(r)
		visible++
		i += size
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

func prettyVisibleWidth(s string) int {
	width := 0
	for i := 0; i < len(s); {
		if _, next, ok := prettyANSISeq(s, i); ok {
			i = next
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		width++
		i += size
	}
	return width
}

func prettyANSISeq(s string, i int) (string, int, bool) {
	if i+2 >= len(s) || s[i] != '\x1b' || s[i+1] != '[' {
		return "", i, false
	}
	for j := i + 2; j < len(s); j++ {
		if s[j] >= '@' && s[j] <= '~' {
			return s[i : j+1], j + 1, true
		}
	}
	return "", i, false
}

func prettyWriteEntry(w io.Writer, ts string, styledLabel string, lines []string) {
	prettyWriteBlock(w, ts, styledLabel, lines)
	fmt.Fprintln(w)
}

// prettyWriteEntryNoWrap is like prettyWriteEntry but emits each line as-is
// without width-based wrapping. Use for content where mid-line breaks would
// corrupt the value (URLs with tokens, file paths, etc.) so terminal copy
// produces a usable string.
func prettyWriteEntryNoWrap(w io.Writer, ts string, styledLabel string, lines []string) {
	if len(lines) == 0 {
		lines = []string{""}
	}
	blankTime := strings.Repeat(" ", len("["+ts+"]"))
	labelWidth := prettyVisibleWidth(styledLabel)
	if labelWidth < prettyLabelWidth {
		labelWidth = prettyLabelWidth
	}
	railLabel := dim(prettyPadLabelWidth("┆", labelWidth))
	for i, line := range lines {
		if i == 0 {
			fmt.Fprintf(w, "  %s %s  %s\n", dim("["+ts+"]"), styledLabel, line)
			continue
		}
		fmt.Fprintf(w, "  %s %s  %s\n", blankTime, railLabel, line)
	}
	fmt.Fprintln(w)
}

func prettyTextLines(text string) []string {
	return strings.Split(text, "\n")
}

func prettyFieldLines(key, value string) []string {
	parts := strings.Split(value, "\n")
	if len(parts) == 1 {
		return []string{fmt.Sprintf("%s: %s", key, value)}
	}
	lines := []string{key + ":"}
	for _, part := range parts {
		lines = append(lines, "  "+part)
	}
	return lines
}

func prettyJSONFieldLines(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return nil
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return []string{dim("with " + raw)}
	}
	obj, ok := value.(map[string]any)
	if !ok {
		var indented bytes.Buffer
		if err := json.Indent(&indented, []byte(raw), "", "  "); err == nil {
			return prettyTextLines(indented.String())
		}
		return []string{raw}
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(obj))
	for _, key := range keys {
		lines = append(lines, prettyJSONValueLines(key, obj[key])...)
	}
	return lines
}

func prettyJSONValueLines(key string, value any) []string {
	switch typed := value.(type) {
	case string:
		parts := strings.Split(typed, "\n")
		if len(parts) == 1 {
			return []string{fmt.Sprintf("%s: %s", key, parts[0])}
		}
		lines := []string{key + ":"}
		for _, part := range parts {
			lines = append(lines, "  "+part)
		}
		return lines
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return []string{fmt.Sprintf("%s: %v", key, typed)}
		}
		return []string{fmt.Sprintf("%s: %s", key, string(encoded))}
	}
}

func normalizedPrettyMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "all":
		return "all"
	default:
		return "messages"
	}
}

func shouldPrintPrettyProgress(cfg model.Config) bool {
	return cfg.Pretty && normalizedPrettyMode(cfg.PrettyMode) == "all"
}

// printTurn writes a human-readable agent turn to w.
// Output style matches the pretty activity rail: fixed labels and a continuation rail
// so multi-line prompts and responses stay within one left-aligned column.
func printTurn(w io.Writer, a model.Agent, resp model.LLMResponse, mode string, showStats bool, tokensPerTurn bool) {
	printTurnAt(w, time.Now(), a, resp, mode, showStats, tokensPerTurn)
}

func printTurnAt(w io.Writer, at time.Time, a model.Agent, resp model.LLMResponse, mode string, showStats bool, tokensPerTurn bool) {
	ts := at.Format("15:04:05")
	if normalizedPrettyMode(mode) == "messages" && a.UserPrompt != "" {
		prettyWriteEntry(w, ts, boldCyan(prettyPadLabel("user")), prettyTextLines(a.UserPrompt))
	}
	respLines := []string{boldGreen(a.Name)}
	if resp.Content != "" {
		if !strings.Contains(resp.Content, "\n") {
			respLines[0] = boldGreen(a.Name) + "  " + resp.Content
		} else {
			respLines = append(respLines, prettyTextLines(resp.Content)...)
		}
	}
	if tokensPerTurn || showStats {
		respLines = append(respLines,
			dim(fmt.Sprintf("tokens: prompt=%d  completion=%d  total=%d",
				resp.PromptTokens, resp.CompletionTokens, resp.TotalTokens)))
	}
	prettyWriteEntry(w, ts, boldGreen(prettyPadLabel("agent")), respLines)
}

// buildHTTPClient returns an HTTPClient from cfg.
func buildHTTPClient(cfg model.Config) *session.HTTPClient {
	return session.NewHTTPClient(cfg.LLMEndpoint, cfg.LLMAPIKey, cfg.LLMTimeout)
}

// lifecycleBaseName returns the base filename (no directory) of a lifecycle file path.
// Returns "" if path is empty. Used to group agents by lifecycle in the UI.
func lifecycleBaseName(path string) string {
	if path == "" {
		return ""
	}
	i := strings.LastIndex(path, "/")
	if i >= 0 {
		return path[i+1:]
	}
	return path
}
