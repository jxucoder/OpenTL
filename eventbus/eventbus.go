// Package eventbus provides the Bus interface and an in-memory implementation
// for real-time event streaming in TeleCoder.
package eventbus

import (
	"sync"

	"github.com/jxucoder/TeleCoder/model"
)

// Bus provides pub/sub for session events.
type Bus interface {
	Subscribe(sessionID string) chan *model.Event
	Unsubscribe(sessionID string, ch chan *model.Event)
	Publish(sessionID string, event *model.Event)
}

// InMemoryBus is the default in-memory Bus implementation.
type InMemoryBus struct {
	mu   sync.RWMutex
	subs map[string][]chan *model.Event
}

// NewInMemoryBus creates a new InMemoryBus.
func NewInMemoryBus() *InMemoryBus {
	return &InMemoryBus{
		subs: make(map[string][]chan *model.Event),
	}
}

// Subscribe creates a channel that receives events for a session.
func (b *InMemoryBus) Subscribe(sessionID string) chan *model.Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan *model.Event, 64)
	b.subs[sessionID] = append(b.subs[sessionID], ch)
	return ch
}

// Unsubscribe removes a channel from the session's subscribers.
func (b *InMemoryBus) Unsubscribe(sessionID string, ch chan *model.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subs[sessionID]
	for i, s := range subs {
		if s == ch {
			b.subs[sessionID] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish sends an event to all subscribers for a session.
func (b *InMemoryBus) Publish(sessionID string, event *model.Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, ch := range b.subs[sessionID] {
		select {
		case ch <- event:
		default:
			// Drop event if subscriber is too slow.
		}
	}
}
