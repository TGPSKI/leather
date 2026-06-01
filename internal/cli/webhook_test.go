package cli

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tgpski/leather/internal/artifact"
	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/curing"
	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
)

// buildWebhookTannery constructs a minimal tanneryDeps and apiDeps for webhook tests.
// routes are installed on the router; webhook endpoints are not set here — callers build them.
func buildWebhookTannery(t *testing.T, routes []model.TanneryRoute, queues map[string]model.QueueConcurrencyConfig) (*tanneryDeps, *apiDeps) {
	t.Helper()
	dir := t.TempDir()
	hideDir := dir + "/hides"
	artDir := dir + "/artifacts"
	queueDir := dir + "/queues"

	td := &tanneryDeps{
		hideStore:    hide.NewStore(hideDir),
		artStore:     artifact.NewStore(artDir),
		curingRouter: curing.NewRouter(routes),
		curingDefs:   []model.CuringDefinition{},
		tannCfg: config.TanneryConfig{
			HideDir:     hideDir,
			ArtifactDir: artDir,
			Routes:      routes,
			Queues:      queues,
		},
	}
	deps := &apiDeps{
		queueMgr: queue.NewManager(queueDir),
		log:      testLogger(t),
	}
	return td, deps
}

// makeHMAC computes the sha256= header value for body with secret.
func makeHMAC(t *testing.T, body, secret string) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// --- webhook handler tests ---

