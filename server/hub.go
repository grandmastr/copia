package main

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Hub fans out Server-Sent Events to every connected /events stream.
type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func NewHub() *Hub { return &Hub{subs: make(map[chan []byte]struct{})} }

func (h *Hub) Subscribe() chan []byte {
	ch := make(chan []byte, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) Unsubscribe(ch chan []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}

// Publish sends an event to all subscribers. A subscriber whose buffer is full
// is disconnected; it reconnects and resyncs from the "hello" event.
func (h *Hub) Publish(event string, v any) {
	data, err := json.Marshal(v) // JSON never contains raw newlines, so one data: line is safe
	if err != nil {
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- msg:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}
