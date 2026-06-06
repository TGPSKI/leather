package tool

import (
	"context"
	"testing"
	"time"
)

// TestParseRateSpec covers valid and invalid rate spec strings.
func TestParseRateSpec(t *testing.T) {
	cases := []struct {
		spec        string
		wantBurst   int
		wantErrText string
	}{
		{"1/s", 1, ""},
		{"60/m", 60, ""},
		{"5000/h", 5000, ""},
		{"10/S", 10, ""}, // case-insensitive unit
		{"10/M", 10, ""},
		{"1/H", 1, ""},
		{"0/s", 0, "positive integer"},   // zero count
		{"-1/s", 0, "positive integer"},  // negative
		{"abc/s", 0, "positive integer"}, // non-numeric count
		{"10/x", 0, "unknown unit"},      // bad unit
		{"10", 0, "missing '/'"},         // no slash
		{"", 0, "missing '/'"},           // empty
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			interval, burst, err := parseRateSpec(tc.spec)
			if tc.wantErrText != "" {
				if err == nil {
					t.Fatalf("parseRateSpec(%q): want error containing %q, got nil", tc.spec, tc.wantErrText)
				}
				if !containsStr(err.Error(), tc.wantErrText) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tc.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRateSpec(%q): unexpected error: %v", tc.spec, err)
			}
			if burst != tc.wantBurst {
				t.Errorf("burst = %d, want %d", burst, tc.wantBurst)
			}
			if interval <= 0 {
				t.Errorf("interval = %v, want > 0", interval)
			}
		})
	}
}

// TestNewHostLimiter_InvalidSpec verifies that a bad rate spec returns an error.
func TestNewHostLimiter_InvalidSpec(t *testing.T) {
	_, err := NewHostLimiter(map[string]string{"example.com": "bad"})
	if err == nil {
		t.Fatal("expected error for invalid spec, got nil")
	}
}

// TestHostLimiter_NilPassThrough verifies that a nil limiter's Wait returns
// immediately without blocking or erroring.
func TestHostLimiter_NilPassThrough(t *testing.T) {
	var l *HostLimiter
	waited, err := l.Wait(context.Background(), "any.host")
	if err != nil {
		t.Errorf("Wait on nil limiter returned error: %v", err)
	}
	if waited {
		t.Error("Wait on nil limiter reported waited=true, want false")
	}
}

// TestHostLimiter_UnknownHostPassThrough verifies that a host with no configured
// limit passes through immediately.
func TestHostLimiter_UnknownHostPassThrough(t *testing.T) {
	l, err := NewHostLimiter(map[string]string{"other.host": "1/s"})
	if err != nil {
		t.Fatalf("NewHostLimiter: %v", err)
	}
	waited, err := l.Wait(context.Background(), "unknown.host")
	if err != nil {
		t.Errorf("Wait returned error: %v", err)
	}
	if waited {
		t.Error("Wait for unconfigured host reported waited=true, want false")
	}
}

// TestHostLimiter_FirstCallImmediate verifies that the first call on a fresh
// bucket does not block (burst token available).
func TestHostLimiter_FirstCallImmediate(t *testing.T) {
	l, err := NewHostLimiter(map[string]string{"api.example.com": "1/s"})
	if err != nil {
		t.Fatalf("NewHostLimiter: %v", err)
	}
	start := time.Now()
	waited, err := l.Wait(context.Background(), "api.example.com")
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	elapsed := time.Since(start)
	if waited {
		t.Error("first call should not wait (burst token available)")
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("first call took %v, want < 50ms", elapsed)
	}
}

// TestHostLimiter_ThrottledSecondCall verifies that with a 1/s limit the second
// immediate call blocks until the next token is available.
func TestHostLimiter_ThrottledSecondCall(t *testing.T) {
	l, err := NewHostLimiter(map[string]string{"slow.example.com": "2/s"})
	if err != nil {
		t.Fatalf("NewHostLimiter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Exhaust the burst (2 tokens).
	for i := 0; i < 2; i++ {
		if _, err := l.Wait(ctx, "slow.example.com"); err != nil {
			t.Fatalf("Wait %d: %v", i, err)
		}
	}

	// Third call should block briefly (waiting for next token at ~500ms interval).
	start := time.Now()
	waited, err := l.Wait(ctx, "slow.example.com")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Wait throttled: %v", err)
	}
	if !waited {
		t.Error("third call should have waited, got waited=false")
	}
	// Interval for 2/s is 500ms; we just need some positive wait.
	if elapsed < time.Millisecond {
		t.Errorf("throttled call took %v, want >= 1ms", elapsed)
	}
}

// TestHostLimiter_ContextCancel verifies that cancelling the context while
// waiting for a token returns the context error.
func TestHostLimiter_ContextCancel(t *testing.T) {
	l, err := NewHostLimiter(map[string]string{"blocked.example.com": "1/h"})
	if err != nil {
		t.Fatalf("NewHostLimiter: %v", err)
	}

	// Consume the only burst token.
	ctx := context.Background()
	if _, err := l.Wait(ctx, "blocked.example.com"); err != nil {
		t.Fatalf("first Wait: %v", err)
	}

	// Next call should block for ~1h; cancel it immediately.
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = l.Wait(cancelCtx, "blocked.example.com")
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
