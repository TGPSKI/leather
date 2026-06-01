package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/devtools/bus"
	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/scheduler"
)

// testLogger returns a discard logger for use in API tests.
func testLogger(t *testing.T) *logging.Logger {
	t.Helper()
	return logging.NewWithWriter("test", model.LogLevelError, io.Discard, false)
}

// newTestServer builds an apiMux with sched and returns a test HTTP server.
func newTestServer(t *testing.T, sched *scheduler.Scheduler) *httptest.Server {
	t.Helper()
	deps := apiDeps{
		sched:       sched,
		metrics:     newRunMetrics(),
		startedAt:   time.Now(),
		log:         testLogger(t),
		devtoolsBus: bus.New(128),
	}
	srv := httptest.NewServer(apiMux(deps))
	t.Cleanup(srv.Close)
	return srv
}

func TestAPI_DevtoolsSnapshot(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	b := bus.New(8)
	b.Publish(bus.Event{Kind: "test.event"})
	deps := apiDeps{
		sched:       sched,
		metrics:     newRunMetrics(),
		startedAt:   time.Now(),
		log:         testLogger(t),
		devtoolsBus: b,
		version:     "vtest",
		commit:      "abc",
	}
	srv := newTestServerWithDeps(t, deps)

	resp, err := http.Get(srv.URL + "/api/devtools/snapshot")
	if err != nil {
		t.Fatalf("GET /api/devtools/snapshot: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Leather-Devtools-Version"); got != "1" {
		t.Fatalf("X-Leather-Devtools-Version = %q, want 1", got)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if body["version"] != "vtest" {
		t.Fatalf("version = %v, want vtest", body["version"])
	}
}

// newTestServerWithDeps builds an apiMux with fully-specified deps.
func newTestServerWithDeps(t *testing.T, deps apiDeps) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(apiMux(deps))
	t.Cleanup(srv.Close)
	return srv
}

func TestAPI_Healthz(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	srv := newTestServer(t, sched)

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	// /healthz now reports 503 when required deps (state_dir, llm_endpoint)
	// are not configured. The test fixture leaves cfg empty so we expect 503.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (empty test deps)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "degraded" {
		t.Errorf("body.status = %v, want degraded", body["status"])
	}
	if _, ok := body["checks"].(map[string]any); !ok {
		t.Errorf("body.checks missing or wrong type: %#v", body["checks"])
	}
}

func TestAPI_Jobs_Empty(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	srv := newTestServer(t, sched)

	resp, err := http.Get(srv.URL + "/jobs")
	if err != nil {
		t.Fatalf("GET /jobs: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var jobs []model.Job
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	if jobs == nil {
		t.Error("expected non-null JSON array, got null")
	}
	if len(jobs) != 0 {
		t.Errorf("len(jobs) = %d, want 0", len(jobs))
	}
}

func TestAPI_CORS_Headers(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	srv := newTestServer(t, sched)

	for _, path := range []string{"/healthz", "/jobs", "/status", "/config", "/metrics", "/history"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		resp.Body.Close() //nolint:errcheck
		got := resp.Header.Get("Access-Control-Allow-Origin")
		if got != "*" {
			t.Errorf("GET %s: Access-Control-Allow-Origin = %q, want *", path, got)
		}
	}
}

func TestAPI_CORS_Options(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	srv := newTestServer(t, sched)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodOptions, srv.URL+"/status", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS /status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS status = %d, want 204", resp.StatusCode)
	}
}

func TestAPI_Status(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	start := time.Now().Add(-5 * time.Second)
	deps := apiDeps{
		sched:     sched,
		metrics:   newRunMetrics(),
		startedAt: start,
		version:   "v0.1.0",
		commit:    "abc1234",
		cfg: model.Config{
			LLMEndpoint:       "http://localhost:8000",
			SchedulerTick:     30 * time.Second,
			MaxConcurrentJobs: 4,
		},
		log: testLogger(t),
	}
	srv := newTestServerWithDeps(t, deps)

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /status: %v", err)
	}
	for _, key := range []string{"started_at", "uptime_seconds", "version", "commit", "llm_endpoint", "agent_count", "scheduler_tick", "max_concurrent_jobs"} {
		if _, ok := body[key]; !ok {
			t.Errorf("/status missing key %q", key)
		}
	}
	if body["version"] != "v0.1.0" {
		t.Errorf("version = %q, want v0.1.0", body["version"])
	}
	if body["commit"] != "abc1234" {
		t.Errorf("commit = %q, want abc1234", body["commit"])
	}
	if uptime, ok := body["uptime_seconds"].(float64); !ok || uptime < 0 {
		t.Errorf("uptime_seconds = %v, want >= 0", body["uptime_seconds"])
	}
}

func TestAPI_Config(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	deps := apiDeps{
		sched:   sched,
		metrics: newRunMetrics(),
		cfg: model.Config{
			AgentDir:           "~/.leather/agents",
			Model:              "llama3",
			MaxTokens:          8192,
			CompletionReserve:  1024,
			SummarizeThreshold: 0.85,
			LLMEndpoint:        "http://localhost:11434",
			LLMTimeout:         2 * time.Minute,
			SchedulerTick:      30 * time.Second,
			MaxConcurrentJobs:  4,
			APIAddr:            "127.0.0.1:8080",
		},
		startedAt: time.Now(),
		log:       testLogger(t),
	}
	srv := newTestServerWithDeps(t, deps)

	resp, err := http.Get(srv.URL + "/config")
	if err != nil {
		t.Fatalf("GET /config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /config: %v", err)
	}
	for _, key := range []string{"agent_dir", "model", "max_tokens", "llm_endpoint", "llm_timeout", "scheduler_tick", "api_addr"} {
		if _, ok := body[key]; !ok {
			t.Errorf("/config missing key %q", key)
		}
	}
	// Ensure sensitive-only fields (log_file, state_dir) are not exposed.
	for _, key := range []string{"log_file", "state_dir", "config_file"} {
		if _, ok := body[key]; ok {
			t.Errorf("/config unexpectedly exposes field %q", key)
		}
	}
}