func TestWebhook_AcceptedWithRoute(t *testing.T) {
	routes := []model.TanneryRoute{
		{Name: "r1", Match: model.RouteMatch{Source: "github"}, HideKind: "github.pr", Curing: "review", Queue: "default"},
	}
	queues := map[string]model.QueueConcurrencyConfig{"default": {Concurrency: 1, MaxDepth: 100}}
	td, deps := buildWebhookTannery(t, routes, queues)

	secret := "test-secret"
	wh := model.WebhookConfig{Name: "gh", Path: "/webhooks/gh", Source: "github", Secret: secret}
	mux := http.NewServeMux()
	mux.HandleFunc(wh.Path, makeWebhookHandler(wh, td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"action":"opened"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+wh.Path, strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", makeHMAC(t, body, secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["hide_id"] == "" {
		t.Error("expected non-empty hide_id in response")
	}
	if result["queue"] != "default" {
		t.Errorf("queue = %q, want %q", result["queue"], "default")
	}
}

func TestWebhook_InvalidSignature(t *testing.T) {
	routes := []model.TanneryRoute{
		{Name: "r1", Match: model.RouteMatch{Source: "github"}, HideKind: "github.pr", Curing: "review", Queue: "default"},
	}
	td, deps := buildWebhookTannery(t, routes, nil)

	wh := model.WebhookConfig{Name: "gh", Path: "/webhooks/gh", Source: "github", Secret: "mysecret"}
	mux := http.NewServeMux()
	mux.HandleFunc(wh.Path, makeWebhookHandler(wh, td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"action":"opened"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+wh.Path, strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=badhex")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWebhook_MissingSignatureWhenRequired(t *testing.T) {
	routes := []model.TanneryRoute{
		{Name: "r1", Match: model.RouteMatch{Source: "github"}, HideKind: "github.pr", Curing: "review", Queue: "default"},
	}
	td, deps := buildWebhookTannery(t, routes, nil)

	wh := model.WebhookConfig{Name: "gh", Path: "/webhooks/gh", Source: "github", Secret: "mysecret"}
	mux := http.NewServeMux()
	mux.HandleFunc(wh.Path, makeWebhookHandler(wh, td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"action":"opened"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+wh.Path, strings.NewReader(body))
	// No X-Hub-Signature-256 header.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWebhook_ValidSignature(t *testing.T) {
	routes := []model.TanneryRoute{
		{Name: "r1", Match: model.RouteMatch{Source: "github"}, HideKind: "github.pr", Curing: "review", Queue: "default"},
	}
	queues := map[string]model.QueueConcurrencyConfig{"default": {Concurrency: 1, MaxDepth: 100}}
	td, deps := buildWebhookTannery(t, routes, queues)

	secret := "supersecret"
	wh := model.WebhookConfig{Name: "gh", Path: "/webhooks/gh", Source: "github", Secret: secret}
	mux := http.NewServeMux()
	mux.HandleFunc(wh.Path, makeWebhookHandler(wh, td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"action":"opened"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+wh.Path, strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", makeHMAC(t, body, secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
}

func TestWebhook_NoSecret_Rejected(t *testing.T) {
	// Fail-closed: a webhook with no configured secret must reject all unsigned requests.
	routes := []model.TanneryRoute{
		{Name: "r1", Match: model.RouteMatch{Source: "github"}, HideKind: "github.pr", Curing: "review", Queue: "default"},
	}
	queues := map[string]model.QueueConcurrencyConfig{"default": {Concurrency: 1, MaxDepth: 100}}
	td, deps := buildWebhookTannery(t, routes, queues)

	wh := model.WebhookConfig{Name: "gh", Path: "/webhooks/gh", Source: "github", Secret: ""}
	mux := http.NewServeMux()
	mux.HandleFunc(wh.Path, makeWebhookHandler(wh, td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"data":"something"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+wh.Path, strings.NewReader(body))
	// No signature header — must now be rejected (fail-closed: empty secret = 401).
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (empty secret is fail-closed)", resp.StatusCode)
	}
}

func TestWebhook_NoRouteMatch_204(t *testing.T) {
	// Source "slack" won't match the "github" route.
	routes := []model.TanneryRoute{
		{Name: "r1", Match: model.RouteMatch{Source: "github"}, HideKind: "github.pr", Curing: "review", Queue: "default"},
	}
	td, deps := buildWebhookTannery(t, routes, nil)

	secret := "test-secret"
	wh := model.WebhookConfig{Name: "sl", Path: "/webhooks/sl", Source: "slack", Secret: secret}
	mux := http.NewServeMux()
	mux.HandleFunc(wh.Path, makeWebhookHandler(wh, td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"text":"hello"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+wh.Path, strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", makeHMAC(t, body, secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestWebhook_BodyTooLarge_413(t *testing.T) {
	td, deps := buildWebhookTannery(t, nil, nil)
	wh := model.WebhookConfig{Name: "gh", Path: "/webhooks/gh", Source: "github", MaxBodyBytes: 10}
	mux := http.NewServeMux()
	mux.HandleFunc(wh.Path, makeWebhookHandler(wh, td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := strings.Repeat("x", 20)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+wh.Path, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}

func TestWebhook_QueueFull_503(t *testing.T) {
	routes := []model.TanneryRoute{
		{Name: "r1", Match: model.RouteMatch{Source: "github"}, HideKind: "github.pr", Curing: "review", Queue: "default"},
	}
	queues := map[string]model.QueueConcurrencyConfig{"default": {Concurrency: 1, MaxDepth: 1}}
	td, deps := buildWebhookTannery(t, routes, queues)

	// Pre-fill the queue to its max depth.
	err := deps.queueMgr.Enqueue("default", model.QueueItem{ID: "seed-1", CuringName: "review"})
	if err != nil {
		t.Fatalf("seed enqueue: %v", err)
	}

	secret := "test-secret"
	wh := model.WebhookConfig{Name: "gh", Path: "/webhooks/gh", Source: "github", Secret: secret}
	mux := http.NewServeMux()
	mux.HandleFunc(wh.Path, makeWebhookHandler(wh, td, deps))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	body := `{"action":"opened"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+wh.Path, strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", makeHMAC(t, body, secret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	if ra := resp.Header.Get("Retry-After"); ra != "30" {
		t.Errorf("Retry-After = %q, want %q", ra, "30")
	}
}

// --- parseHidePath tests ---

func TestParseHidePath_Variants(t *testing.T) {
	tests := []struct {
		path    string
		wantID  string
		wantSub string
		wantPg  int
	}{
		{"/hides/abc123", "abc123", "", 0},
		{"/hides/abc123/cuts/2", "abc123", "cuts", 2},
		{"/hides/abc123/unknown", "abc123", "unknown", 0},
		{"/hides/", "", "", 0},
	}
	for _, tc := range tests {
		id, sub, pg := parseHidePath(tc.path)
		if id != tc.wantID || sub != tc.wantSub || pg != tc.wantPg {
			t.Errorf("parseHidePath(%q) = (%q,%q,%d), want (%q,%q,%d)",
				tc.path, id, sub, pg, tc.wantID, tc.wantSub, tc.wantPg)
		}
	}
}

// --- hide handler tests ---

func TestHidesList_Empty(t *testing.T) {
	td, _ := buildWebhookTannery(t, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/hides", handleHidesCollection(td))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/hides")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var list []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("len = %d, want 0", len(list))
	}
}

func TestHideDelete_204(t *testing.T) {
	td, _ := buildWebhookTannery(t, nil, nil)
	// Write a hide.
	entry, err := td.hideStore.Put("test", "cli", []byte("hello"), nil)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/hides/", dispatchHide(td))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/hides/"+entry.ID, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

func TestDispatchHide_UnknownSubresource_404(t *testing.T) {
	td, _ := buildWebhookTannery(t, nil, nil)
	entry, err := td.hideStore.Put("test", "cli", []byte("data"), nil)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/hides/", dispatchHide(td))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/hides/" + entry.ID + "/unknown")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestArtifacts_FilterByCuring(t *testing.T) {
	td, _ := buildWebhookTannery(t, nil, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/artifacts", handleArtifactsCollection(td))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/artifacts?curing=myprocess")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var arts []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&arts); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Freshly created store has no artifacts.
	if len(arts) != 0 {
		t.Errorf("len = %d, want 0", len(arts))
	}
}

func TestCuringsList(t *testing.T) {
	td, _ := buildWebhookTannery(t, nil, nil)
	td.curingDefs = []model.CuringDefinition{{Name: "review"}}

	mux := http.NewServeMux()
	mux.HandleFunc("/curings", handleCurings(td))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/curings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	var defs []model.CuringDefinition
	if err := json.NewDecoder(resp.Body).Decode(&defs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "review" {
		t.Errorf("defs = %v, want [{Name:review}]", defs)
	}
}
