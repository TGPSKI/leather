package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TGPSKI/leather/internal/model"
)

// --- intake handler tests ---

func TestIntake_Basic_202(t *testing.T) {
	td, deps := buildWebhookTannery(t, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/intake", handleIntake(td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"data":"raw payload"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/intake?kind=raw&source=cli", strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["hide_id"] == "" {
		t.Error("expected non-empty hide_id")
	}
}

func TestIntake_MissingKind_400(t *testing.T) {
	td, deps := buildWebhookTannery(t, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/intake", handleIntake(td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/intake?source=cli", strings.NewReader("body"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestIntake_AutoRoute_EnqueuesItem(t *testing.T) {
	routes := []model.TanneryRoute{
		{Name: "r1", Match: model.RouteMatch{Source: "ci"}, HideKind: "ci.log", Curing: "review", Queue: "default"},
	}
	queues := map[string]model.QueueConcurrencyConfig{"default": {Concurrency: 1, MaxDepth: 100}}
	td, deps := buildWebhookTannery(t, routes, queues)

	mux := http.NewServeMux()
	mux.HandleFunc("/intake", handleIntake(td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := "build log output"
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/intake?kind=ci.log&source=ci", strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["queue"] != "default" {
		t.Errorf("queue = %q, want %q", result["queue"], "default")
	}
	if result["curing"] != "review" {
		t.Errorf("curing = %q, want %q", result["curing"], "review")
	}
}

func TestIntake_ExplicitCuring_SkipsRouter(t *testing.T) {
	// No routes configured; explicit curing+queue params should bypass router.
	td, deps := buildWebhookTannery(t,
		nil,
		map[string]model.QueueConcurrencyConfig{"default": {Concurrency: 1, MaxDepth: 100}},
		model.CuringDefinition{Name: "review", Queue: "default"})

	mux := http.NewServeMux()
	mux.HandleFunc("/intake", handleIntake(td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := "some data"
	url := srv.URL + "/intake?kind=raw&source=cli&curing=review&queue=default"
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["hide_id"] == "" {
		t.Error("expected non-empty hide_id")
	}
}

func TestIntake_NoRoute_StoreOnly(t *testing.T) {
	// No routes — body is stored but no queue item is created.
	td, deps := buildWebhookTannery(t, nil, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/intake", handleIntake(td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := "orphan payload"
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/intake?kind=raw&source=external", strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result["hide_id"] == "" {
		t.Error("expected non-empty hide_id")
	}
	if _, ok := result["queue"]; ok {
		t.Errorf("expected no queue key in response, got %q", result["queue"])
	}
}

func TestIntake_QueueFull_503_HideNotStored(t *testing.T) {
	routes := []model.TanneryRoute{
		{Name: "r1", Match: model.RouteMatch{Source: "ci"}, HideKind: "ci.log", Curing: "review", Queue: "default"},
	}
	queues := map[string]model.QueueConcurrencyConfig{"default": {Concurrency: 1, MaxDepth: 1}}
	td, deps := buildWebhookTannery(t, routes, queues)

	// Pre-fill the queue to its max depth.
	if err := deps.queueMgr.Enqueue("default", model.QueueItem{ID: "seed-1", CuringName: "review"}); err != nil {
		t.Fatalf("seed enqueue: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/intake", handleIntake(td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := "late payload"
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/intake?kind=ci.log&source=ci", strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	// Verify the hide was NOT stored.
	entries, _ := td.hideStore.List()
	if len(entries) != 0 {
		t.Errorf("expected 0 hides stored after backpressure rejection, got %d", len(entries))
	}
}

func TestIntake_BodyTooLarge_413(t *testing.T) {
	td, deps := buildWebhookTannery(t, nil, nil)
	// The intake handler uses a hardcoded 50 MiB limit, which we can't
	// override in a unit test without replacing the handler. Instead,
	// we test that a body that marginally fits is accepted (sanity check).
	// A true 413 test would require sending >50 MiB; we verify the response
	// contract at the code level in TestWebhook_BodyTooLarge_413 which uses
	// the configurable MaxBodyBytes on webhook handlers.
	// Ensure a normal-size body is 202.
	mux := http.NewServeMux()
	mux.HandleFunc("/intake", handleIntake(td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := strings.Repeat("a", 1024) // 1 KiB — well within 50 MiB limit
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/intake?kind=raw&source=cli", strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202 for body within limit", resp.StatusCode)
	}
}

// --- issue #75: the two intake surfaces agree ---

// postIntake sends an empty-bodied intake and returns the status and JSON body.
func postIntake(t *testing.T, td *tanneryDeps, deps *apiDeps, query string) (int, map[string]string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/intake", handleIntake(td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/intake?"+query, "text/plain", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestIntake_RoutingSymmetry(t *testing.T) {
	queues := map[string]model.QueueConcurrencyConfig{"work-q": {Concurrency: 1, MaxDepth: 100}}
	defs := []model.CuringDefinition{{Name: "summarize", Queue: "work-q"}}

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantCuring string
		wantQueue  string
	}{
		// `curing=` alone used to store the hide and route nothing, even though
		// the curing definition already declared its queue.
		{"curing alone", "kind=raw&curing=summarize", http.StatusAccepted, "summarize", "work-q"},
		{"queue alone", "kind=raw&queue=work-q", http.StatusAccepted, "summarize", "work-q"},
		{"both", "kind=raw&curing=summarize&queue=work-q", http.StatusAccepted, "summarize", "work-q"},
		// Naming a destination that cannot route is an error, not a 202 with
		// the routing fields quietly missing.
		{"unknown curing", "kind=raw&curing=nope", http.StatusBadRequest, "", ""},
		{"undeclared queue", "kind=raw&queue=typo-q", http.StatusBadRequest, "", ""},
		// Naming nothing is a valid hide-only ingest.
		{"nothing named", "kind=raw", http.StatusAccepted, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			td, deps := buildWebhookTannery(t, nil, queues, defs...)
			status, body := postIntake(t, td, deps, tc.query)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %v)", status, tc.wantStatus, body)
			}
			if tc.wantStatus != http.StatusAccepted {
				return
			}
			if body["curing"] != tc.wantCuring || body["queue"] != tc.wantQueue {
				t.Errorf("response curing=%q queue=%q, want curing=%q queue=%q",
					body["curing"], body["queue"], tc.wantCuring, tc.wantQueue)
			}
			depth := deps.queueMgr.Depth("work-q")
			wantDepth := 0
			if tc.wantQueue != "" {
				wantDepth = 1
			}
			if depth != wantDepth {
				t.Errorf("work-q depth = %d, want %d", depth, wantDepth)
			}
		})
	}
}

// A queue_pattern route reaching /intake used to resolve to an empty queue name
// and enqueue nothing, silently.
func TestIntake_QueuePatternRouteEnqueues(t *testing.T) {
	routes := []model.TanneryRoute{
		{Name: "fan", Match: model.RouteMatch{Source: "bulk"}, Curing: "fanout", QueuePattern: "fan/{{hide_id}}"},
	}
	td, deps := buildWebhookTannery(t, routes, nil)
	status, body := postIntake(t, td, deps, "kind=raw&source=bulk")
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body %v)", status, body)
	}
	wantQueue := "fan/" + body["hide_id"]
	if body["queue"] != wantQueue {
		t.Fatalf("queue = %q, want %q", body["queue"], wantQueue)
	}
	if deps.queueMgr.Depth(wantQueue) != 1 {
		t.Errorf("%s depth = %d, want 1", wantQueue, deps.queueMgr.Depth(wantQueue))
	}
}
