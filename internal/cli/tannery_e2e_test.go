package cli

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/artifact"
	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/curing"
	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
)

// TestTanneryPipeline_E2E exercises the full end-to-end path:
//
//	POST /webhooks/{name} → hide created → queue item enqueued
//	→ CuringWorker.process → artifact written → hide deleted
//
// All I/O is local to t.TempDir(). The LLM is stubbed with MockLLM.
func TestTanneryPipeline_E2E(t *testing.T) {
	dir := t.TempDir()
	hideDir := dir + "/hides"
	artDir := dir + "/artifacts"
	queueDir := dir + "/queues"

	routes := []model.TanneryRoute{
		{
			Name:     "github-pr",
			Match:    model.RouteMatch{Source: "github"},
			HideKind: "github.pr",
			Curing:   "pr-review",
			Queue:    "default",
		},
	}
	queues := map[string]model.QueueConcurrencyConfig{
		"default": {Concurrency: 1, MaxDepth: 100},
	}

	// -- Tannery HTTP layer --
	hideStore := hide.NewStore(hideDir)
	artStore := artifact.NewStore(artDir)
	qmgr := queue.NewManager(queueDir)
	log := testLogger(t)

	td := &tanneryDeps{
		hideStore:    hideStore,
		artStore:     artStore,
		curingRouter: curing.NewRouter(routes),
		tannCfg: config.TanneryConfig{
			HideDir:     hideDir,
			ArtifactDir: artDir,
			Routes:      routes,
			Queues:      queues,
		},
	}
	deps := &apiDeps{queueMgr: qmgr, log: log}

	// Register webhook handler.
	e2eSecret := "e2e-test-secret"
	wh := model.WebhookConfig{
		Name:   "gh",
		Path:   "/webhooks/gh",
		Source: "github",
		Secret: e2eSecret,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(wh.Path, makeWebhookHandler(wh, td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 1. POST a webhook — should create hide + enqueue item.
	body := `{"action":"opened","number":42}`
	mac := hmac.New(sha256.New, []byte(e2eSecret))
	mac.Write([]byte(body))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	req, _ := http.NewRequest(http.MethodPost, srv.URL+wh.Path, strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("webhook POST: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("webhook status = %d, want 202", resp.StatusCode)
	}

	// 2. Verify a hide was created.
	entries, err := hideStore.List()
	if err != nil {
		t.Fatalf("hide list: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 hide after webhook, got %d", len(entries))
	}
	hideEntry := entries[0]

	// 3. Dequeue the item from the queue.
	q, err := qmgr.Get("default")
	if err != nil {
		t.Fatalf("queue get: %v", err)
	}
	item, ok, err := q.Dequeue()
	if err != nil || !ok {
		t.Fatalf("dequeue: ok=%v err=%v", ok, err)
	}
	if item.HideID != hideEntry.ID {
		t.Errorf("queued hide_id = %q, want %q", item.HideID, hideEntry.ID)
	}
	if item.CuringName != "pr-review" {
		t.Errorf("curing = %q, want %q", item.CuringName, "pr-review")
	}

	// 4. Run the CuringWorker.process() — exercises the curing execution layer.
	mockLLM := session.NewMockLLM(session.MockConfig{
		Response:         "LGTM — PR looks good.",
		TokensPerMessage: 10,
	})
	agentDef := model.Agent{
		Name:        "pr-agent",
		Model:       "test-model",
		Temperature: 0.7,
		Timeout:     10 * time.Second,
		Enabled:     true,
	}
	curingDef := model.CuringDefinition{
		Name:          "pr-review",
		Agent:         "pr-agent",
		Queue:         "default",
		PageSizeBytes: 3800,
		MaxAttempts:   3,
	}

	runnerDeps := &curing.RunnerDeps{
		Client:        mockLLM,
		ToolReg:       tool.NewRegistry(),
		Log:           log,
		MaxToolRounds: 1,
		QueueMgr:      qmgr,
	}
	worker, err := curing.NewWorker(
		curingDef,
		map[string]model.Agent{"pr-agent": agentDef},
		1,
		hideStore,
		artStore,
		runnerDeps,
		qmgr,
		nil,
		log,
	)
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}

	if err := worker.ProcessItem(context.Background(), item); err != nil {
		t.Fatalf("process: %v", err)
	}

	// 5. Verify artifact was written.
	arts, err := artStore.ListByCuring("pr-review")
	if err != nil {
		t.Fatalf("artifacts list: %v", err)
	}
	if len(arts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(arts))
	}
	if arts[0].Content != "LGTM — PR looks good." {
		t.Errorf("artifact content = %q", arts[0].Content)
	}
	if arts[0].HideID != hideEntry.ID {
		t.Errorf("artifact HideID = %q, want %q", arts[0].HideID, hideEntry.ID)
	}

	// 6. Verify hide was deleted after successful curing.
	remaining, _ := hideStore.List()
	if len(remaining) != 0 {
		t.Errorf("expected hide deleted after artifact write, got %d remaining", len(remaining))
	}
}
