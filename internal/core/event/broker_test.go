package event_test

import (
	"sync"
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/event"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// drain reads up to n events from ch within timeout, returning what it got.
func drain[T any](ch <-chan T, n int, timeout time.Duration) []T {
	out := make([]T, 0, n)
	deadline := time.After(timeout)
	for len(out) < n {
		select {
		case v, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, v)
		case <-deadline:
			return out
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// TestBroker_SubscribeAndPublish — basic single-subscriber round-trip
// ---------------------------------------------------------------------------

func TestBroker_SubscribeAndPublish(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[string]()
	defer b.Close()

	sub := b.Subscribe(4)
	defer sub.Cancel()

	b.Publish("hello")
	b.Publish("world")

	got := drain(sub.Ch(), 2, time.Second)
	require.Len(t, got, 2)
	assert.Equal(t, "hello", got[0])
	assert.Equal(t, "world", got[1])
}

// ---------------------------------------------------------------------------
// TestBroker_MultipleSubscribers — every subscriber receives every event
// ---------------------------------------------------------------------------

func TestBroker_MultipleSubscribers(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	defer b.Close()

	const numSubs = 5
	subs := make([]*event.Subscription[int], numSubs)
	for i := range subs {
		subs[i] = b.Subscribe(8)
		defer subs[i].Cancel()
	}

	b.Publish(42)

	for i, sub := range subs {
		got := drain(sub.Ch(), 1, time.Second)
		require.Len(t, got, 1, "subscriber %d should receive the event", i)
		assert.Equal(t, 42, got[0])
	}
}

// ---------------------------------------------------------------------------
// TestBroker_NonBlockingPublish — a full subscriber buffer must never stall
// the publisher
// ---------------------------------------------------------------------------

func TestBroker_NonBlockingPublish(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	defer b.Close()

	// Buffer of 1 — will be full after the first event if nobody reads.
	slow := b.Subscribe(1)
	defer slow.Cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Publish far more events than the buffer can hold.
		for i := range 100 {
			b.Publish(i)
		}
	}()

	select {
	case <-done:
		// pass — publish loop completed without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on slow subscriber")
	}
}

// ---------------------------------------------------------------------------
// TestBroker_Cancel — cancelled subscriber stops receiving; channel is closed
// ---------------------------------------------------------------------------

func TestBroker_Cancel(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[string]()
	defer b.Close()

	sub := b.Subscribe(8)
	b.Publish("before-cancel")

	// Drain the first event so the channel is empty before Cancel.
	got := drain(sub.Ch(), 1, time.Second)
	require.Len(t, got, 1)
	assert.Equal(t, "before-cancel", got[0])

	sub.Cancel()

	// After Cancel the channel must be closed (range terminates).
	b.Publish("after-cancel")

	// Channel should be closed; a receive on a closed channel returns immediately.
	select {
	case _, ok := <-sub.Ch():
		assert.False(t, ok, "channel must be closed after Cancel")
	case <-time.After(time.Second):
		t.Fatal("channel was not closed after Cancel")
	}
}

// ---------------------------------------------------------------------------
// TestBroker_CancelIdempotent — Cancel called twice must not panic
// ---------------------------------------------------------------------------

func TestBroker_CancelIdempotent(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	defer b.Close()

	sub := b.Subscribe(4)
	assert.NotPanics(t, func() {
		sub.Cancel()
		sub.Cancel()
	})
}

// ---------------------------------------------------------------------------
// TestBroker_Close — Close shuts down the broker and closes all channels
// ---------------------------------------------------------------------------

func TestBroker_Close(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()

	sub1 := b.Subscribe(4)
	sub2 := b.Subscribe(4)

	b.Close()

	// Both channels must be closed after Close.
	for i, ch := range []<-chan int{sub1.Ch(), sub2.Ch()} {
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "sub%d channel must be closed after broker Close", i+1)
		case <-time.After(time.Second):
			t.Fatalf("sub%d channel was not closed after broker Close", i+1)
		}
	}
}

// ---------------------------------------------------------------------------
// TestBroker_CloseIdempotent — Close called twice must not panic
// ---------------------------------------------------------------------------

func TestBroker_CloseIdempotent(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	assert.NotPanics(t, func() {
		b.Close()
		b.Close()
	})
}

// ---------------------------------------------------------------------------
// TestBroker_SubscribeAfterClose — subscribing to a closed broker returns a
// channel that is already closed
// ---------------------------------------------------------------------------

