package curing

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/artifact"
	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/notify"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/runner"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
)

// --- test helpers ---

func testLog(t *testing.T) *logging.Logger {
	t.Helper()
	return logging.New("test", model.LogLevelError)
}

func testAgent(name string) model.Agent {
	return model.Agent{
		Name:        name,
		Model:       "test-model",
		Temperature: 0.7,
		Timeout:     10 * time.Second,
		Enabled:     true,
	}
}

func testDef(name, agentName, queueName string) model.CuringDefinition {
	return model.CuringDefinition{
		Name:           name,
		Agent:          agentName,
		Queue:          queueName,
		PageSizeBytes:  3800,
		MaxAttempts:    3,
		TimeoutSeconds: 30,
	}
}

// testWorker builds a Worker wired to real hide, artifact, and queue stores under dir.
func testWorker(
	t *testing.T,
	def model.CuringDefinition,
	agents map[string]model.Agent,
	client session.LLMClient,
	notifiers map[string]notify.Notifier,
	dir string,
) (*Worker, *hide.Store, *artifact.Store, *queue.Manager) {
	t.Helper()
	hs := hide.NewStore(dir + "/hides")
	as := artifact.NewStore(dir + "/artifacts")
	qmgr := queue.NewManager(dir + "/queues")
	log := testLog(t)

	if notifiers == nil {
		notifiers = map[string]notify.Notifier{}
	}
	deps := &RunnerDeps{
		Client:        client,
		ToolReg:       tool.NewRegistry(),
		Log:           log,
		MaxToolRounds: 1,
		Notifiers:     notifiers,
	}
	deps.QueueMgr = qmgr

	w, err := NewWorker(def, agents, 1, hs, as, deps, qmgr, nil, log)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return w, hs, as, qmgr
}

// putHide writes a hide into the store and returns the entry.
func putHide(t *testing.T, hs *hide.Store, kind, content string) hide.StoreEntry {
	t.Helper()
	entry, err := hs.Put(kind, "test", []byte(content), nil)
	if err != nil {
		t.Fatalf("put hide: %v", err)
	}
	return entry
}

// makeItem builds a QueueItem referencing a hide.
func makeItem(hideID, hideKind, curingName string) model.QueueItem {
	return model.QueueItem{
		ID:           "item_test_001",
		CuringName:   curingName,
		HideID:       hideID,
		HideKind:     hideKind,
		EnqueuedAt:   time.Now().Unix(),
		AttemptCount: 0,
		Payload:      map[string]any{},
	}
}

// --- mock notifier ---

type mockNotifier struct {
	name string
	err  error
	mu   sync.Mutex
	sent int
}

func (m *mockNotifier) Send(_ context.Context, _ notify.Message) error {
	m.mu.Lock()
	m.sent++
	m.mu.Unlock()
	return m.err
}
func (m *mockNotifier) Name() string { return m.name }

// --- blocking LLM client (blocks until ctx is cancelled) ---

type blockingClient struct{}

func (b *blockingClient) Complete(ctx context.Context, _ string, _ []model.Message, _ session.CompletionOptions) (model.LLMResponse, error) {
	<-ctx.Done()
	return model.LLMResponse{}, ctx.Err()
}

func (b *blockingClient) CountTokens(messages []model.Message) (int, error) {
	return len(messages) * 10, nil
}

// --- tests ---

func TestWorker_ProcessSuccess(t *testing.T) {
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "artifact content"})
	def := testDef("pr-review", "pr-agent", "default")
	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}

	w, hs, as, _ := testWorker(t, def, agents, mock, nil, dir)

	entry := putHide(t, hs, "github.pr", "large thread content")
	item := makeItem(entry.ID, "github.pr", "pr-review")

	if err := w.process(context.Background(), item); err != nil {
		t.Fatalf("process: %v", err)
	}

	// Artifact should be written.
	arts, err := as.ListByCuring("pr-review")
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].Content != "artifact content" {
		t.Errorf("content: got %q", arts[0].Content)
	}
	if arts[0].HideID != entry.ID {
		t.Errorf("HideID: got %q", arts[0].HideID)
	}

	// Hide should be deleted after successful artifact write.
	_, _, err = hs.Get(entry.ID)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected hide deleted; get returned %v", err)
	}
}

