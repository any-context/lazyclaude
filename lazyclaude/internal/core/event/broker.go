// Package event provides a generic, thread-safe pub/sub event broker.
// It is designed for LOCAL in-process communication (e.g. server → GUI)
// and complements the file-based notify queue used for SSH remote compatibility.
package event

import "sync"

// Broker is a generic pub/sub event broker.
// Construct one with NewBroker; the zero value is not usable.
type Broker[T any] struct {
	mu     sync.Mutex
	subs   map[uint64]*Subscription[T]
	nextID uint64
	closed bool
}

// Subscription represents a single subscriber's view of the broker.
type Subscription[T any] struct {
	id     uint64
	ch     chan T
	broker *Broker[T]
	once   sync.Once
}

// NewBroker creates a new, open Broker ready to accept publishers and subscribers.
func NewBroker[T any]() *Broker[T] {
	return &Broker[T]{
		subs: make(map[uint64]*Subscription[T]),
	}
}

// Subscribe creates a new subscription with a buffered channel of the given size.
// If bufSize is 0 an unbuffered channel is used: events are dropped when
// no receiver is ready (consistent with the non-blocking publish contract).
// If the broker is already closed the returned subscription's channel is
// immediately closed.
func (b *Broker[T]) Subscribe(bufSize int) *Subscription[T] {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan T, bufSize)
	s := &Subscription[T]{
		id:     b.nextID,
		ch:     ch,
		broker: b,
	}
	b.nextID++

	if b.closed {
		close(ch)
		return s
	}

	b.subs[s.id] = s
	return s
}

// Publish sends event to every active subscriber.
// The call is non-blocking: if a subscriber's buffer is full, the event is
// silently dropped for that subscriber.
func (b *Broker[T]) Publish(event T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}

	for _, s := range b.subs {
		select {
		case s.ch <- event:
		default:
			// Subscriber buffer full; drop to preserve non-blocking guarantee.
		}
	}
}

// Close shuts the broker down and closes every subscriber channel.
// Subsequent calls to Close are no-ops.
func (b *Broker[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for _, s := range b.subs {
		close(s.ch)
	}
	// Clear the map so Cancel calls on existing subscriptions are idempotent.
	b.subs = make(map[uint64]*Subscription[T])
}

// Ch returns the read-only channel on which events arrive.
func (s *Subscription[T]) Ch() <-chan T {
	return s.ch
}

// Cancel unsubscribes this subscription and closes its channel.
// Subsequent calls to Cancel are no-ops.
func (s *Subscription[T]) Cancel() {
	s.once.Do(func() {
		b := s.broker
		b.mu.Lock()
		defer b.mu.Unlock()

		// If the broker is already closed it already closed the channel;
		// skip double-close.
		if _, exists := b.subs[s.id]; exists {
			delete(b.subs, s.id)
			close(s.ch)
		}
	})
}
