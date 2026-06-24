/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package broadcaster

import (
	"context"
	"sync"
)

// Broadcaster is a generic pub/sub mechanism that broadcasts messages to all subscribers.
type Broadcaster[T any] struct {
	mu          sync.RWMutex
	subscribers map[chan T]struct{}
}

// New creates a new Broadcaster instance.
func New[T any]() *Broadcaster[T] {
	return &Broadcaster[T]{
		subscribers: make(map[chan T]struct{}),
	}
}

// Subscribe registers a new subscriber and returns a channel that receives broadcasts.
// The subscription is automatically cleaned up when the context is cancelled.
func (b *Broadcaster[T]) Subscribe(ctx context.Context) <-chan T {
	ch := make(chan T, 1)

	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		delete(b.subscribers, ch)
		close(ch)
		b.mu.Unlock()
	}()

	return ch
}

// Publish sends a message to all current subscribers.
// Non-blocking: slow consumers will miss messages.
func (b *Broadcaster[T]) Publish(_ context.Context, data T) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.subscribers {
		select {
		case ch <- data:
		default:
		}
	}
}

// SubscriberCount returns the current number of active subscribers.
func (b *Broadcaster[T]) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}