func TestWorker_ProcessAgentNotFound(t *testing.T) {
	// NewWorker fails fast when the configured agent name is not in the agents
	// map. This catches operator typos at startup rather than wasting the first
	// dequeued item as a sentinel.
	dir := t.TempDir()
	hs := hide.NewStore(dir + "/hides")
	as := artifact.NewStore(dir + "/artifacts")
	qmgr := queue.NewManager(dir + "/queues")
	log := testLog(t)
	def := testDef("pr-review", "nonexistent-agent", "default")
	deps := &RunnerDeps{ToolReg: tool.NewRegistry(), Log: log}

	_, err := NewWorker(def, map[string]model.Agent{}, 1, hs, as, deps, qmgr, nil, log)
	if err == nil {
		t.Fatal("expected NewWorker error for missing agent, got nil")
	}
}

func TestWorker_ProcessHideNotFound(t *testing.T) {
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "response"})
	def := testDef("pr-review", "pr-agent", "default")
	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}

	w, _, _, _ := testWorker(t, def, agents, mock, nil, dir)
	item := makeItem("hide_missing_00000000_0000_0000", "kind", "pr-review")

	err := w.process(context.Background(), item)
	if err == nil {
		t.Fatal("expected error for missing hide")
	}
	if errors.Is(err, errAgentNotFound) {
		t.Error("should not be errAgentNotFound for missing hide")
	}
}

func TestBuildReflectionTurns_AlternatesSummaryThenNextPage(t *testing.T) {
	firstCut := hide.Cut{
		HideID:     "hide_test",
		Source:     "test",
		PageNumber: 1,
		TotalPages: 3,
		SizeBytes:  42,
		Content:    "page 1 body",
		IsFinal:    false,
	}

	turns := buildReflectionTurns(firstCut)
	if len(turns) != 4 {
		t.Fatalf("turn count = %d, want 4", len(turns))
	}
	if !strings.Contains(turns[0], "List 3-5 key facts verbatim from this page") {
		t.Fatalf("turn 0 missing summary instruction: %q", turns[0])
	}
	if !strings.Contains(turns[0], "Do not call hide_next yet") {
		t.Fatalf("turn 0 missing no-navigation instruction: %q", turns[0])
	}
	if !strings.Contains(turns[1], "Now call hide_next to retrieve page 2") {
		t.Fatalf("turn 1 = %q, want page-2 call-next prompt", turns[1])
	}
	if !strings.Contains(turns[1], "follow that page's instruction") {
		t.Fatalf("turn 1 missing page-follow instruction: %q", turns[1])
	}
	if !strings.Contains(turns[2], "Now call hide_next to retrieve page 3") {
		t.Fatalf("turn 2 = %q, want page-3 call-next prompt", turns[2])
	}
	if !strings.Contains(turns[3], "You have now read all 3 pages") {
		t.Fatalf("turn 3 = %q, want final-output prompt", turns[3])
	}
}

func TestWorker_ProcessPrependsStrictPagingPreamble(t *testing.T) {
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "artifact content"})
	def := testDef("pr-review", "pr-agent", "default")
	agent := testAgent("pr-agent")
	agent.SystemPrompt = "Produce the final note immediately."
	agents := map[string]model.Agent{"pr-agent": agent}

	w, hs, _, _ := testWorker(t, def, agents, mock, nil, dir)
	var events []runner.ProgressEvent
	w.deps.ProgressFn = func(ev runner.ProgressEvent) {
		events = append(events, ev)
	}
	entry := putHide(t, hs, "github.pr", strings.Repeat("x", def.PageSizeBytes+200))
	item := makeItem(entry.ID, "github.pr", "pr-review")

	if err := w.process(context.Background(), item); err != nil {
		t.Fatalf("process: %v", err)
	}

	var systemPrompt string
	for _, ev := range events {
		if ev.Kind == "system" {
			systemPrompt = ev.Prompt
			break
		}
	}
	if systemPrompt == "" {
		t.Fatalf("expected system progress event, got %+v", events)
	}
	if !strings.Contains(systemPrompt, "Follow a strict alternating protocol") {
		t.Fatalf("system prompt missing strict paging preamble: %q", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "do not call hide_next, hide_jump, or produce final output") {
		t.Fatalf("system prompt missing no-navigation rule: %q", systemPrompt)
	}
	var userPrompts []string
	for _, ev := range events {
		if ev.Kind == "user" {
			userPrompts = append(userPrompts, ev.Prompt)
		}
	}
	if len(userPrompts) < 2 {
		t.Fatalf("user prompt count = %d, want at least 2; events=%+v", len(userPrompts), events)
	}
	if !strings.Contains(userPrompts[1], "Now call hide_next to retrieve page 2") {
		t.Fatalf("second user prompt = %q, want next-page instruction", userPrompts[1])
	}
}

