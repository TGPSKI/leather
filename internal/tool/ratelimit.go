package tool

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HostLimiter applies per-host token-bucket rate limiting.
// Each host bucket allows up to burst tokens immediately; thereafter one token
// is added every interval. The zero value (nil pointer) is safe to call — Wait
// returns immediately when the limiter is nil.
type HostLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

// tokenBucket is a simple stdlib-only token bucket per host.
type tokenBucket struct {
	interval time.Duration // time between token refills
	burst    int           // maximum tokens (also initial fill)
	tokens   int
	last     time.Time
}

// take attempts to consume one token. It returns (true, wait) where wait is 0
// if a token was available immediately, or the duration to sleep before the
// next token arrives. The caller is responsible for sleeping.
func (b *tokenBucket) take(now time.Time) (immediate bool, wait time.Duration) {
	// Refill tokens based on elapsed time.
	elapsed := now.Sub(b.last)
	if elapsed >= b.interval {
		added := int(elapsed / b.interval)
		b.tokens += added
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.last = b.last.Add(time.Duration(added) * b.interval)
	}

	if b.tokens > 0 {
		b.tokens--
		return true, 0
	}
	// Calculate wait until the next token.
	wait = b.interval - now.Sub(b.last)
	return false, wait
}

// NewHostLimiter builds a HostLimiter from a host→rateSpec map.
// Each rateSpec is "N/s", "N/m", or "N/h" where N is the token count per period.
// An empty map returns a limiter that never blocks.
func NewHostLimiter(specs map[string]string) (*HostLimiter, error) {
	l := &HostLimiter{buckets: make(map[string]*tokenBucket, len(specs))}
	for host, spec := range specs {
		interval, burst, err := parseRateSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("host limiter %q: %w", host, err)
		}
		l.buckets[host] = &tokenBucket{
			interval: interval,
			burst:    burst,
			tokens:   burst,
			last:     time.Now(),
		}
	}
	return l, nil
}

// Wait blocks until the token bucket for host allows one more request, or until
// ctx is cancelled. It returns (true, nil) when it had to wait, (false, nil)
// when the token was available immediately, and (false, err) on context cancel.
// Hosts with no configured limit pass through immediately.
func (l *HostLimiter) Wait(ctx context.Context, host string) (waited bool, err error) {
	if l == nil {
		return false, nil
	}
	l.mu.Lock()
	b, ok := l.buckets[host]
	if !ok {
		l.mu.Unlock()
		return false, nil
	}
	immediate, wait := b.take(time.Now())
	l.mu.Unlock()

	if immediate {
		return false, nil
	}

	// Need to wait for a token.
	select {
	case <-time.After(wait):
		// Consume the token that just became available.
		l.mu.Lock()
		b.take(time.Now())
		l.mu.Unlock()
		return true, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// parseRateSpec parses "N/s", "N/m", or "N/h" into a (interval, burst) pair.
// interval is the time between individual token refills (period / N).
// burst is N (the maximum sustained rate per period equals N tokens).
func parseRateSpec(spec string) (interval time.Duration, burst int, err error) {
	spec = strings.TrimSpace(spec)
	slash := strings.LastIndex(spec, "/")
	if slash < 0 {
		return 0, 0, fmt.Errorf("rate spec %q: missing '/' separator (want N/s, N/m, or N/h)", spec)
	}
	countStr := strings.TrimSpace(spec[:slash])
	unit := strings.TrimSpace(spec[slash+1:])

	n, convErr := strconv.Atoi(countStr)
	if convErr != nil || n <= 0 {
		return 0, 0, fmt.Errorf("rate spec %q: count must be a positive integer", spec)
	}

	var period time.Duration
	switch strings.ToLower(unit) {
	case "s":
		period = time.Second
	case "m":
		period = time.Minute
	case "h":
		period = time.Hour
	default:
		return 0, 0, fmt.Errorf("rate spec %q: unknown unit %q (want s, m, or h)", spec, unit)
	}

	// interval = period / N so that N tokens are available per period.
	interval = period / time.Duration(n)
	if interval < time.Millisecond {
		interval = time.Millisecond // floor to avoid spinning
	}
	return interval, n, nil
}
