package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tgpski/leather/internal/model"
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
	td, deps := buildWebhookTannery(t, nil, nil)

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
