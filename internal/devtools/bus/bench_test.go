package bus

import (
	"context"
	"testing"
)

func BenchmarkPublish(b *testing.B) {
	bus := New(4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(Event{Kind: "bench.publish"})
	}
}

func BenchmarkFanout10(b *testing.B) {
	bus := New(4096)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < 10; i++ {
		ch := bus.Subscribe(ctx, 0)
		go drain(ch)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Publish(Event{Kind: "bench.fanout"})
	}
}

func drain(ch <-chan Event) {
	for range ch {
	}
}
