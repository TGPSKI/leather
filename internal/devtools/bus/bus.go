package bus

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

const defaultSubscriberBuffer = 64

// Bus is an in-process fan-out bus backed by a bounded ring buffer.
type Bus struct {
	mu sync.Mutex

	capacity int
	ring     []Event
	head     int
	size     int

	lastSeq uint64

	published uint64
	evicted   uint64
	// dropped is incremented from fan-out paths that may run outside the
	// main mutex; use atomic operations for it.
	dropped uint64

	nextSubID   uint64
	subscribers map[uint64]chan Event
}

// New constructs a Bus with the requested ring capacity.
func New(capacity int) *Bus {
	if capacity <= 0 {
		capacity = 1
	}
	return &Bus{
		capacity:    capacity,
		ring:        make([]Event, capacity),
		subscribers: make(map[uint64]chan Event),
	}
}

// Publish appends ev to the ring and fans out to active subscribers.
//
// The fan-out sends happen after the subscriber map snapshot is taken and
// the bus mutex is released, so a slow or full subscriber channel cannot
// block other Publish calls or Subscribe/Unsubscribe operations.
func (b *Bus) Publish(ev Event) uint64 {
	b.mu.Lock()
	ev = normalizeEvent(ev)
	if ev.At == 0 {
		ev.At = time.Now().Unix()
	}
	b.lastSeq++
	ev.Seq = b.lastSeq

	b.append(ev)
	b.published++

	// Snapshot subscriber channels under the lock, then send outside.
	subs := make([]chan Event, 0, len(b.subscribers))
	for _, sub := range b.subscribers {
		subs = append(subs, sub)
	}
	seq := ev.Seq
	b.mu.Unlock()

	for _, sub := range subs {
		b.sendDropOldest(sub, ev)
	}

	return seq
}

// AppendCause links parent as a cause of child when child is still in the ring.
func (b *Bus) AppendCause(parent, child uint64) bool {
	if parent == 0 || child == 0 {
		return false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	idx, ok := b.findBySeq(child)
	if !ok {
		return false
	}
	for _, seq := range b.ring[idx].CausesSeq {
		if seq == parent {
			return true
		}
	}
	b.ring[idx].CausesSeq = append(b.ring[idx].CausesSeq, parent)
	return true
}

// Subscribe returns a stream of events newer than fromSeq, plus live updates.
//
// The subscription is cleaned up when ctx is cancelled. Callers that cannot
// supply a cancellable context (e.g. background bookkeeping) should use
// SubscribeWithCloser to avoid leaking the cleanup goroutine and channel.
func (b *Bus) Subscribe(ctx context.Context, fromSeq uint64) <-chan Event {
	ch, _ := b.subscribeWithCloser(ctx, fromSeq)
	return ch
}

// SubscribeWithCloser is like Subscribe but also returns a closer function.
// Calling the closer (idempotent) unregisters the subscriber and stops the
// cleanup goroutine, even if ctx is never cancelled. Callers should call it
// when the subscription is no longer needed (e.g. when an SSE request
// handler returns).
func (b *Bus) SubscribeWithCloser(ctx context.Context, fromSeq uint64) (<-chan Event, func()) {
	return b.subscribeWithCloser(ctx, fromSeq)
}

func (b *Bus) subscribeWithCloser(ctx context.Context, fromSeq uint64) (<-chan Event, func()) {
	ch := make(chan Event, defaultSubscriberBuffer)
	done := make(chan struct{})

	b.mu.Lock()
	backlog := make([]Event, 0)
	for _, ev := range b.snapshotLocked() {
		if ev.Seq > fromSeq {
			backlog = append(backlog, ev)
		}
	}
	subID := b.nextSubID
	b.nextSubID++
	b.subscribers[subID] = ch
	b.mu.Unlock()

	// Replay backlog without holding the lock.
	for _, ev := range backlog {
		b.sendDropOldest(ch, ev)
	}

	var once sync.Once
	closer := func() {
		once.Do(func() {
			b.mu.Lock()
			if sub, ok := b.subscribers[subID]; ok {
				delete(b.subscribers, subID)
				close(sub)
			}
			b.mu.Unlock()
			close(done)
		})
	}

	go func() {
		select {
		case <-ctx.Done():
		case <-done:
		}
		closer()
	}()

	return ch, closer
}

// Snapshot returns all buffered events ordered from oldest to newest.
func (b *Bus) Snapshot() []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.snapshotLocked()
}

// Stats returns a point-in-time bus summary.
func (b *Bus) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()

	stats := Stats{
		Capacity:    b.capacity,
		Size:        b.size,
		Published:   b.published,
		Evicted:     b.evicted,
		Dropped:     atomic.LoadUint64(&b.dropped),
		Subscribers: len(b.subscribers),
	}
	if b.size > 0 {
		stats.OldestSeq = b.ring[b.head].Seq
		last := (b.head + b.size - 1) % b.capacity
		stats.NewestSeq = b.ring[last].Seq
	}
	return stats
}

func (b *Bus) append(ev Event) {
	if b.size < b.capacity {
		idx := (b.head + b.size) % b.capacity
		b.ring[idx] = ev
		b.size++
		return
	}

	b.ring[b.head] = ev
	b.head = (b.head + 1) % b.capacity
	b.evicted++
}

// sendDropOldest delivers ev to ch, dropping the oldest queued event when
// the channel is full. It is safe to call outside the bus mutex; the dropped
// counter uses atomic operations.
func (b *Bus) sendDropOldest(ch chan Event, ev Event) {
	defer func() {
		// Closed-channel sends panic; tolerate to avoid crashing Publish
		// during a race with subscriber close.
		_ = recover()
	}()

	select {
	case ch <- ev:
		return
	default:
	}

	select {
	case <-ch:
		atomic.AddUint64(&b.dropped, 1)
	default:
		atomic.AddUint64(&b.dropped, 1)
	}

	select {
	case ch <- ev:
	default:
		atomic.AddUint64(&b.dropped, 1)
	}
}

func (b *Bus) snapshotLocked() []Event {
	out := make([]Event, 0, b.size)
	for i := 0; i < b.size; i++ {
		idx := (b.head + i) % b.capacity
		out = append(out, cloneEvent(b.ring[idx]))
	}
	return out
}

func (b *Bus) findBySeq(seq uint64) (int, bool) {
	for i := 0; i < b.size; i++ {
		idx := (b.head + i) % b.capacity
		if b.ring[idx].Seq == seq {
			return idx, true
		}
	}
	return 0, false
}

func normalizeEvent(ev Event) Event {
	out := ev
	out.Seq = 0
	if len(ev.CausesSeq) > 0 {
		out.CausesSeq = append([]uint64(nil), ev.CausesSeq...)
	}
	if len(ev.Payload) > 0 {
		out.Payload = append([]byte(nil), ev.Payload...)
	}
	return out
}

func cloneEvent(ev Event) Event {
	out := ev
	if len(ev.CausesSeq) > 0 {
		out.CausesSeq = append([]uint64(nil), ev.CausesSeq...)
	}
	if len(ev.Payload) > 0 {
		out.Payload = append([]byte(nil), ev.Payload...)
	}
	return out
}