func TestWorker_ProcessDoesNotCaptureContextWhenDebugDisabled(t *testing.T) {
	dir := t.TempDir()
	def := testDef("pr-review", "pr-agent", "default")
	mock := session.NewMockLLM(session.MockConfig{Response: "artifact content"})
	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}
	w, hs, _, _ := testWorker(t, def, agents, mock, nil, dir)

	var completeEvents []runner.ProgressEvent
	w.deps.OnComplete = func(_ model.Agent, _ model.RunRecord, _ model.Artifact, events []runner.ProgressEvent) {
		completeEvents = append(completeEvents, events...)
	}
	entry := putHide(t, hs, "github.pr", "short hide content")
	item := makeItem(entry.ID, "github.pr", "pr-review")

	if err := w.process(context.Background(), item); err != nil {
		t.Fatalf("process: %v", err)
	}
	for _, ev := range completeEvents {
		if ev.Kind == "context" {
			t.Fatalf("captured context event with DebugContextFn disabled: %+v", ev)
		}
	}
}

func TestWorker_ProcessRetryExhausted(t *testing.T) {
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "response"})
	def := testDef("pr-review", "pr-agent", "default")
	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}

	w, _, _, qmgr := testWorker(t, def, agents, mock, nil, dir)
	// Item with AttemptCount already at MaxAttempts-1; next failure pushes to DLQ.
	item := makeItem("hide_missing_00000000_0000_0000", "kind", "pr-review")
	item.AttemptCount = def.MaxAttempts - 1

	w.handleItem(context.Background(), item)

	dlqName := def.Queue + "-dlq"
	dlq, err := qmgr.Get(dlqName)
	if err != nil {
		t.Fatalf("get DLQ: %v", err)
	}
	if dlq.Len() != 1 {
		t.Errorf("expected 1 item in DLQ, got %d", dlq.Len())
	}
}

func TestWorker_AgentNotFound_GoesToDLQImmediately(t *testing.T) {
	// Renamed/repurposed: now verifies that a missing hide (errHideMissing
	// sentinel) routes to the DLQ on the first attempt, bypassing the retry
	// counter — analogous to the old errAgentNotFound behaviour for hides that
	// disappear between enqueue and dequeue.
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "response"})
	def := testDef("pr-review", "pr-agent", "default")
	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}

	w, _, _, qmgr := testWorker(t, def, agents, mock, nil, dir)
	item := makeItem("hide_missing_00000000_0000_0000", "kind", "pr-review")
	item.AttemptCount = 0 // first attempt

	w.handleItem(context.Background(), item)

	// Despite AttemptCount=0 (< MaxAttempts=3), should go straight to DLQ.
	dlqName := def.Queue + "-dlq"
	dlq, err := qmgr.Get(dlqName)
	if err != nil {
		t.Fatalf("get DLQ: %v", err)
	}
	if dlq.Len() != 1 {
		t.Errorf("expected 1 item in DLQ immediately, got %d", dlq.Len())
	}
}

