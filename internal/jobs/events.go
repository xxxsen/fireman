package jobs

import (
	"encoding/json"
	"sync"
)

// Event is an SSE payload for job progress.
type Event struct {
	JobID           string `json:"job_id"`
	Status          string `json:"status"`
	Phase           string `json:"phase,omitempty"`
	ProgressCurrent int    `json:"progress_current"`
	ProgressTotal   int    `json:"progress_total"`
	ErrorCode       string `json:"error_code,omitempty"`
	ErrorMessage    string `json:"error_message,omitempty"`
	RunID           string `json:"run_id,omitempty"`
}

// EventHub broadcasts job events to SSE subscribers.
type EventHub struct {
	mu   sync.RWMutex
	subs map[string]map[chan Event]struct{}
}

func NewEventHub() *EventHub {
	return &EventHub{subs: make(map[string]map[chan Event]struct{})}
}

func (h *EventHub) Subscribe(jobID string) (<-chan Event, func()) {
	ch := make(chan Event, 8)
	h.mu.Lock()
	if h.subs[jobID] == nil {
		h.subs[jobID] = make(map[chan Event]struct{})
	}
	h.subs[jobID][ch] = struct{}{}
	h.mu.Unlock()
	unsub := func() {
		h.mu.Lock()
		delete(h.subs[jobID], ch)
		if len(h.subs[jobID]) == 0 {
			delete(h.subs, jobID)
		}
		h.mu.Unlock()
		close(ch)
	}
	return ch, unsub
}

func (h *EventHub) Publish(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[ev.JobID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// FormatSSE formats one Server-Sent Event frame.
func FormatSSE(ev Event) ([]byte, error) {
	b, err := json.Marshal(ev)
	if err != nil {
		return nil, err
	}
	return append(append([]byte("data: "), b...), '\n', '\n'), nil
}