func TestAPI_Metrics_Empty(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	srv := newTestServer(t, sched)

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /metrics: %v", err)
	}
	agents, ok := body["agents"]
	if !ok {
		t.Fatal("/metrics missing \"agents\" key")
	}
	agentsMap, ok := agents.(map[string]interface{})
	if !ok {
		t.Fatalf("/metrics agents is not an object: %T", agents)
	}
	if len(agentsMap) != 0 {
		t.Errorf("agents map has %d entries, want 0", len(agentsMap))
	}
}

func TestAPI_History_Empty(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	srv := newTestServer(t, sched)

	resp, err := http.Get(srv.URL + "/history")
	if err != nil {
		t.Fatalf("GET /history: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var recs []model.RunRecord
	if err := json.NewDecoder(resp.Body).Decode(&recs); err != nil {
		t.Fatalf("decode /history: %v", err)
	}
	if recs == nil {
		t.Error("expected non-null JSON array, got null")
	}
	if len(recs) != 0 {
		t.Errorf("len(history) = %d, want 0", len(recs))
	}
}

func TestAPI_Metrics_WithRuns(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	m := newRunMetrics()
	m.record(model.RunRecord{
		AgentName: "test-agent",
		Time:      model.RunTime{StartTs: 1000, DurationMs: 850},
		Status:    model.JobStatusSuccess,
		Tokens:    model.RunTokens{Prompt: 67, Response: 29, Total: 96},
	})
	m.record(model.RunRecord{
		AgentName: "test-agent",
		Time:      model.RunTime{StartTs: 2000, DurationMs: 100},
		Status:    model.JobStatusError,
		Error:     "timeout",
	})
	deps := apiDeps{
		sched:     sched,
		metrics:   m,
		startedAt: time.Now(),
		log:       testLogger(t),
	}
	srv := newTestServerWithDeps(t, deps)

	// Check /metrics.
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()

	var mr metricsResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		t.Fatalf("decode /metrics: %v", err)
	}
	ag, ok := mr.Agents["test-agent"]
	if !ok {
		t.Fatal("/metrics missing test-agent")
	}
	if ag.RunCount != 2 {
		t.Errorf("run_count = %d, want 2", ag.RunCount)
	}
	if ag.ErrorCount != 1 {
		t.Errorf("error_count = %d, want 1", ag.ErrorCount)
	}
	if ag.TotalPromptTokens != 67 {
		t.Errorf("total_prompt_tokens = %d, want 67", ag.TotalPromptTokens)
	}
	if len(ag.RecentRuns) != 2 {
		t.Errorf("recent_runs len = %d, want 2", len(ag.RecentRuns))
	}

	// Check /history.
	resp2, err := http.Get(srv.URL + "/history")
	if err != nil {
		t.Fatalf("GET /history: %v", err)
	}
	defer resp2.Body.Close()

	var recs []model.RunRecord
	if err := json.NewDecoder(resp2.Body).Decode(&recs); err != nil {
		t.Fatalf("decode /history: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("history len = %d, want 2", len(recs))
	}
	// Most recent first (Time.StartTs desc).
	if len(recs) >= 2 && recs[0].Time.StartTs < recs[1].Time.StartTs {
		t.Errorf("history not sorted desc: recs[0].Time.StartTs=%d recs[1].Time.StartTs=%d", recs[0].Time.StartTs, recs[1].Time.StartTs)
	}
}

func TestAPI_Jobs_WithRegistered(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	noop := func(_ context.Context, _ model.Job) error { return nil }
	if err := sched.Register("agent-a", "0 * * * *", noop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv := newTestServer(t, sched)

	resp, err := http.Get(srv.URL + "/jobs")
	if err != nil {
		t.Fatalf("GET /jobs: %v", err)
	}
	defer resp.Body.Close()

	var jobs []model.Job
	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if jobs[0].AgentName != "agent-a" {
		t.Errorf("AgentName = %q, want agent-a", jobs[0].AgentName)
	}
}

func TestAPI_JobsName_Found(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	noop := func(_ context.Context, _ model.Job) error { return nil }
	if err := sched.Register("my-agent", "0 * * * *", noop); err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv := newTestServer(t, sched)

	resp, err := http.Get(srv.URL + "/jobs/my-agent")
	if err != nil {
		t.Fatalf("GET /jobs/my-agent: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	var job model.Job
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.AgentName != "my-agent" {
		t.Errorf("AgentName = %q, want my-agent", job.AgentName)
	}
}

func TestAPI_JobsName_NotFound(t *testing.T) {
	sched := scheduler.New(scheduler.Options{})
	srv := newTestServer(t, sched)

	resp, err := http.Get(srv.URL + "/jobs/does-not-exist")
	if err != nil {
		t.Fatalf("GET /jobs/does-not-exist: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "not found" {
		t.Errorf("body.error = %q, want 'not found'", body["error"])
	}
}
