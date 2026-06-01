package cli

import (
	"testing"
)

func TestLatencyPercentiles(t *testing.T) {
	tests := []struct {
		name      string
		durations []int64
		wantP50   int64
		wantP95   int64
		wantP99   int64
	}{
		{name: "empty", durations: nil, wantP50: 0, wantP95: 0, wantP99: 0},
		{name: "one", durations: []int64{100}, wantP50: 100, wantP95: 100, wantP99: 100},
		{name: "known_set", durations: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, wantP50: 5, wantP95: 10, wantP99: 10},
		{name: "two_elements", durations: []int64{10, 20}, wantP50: 10, wantP95: 20, wantP99: 20},
		{name: "unsorted", durations: []int64{50, 10, 30, 20, 40}, wantP50: 30, wantP95: 50, wantP99: 50},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p50, p95, p99 := latencyPercentiles(tc.durations)
			if p50 != tc.wantP50 {
				t.Errorf("p50 got %d, want %d", p50, tc.wantP50)
			}
			if p95 != tc.wantP95 {
				t.Errorf("p95 got %d, want %d", p95, tc.wantP95)
			}
			if p99 != tc.wantP99 {
				t.Errorf("p99 got %d, want %d", p99, tc.wantP99)
			}
		})
	}
}

func TestAgentHistoryDurations(t *testing.T) {
	t.Helper()
	h := &agentHistory{}
	if got := h.durations(); len(got) != 0 {
		t.Fatalf("expected empty durations, got %v", got)
	}
}
