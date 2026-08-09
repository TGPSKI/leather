package curing

import (
	"strings"
	"testing"

	"github.com/TGPSKI/leather/internal/model"
)

func testRoutingRequest() RoutingRequest {
	return RoutingRequest{
		Source: "cli",
		Router: NewRouter([]model.TanneryRoute{
			{Name: "r1", Match: model.RouteMatch{Source: "github"}, Curing: "review", Queue: "review-q"},
			{Name: "r2", Match: model.RouteMatch{Source: "bulk"}, Curing: "fanout", QueuePattern: "fan/{{hide_id}}"},
		}),
		Defs: []model.CuringDefinition{
			{Name: "review", Queue: "review-q"},
			{Name: "write", Queue: "write-q"},
			{Name: "fanout", QueuePrefix: "fan/"},
		},
		Queues: map[string]model.QueueConcurrencyConfig{
			"review-q": {Concurrency: 1},
			"write-q":  {Concurrency: 1},
		},
	}
}

// The routing table from issue #75: every cell is what both surfaces now do.
func TestResolveRouting(t *testing.T) {
	tests := []struct {
		name       string
		curing     string
		queue      string
		source     string
		wantCuring string
		wantQueue  string
		wantErr    string
	}{
		{
			name: "curing alone resolves to its declared queue",
			// The routing fact was always derivable — the curing definition
			// carries it — but neither surface used to consult it.
			curing: "write", wantCuring: "write", wantQueue: "write-q",
		},
		{
			name:  "queue alone routes and picks up its consuming curing",
			queue: "write-q", wantCuring: "write", wantQueue: "write-q",
		},
		{
			name: "curing and queue together", curing: "write", queue: "review-q",
			wantCuring: "write", wantQueue: "review-q",
		},
		{
			name: "neither, no route match: hide only",
			// Not an error: naming no destination is a valid hide-only ingest.
		},
		{
			name: "neither, route matches", source: "github",
			wantCuring: "review", wantQueue: "review-q",
		},
		{
			name:    "unknown curing errors",
			curing:  "nope",
			wantErr: `unknown curing "nope"`,
		},
		{
			name:    "undeclared queue errors",
			queue:   "typo-q",
			wantErr: `queue "typo-q" is not declared`,
		},
		{
			name:    "curing with only single-use queues needs an explicit queue",
			curing:  "fanout",
			wantErr: "single-use queues",
		},
		{
			name:   "single-use queue name is accepted under its prefix",
			curing: "fanout", queue: "fan/abc",
			wantCuring: "fanout", wantQueue: "fan/abc",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := testRoutingRequest()
			req.Curing, req.Queue = tc.curing, tc.queue
			if tc.source != "" {
				req.Source = tc.source
			}
			got, err := ResolveRouting(req)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("ResolveRouting = %+v, want error %q", got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveRouting: %v", err)
			}
			if got.Curing != tc.wantCuring || got.Queue != tc.wantQueue {
				t.Errorf("routing = curing:%q queue:%q, want curing:%q queue:%q",
					got.Curing, got.Queue, tc.wantCuring, tc.wantQueue)
			}
			if got.Routed() != (tc.wantQueue != "") {
				t.Errorf("Routed() = %v, want %v", got.Routed(), tc.wantQueue != "")
			}
		})
	}
}

// A queue_pattern route reaches intake as a pattern and expands once the hide
// exists; before, the intake path read route.Queue (empty) and enqueued nothing.
func TestResolveRouting_QueuePatternRoute(t *testing.T) {
	req := testRoutingRequest()
	req.Source = "bulk"
	got, err := ResolveRouting(req)
	if err != nil {
		t.Fatalf("ResolveRouting: %v", err)
	}
	if !got.Routed() {
		t.Fatal("pattern route did not resolve to a destination")
	}
	if q := got.QueueFor("hide_123"); q != "fan/hide_123" {
		t.Errorf("QueueFor = %q, want %q", q, "fan/hide_123")
	}
}

// A queue named by a curing that has no static queue of its own must still
// resolve when it falls inside that curing's single-use namespace.
func TestResolveRouting_UnknownQueueNamesDeclaredOnes(t *testing.T) {
	req := testRoutingRequest()
	req.Queue = "typo-q"
	_, err := ResolveRouting(req)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"review-q", "write-q"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not list declared queue %q", err, want)
		}
	}
}