func TestWorker_TimeoutEnforced(t *testing.T) {
	dir := t.TempDir()
	def := testDef("pr-review", "pr-agent", "default")
	// TimeoutSeconds=1 so process gets a 1-second deadline.
	def.TimeoutSeconds = 1

	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}
	w, hs, _, _ := testWorker(t, def, agents, &blockingClient{}, nil, dir)

	entry := putHide(t, hs, "kind", "content")
	item := makeItem(entry.ID, "kind", "pr-review")

	start := time.Now()
	err := w.process(context.Background(), item)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from timeout")
	}
	// Should have returned within ~3 seconds (1s timeout + overhead).
	if elapsed > 5*time.Second {
		t.Errorf("process took too long (%v), timeout not enforced", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestWorker_RunnerNotShared(t *testing.T) {
	// Two concurrent process calls must not race. The race detector verifies isolation.
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "response"})
	def := testDef("pr-review", "pr-agent", "default")
	// No timeout so both can run to completion.
	def.TimeoutSeconds = 0
	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}

	w, hs, _, _ := testWorker(t, def, agents, mock, nil, dir)

	e1 := putHide(t, hs, "kind", "content-1")
	e2 := putHide(t, hs, "kind", "content-2")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, entry := range []hide.StoreEntry{e1, e2} {
		wg.Add(1)
		go func(idx int, e hide.StoreEntry) {
			defer wg.Done()
			item := makeItem(e.ID, "kind", "pr-review")
			item.ID = item.ID + "_" + e.ID
			errs[idx] = w.process(context.Background(), item)
		}(i, entry)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

func TestWorker_NotifyFailureDoesNotRetry(t *testing.T) {
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "response"})
	def := testDef("pr-review", "pr-agent", "default")
	def.Output.Notify = "slack"

	failNotifier := &mockNotifier{name: "slack", err: errors.New("slack down")}
	notifiers := map[string]notify.Notifier{"slack": failNotifier}

	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}
	w, hs, as, _ := testWorker(t, def, agents, mock, notifiers, dir)

	entry := putHide(t, hs, "kind", "content")
	item := makeItem(entry.ID, "kind", "pr-review")

	// process must return nil even though notify failed.
	if err := w.process(context.Background(), item); err != nil {
		t.Errorf("process should return nil on notify failure, got %v", err)
	}

	// Artifact must still be written.
	arts, _ := as.ListByCuring("pr-review")
	if len(arts) != 1 {
		t.Errorf("expected 1 artifact despite notify failure, got %d", len(arts))
	}
}

func TestWorker_HideRetainedOnDLQ(t *testing.T) {
	// Hide retention on DLQ: when an item is routed to DLQ, the hide must NOT
	// be deleted so the operator can inspect it. We exercise this via the
	// retry-exhausted path (a runner failure surfaces, item exhausts attempts).
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "", Err: errors.New("llm failed")})
	def := testDef("pr-review", "pr-agent", "default")
	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}

	w, hs, _, _ := testWorker(t, def, agents, mock, nil, dir)
	entry := putHide(t, hs, "kind", "content")
	item := makeItem(entry.ID, "kind", "pr-review")
	item.AttemptCount = def.MaxAttempts - 1 // next failure -> DLQ

	w.handleItem(context.Background(), item)

	// Hide should still exist (only deleted on success path).
	_, _, err := hs.Get(entry.ID)
	if err != nil {
		t.Errorf("hide should be retained after DLQ routing, got error: %v", err)
	}
}

func TestWorker_OutputQueue(t *testing.T) {
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "artifact output"})
	def := testDef("pr-review", "pr-agent", "default")
	def.Output.Queue = "downstream"

	agents := map[string]model.Agent{"pr-agent": testAgent("pr-agent")}
	w, hs, _, qmgr := testWorker(t, def, agents, mock, nil, dir)

	entry := putHide(t, hs, "kind", "content")
	item := makeItem(entry.ID, "kind", "pr-review")

	if err := w.process(context.Background(), item); err != nil {
		t.Fatalf("process: %v", err)
	}

	downstream, err := qmgr.Get("downstream")
	if err != nil {
		t.Fatal(err)
	}
	if downstream.Len() != 1 {
		t.Errorf("expected 1 item in downstream queue, got %d", downstream.Len())
	}
}

func TestSupervisor_StartDrain(t *testing.T) {
	dir := t.TempDir()
	mock := session.NewMockLLM(session.MockConfig{Response: "response"})
	defs := []model.CuringDefinition{
		testDef("curing-a", "agent-a", "queue-a"),
		testDef("curing-b", "agent-b", "queue-b"),
	}
	agents := map[string]model.Agent{
		"agent-a": testAgent("agent-a"),
		"agent-b": testAgent("agent-b"),
	}
	hs := hide.NewStore(dir + "/hides")
	as := artifact.NewStore(dir + "/artifacts")
	qmgr := queue.NewManager(dir + "/queues")
	log := testLog(t)

	deps := &RunnerDeps{
		Client:        mock,
		ToolReg:       tool.NewRegistry(),
		Log:           log,
		MaxToolRounds: 1,
		Notifiers:     map[string]notify.Notifier{},
		QueueMgr:      qmgr,
	}

	sup, err := NewSupervisor(defs, agents, nil, hs, as, deps, qmgr, nil, log)
	if err != nil {
		t.Fatalf("NewSupervisor: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sup.Start(ctx)

	// Workers should be running; cancel and drain should complete promptly.
	cancel()
	done := make(chan struct{})
	go func() {
		sup.Drain()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("Drain timed out; workers may be stuck")
	}
}
