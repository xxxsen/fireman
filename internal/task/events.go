package task

import (
	"encoding/json"
	"sync"
)

type EventHub struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

func NewEventHub() *EventHub { return &EventHub{subs: map[string]map[chan Event]struct{}{}} }

func (h *EventHub) Subscribe(taskID string) (<-chan Event, func()) {
	ch := make(chan Event, 8)
	h.mu.Lock()
	if h.subs[taskID] == nil {
		h.subs[taskID] = map[chan Event]struct{}{}
	}
	h.subs[taskID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[taskID], ch)
		if len(h.subs[taskID]) == 0 {
			delete(h.subs, taskID)
		}
		close(ch)
		h.mu.Unlock()
	}
}

func (h *EventHub) Publish(event Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[event.TaskID] {
		select {
		case ch <- event:
		default:
		}
	}
}

func FormatSSE(event Event) ([]byte, error) {
	b, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return append(append([]byte("data: "), b...), '\n', '\n'), nil
}
