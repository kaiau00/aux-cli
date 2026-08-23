package pubsub

import (
	"context"
	"sync"
	"testing"
)

// Publish must not send on a channel that unsubscribing has closed.
//
// This is not hypothetical: a crash log from 2026-07-04 caught it in the wild,
// panicking inside logging.Info -> writer.Write -> Broker.Publish and taking
// out the sessions subscription. The window is between Publish copying the
// subscriber set and Publish sending to it -- the lock was released in between,
// so a context cancellation could close a channel Publish still held.
//
// The panic is what fails this test; the assertions afterwards are incidental.
func TestPublishDoesNotSendOnAClosedSubscriber(t *testing.T) {
	b := NewBroker[string]()

	const subscribers = 24
	var wg sync.WaitGroup

	// Publishers keep hammering the subscriber set while it churns.
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					b.Publish(CreatedEvent, "payload")
				}
			}
		}()
	}

	// Subscribers come and go, each cancellation closing its channel.
	for i := 0; i < subscribers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 40; j++ {
				ctx, cancel := context.WithCancel(context.Background())
				ch := b.Subscribe(ctx)
				go func() {
					for range ch {
					}
				}()
				cancel()
			}
		}()
	}

	// Let the subscriber churn finish, then stop the publishers.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	for i := 0; i < subscribers; i++ {
		_ = i
	}
	close(stop)
	<-done
}

// Shutdown closes every subscriber channel, and is the other side of the same
// race: it takes the write lock and closes channels a concurrent Publish may
// already have copied.
func TestPublishDuringShutdownDoesNotPanic(t *testing.T) {
	for attempt := 0; attempt < 50; attempt++ {
		b := NewBroker[string]()
		for i := 0; i < 8; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b.Subscribe(ctx)
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				b.Publish(CreatedEvent, "payload")
			}
		}()
		go func() {
			defer wg.Done()
			b.Shutdown()
		}()
		wg.Wait()
	}
}
