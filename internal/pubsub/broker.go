package pubsub

import (
	"context"
	"sync"
)

const bufferSize = 64

type Broker[T any] struct {
	subs      map[chan Event[T]]struct{}
	mu        sync.RWMutex
	done      chan struct{}
	subCount  int
	maxEvents int
}

func NewBroker[T any]() *Broker[T] {
	return NewBrokerWithOptions[T](bufferSize, 1000)
}

func NewBrokerWithOptions[T any](channelBufferSize, maxEvents int) *Broker[T] {
	b := &Broker[T]{
		subs:      make(map[chan Event[T]]struct{}),
		done:      make(chan struct{}),
		subCount:  0,
		maxEvents: maxEvents,
	}
	return b
}

func (b *Broker[T]) Shutdown() {
	select {
	case <-b.done: // Already closed
		return
	default:
		close(b.done)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subs {
		delete(b.subs, ch)
		close(ch)
	}

	b.subCount = 0
}

func (b *Broker[T]) Subscribe(ctx context.Context) <-chan Event[T] {
	b.mu.Lock()
	defer b.mu.Unlock()

	select {
	case <-b.done:
		ch := make(chan Event[T])
		close(ch)
		return ch
	default:
	}

	sub := make(chan Event[T], bufferSize)
	b.subs[sub] = struct{}{}
	b.subCount++

	go func() {
		<-ctx.Done()

		b.mu.Lock()
		defer b.mu.Unlock()

		select {
		case <-b.done:
			return
		default:
		}

		delete(b.subs, sub)
		close(sub)
		b.subCount--
	}()

	return sub
}

func (b *Broker[T]) GetSubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.subCount
}

// Publish delivers an event to every current subscriber, dropping it for any
// subscriber whose buffer is full.
//
// The read lock is held across the sends, not merely across a copy of the
// subscriber set. Closing a subscriber channel -- from Shutdown, or from the
// goroutine watching that subscriber's context -- takes the write lock, so
// holding the read lock here is precisely what makes it impossible to send on a
// channel that has already been closed. Releasing the lock after copying left
// that window open, and it was not theoretical: it panicked in the wild on
// 2026-07-04, inside a log write, taking the sessions subscription down with it.
//
// Holding a read lock across a send is only safe because these sends cannot
// block -- the default branch drops the event when a buffer is full. Do not add
// a blocking send here.
func (b *Broker[T]) Publish(t EventType, payload T) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	select {
	case <-b.done:
		return
	default:
	}

	event := Event[T]{Type: t, Payload: payload}
	for sub := range b.subs {
		select {
		case sub <- event:
		default:
		}
	}
}