func TestBroker_SubscribeAfterClose(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[string]()
	b.Close()

	sub := b.Subscribe(4)

	select {
	case _, ok := <-sub.Ch():
		assert.False(t, ok, "channel returned after Close must already be closed")
	case <-time.After(time.Second):
		t.Fatal("channel returned after Close was not closed")
	}
}

// ---------------------------------------------------------------------------
// TestBroker_PublishAfterClose — Publish on a closed broker must not panic
// ---------------------------------------------------------------------------

func TestBroker_PublishAfterClose(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	b.Close()

	assert.NotPanics(t, func() {
		b.Publish(1)
	})
}

// ---------------------------------------------------------------------------
// TestBroker_ConcurrentPublish — multiple goroutines publish concurrently;
// all events must be delivered (or at least publishing must not panic/race)
// ---------------------------------------------------------------------------

func TestBroker_ConcurrentPublish(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	defer b.Close()

	const (
		numPublishers = 8
		eventsEach    = 50
		bufSize       = numPublishers * eventsEach
	)

	sub := b.Subscribe(bufSize)
	defer sub.Cancel()

	var wg sync.WaitGroup
	wg.Add(numPublishers)
	for p := range numPublishers {
		go func(p int) {
			defer wg.Done()
			for i := range eventsEach {
				b.Publish(p*1000 + i)
			}
		}(p)
	}
	wg.Wait()

	// We published numPublishers*eventsEach events into a buffer of that size;
	// all of them must have arrived (buffer was large enough to hold everything).
	got := drain(sub.Ch(), bufSize, 2*time.Second)
	assert.Len(t, got, bufSize, "all events must be received when buffer is large enough")
}

// ---------------------------------------------------------------------------
// TestBroker_CancelledSubDoesNotReceive — a cancelled subscription is removed
// from the broker's routing table; subsequent publishes must not attempt to
// send to its closed channel (no panic, no goroutine leak)
// ---------------------------------------------------------------------------

func TestBroker_CancelledSubDoesNotReceive(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	defer b.Close()

	const numEvents = 10
	// Buffer is large enough to receive all events without dropping.
	active := b.Subscribe(numEvents)
	defer active.Cancel()

	cancelled := b.Subscribe(numEvents)
	// Cancel is synchronous; by the time it returns the sub is removed.
	cancelled.Cancel()

	// Publishing after Cancel must not panic (send on closed channel).
	assert.NotPanics(t, func() {
		for i := range numEvents {
			b.Publish(i)
		}
	})

	// The active subscriber must receive every event.
	got := drain(active.Ch(), numEvents, time.Second)
	assert.Len(t, got, numEvents)
}

// ---------------------------------------------------------------------------
// TestBroker_ZeroBufferSize — bufSize=0 is an edge case; broker must not panic
// and the channel behaves as unbuffered (events drop immediately if nobody reads)
// ---------------------------------------------------------------------------

func TestBroker_ZeroBufferSize(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	defer b.Close()

	sub := b.Subscribe(0)
	defer sub.Cancel()

	// Must not block even with unbuffered channel.
	assert.NotPanics(t, func() {
		b.Publish(1)
	})
}

// ---------------------------------------------------------------------------
// TestBroker_HasSubscribers — tracks active subscriber count correctly
// ---------------------------------------------------------------------------

func TestBroker_HasSubscribers(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	defer b.Close()

	assert.False(t, b.HasSubscribers(), "no subscribers initially")

	sub1 := b.Subscribe(4)
	assert.True(t, b.HasSubscribers(), "true after first subscribe")

	sub2 := b.Subscribe(4)
	assert.True(t, b.HasSubscribers(), "true with two subscribers")

	sub1.Cancel()
	assert.True(t, b.HasSubscribers(), "true with one remaining subscriber")

	sub2.Cancel()
	assert.False(t, b.HasSubscribers(), "false after all cancelled")
}

func TestBroker_HasSubscribers_AfterClose(t *testing.T) {
	t.Parallel()

	b := event.NewBroker[int]()
	_ = b.Subscribe(4)
	b.Close()

	assert.False(t, b.HasSubscribers(), "false after broker closed")
}

// ---------------------------------------------------------------------------
// Benchmark
// ---------------------------------------------------------------------------

func BenchmarkBroker_Publish(b *testing.B) {
	br := event.NewBroker[int]()
	defer br.Close()

	sub := br.Subscribe(b.N + 1)
	defer sub.Cancel()

	b.ResetTimer()
	for i := range b.N {
		br.Publish(i)
	}
}
