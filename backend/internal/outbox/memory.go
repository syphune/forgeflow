package outbox

import (
	"context"
	"sync"
)

type MemoryWriter struct {
	mu     sync.Mutex
	events []Event
	keys   map[string]struct{}
}

func NewMemoryWriter() *MemoryWriter {
	return &MemoryWriter{keys: make(map[string]struct{})}
}

func (w *MemoryWriter) Append(_ context.Context, event Event) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.keys[event.IdempotencyKey]; exists {
		return nil
	}
	w.keys[event.IdempotencyKey] = struct{}{}
	w.events = append(w.events, event)
	return nil
}

func (w *MemoryWriter) Events() []Event {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Event(nil), w.events...)
}
