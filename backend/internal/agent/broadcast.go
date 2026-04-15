package agent

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Event types for SSE broadcasting.
const (
	EventFlowStarted      = "flow_started"
	EventTaskCreated      = "task_created"
	EventSubtaskStarted   = "subtask_started"
	EventActionCompleted  = "action_completed"
	EventToolExecuted     = "tool_executed"
	EventSubtaskCompleted = "subtask_completed"
	EventTaskCompleted    = "task_completed"
	EventFlowCompleted    = "flow_completed"
	EventFlowFailed       = "flow_failed"
	EventFlowStopped      = "flow_stopped"

	// Finding events.
	EventFindingCreated = "finding_created"

	// Streaming events (additive — existing events remain for compatibility).
	EventAgentStreamDelta = "agent_stream_delta"
	EventAgentStreamDone  = "agent_stream_done"
)

// Event represents a real-time update for a flow execution.
type Event struct {
	FlowID    string    `json:"flow_id"`
	Type      string    `json:"type"`
	Data      any       `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

// subscriber is an SSE client listening for events on a specific flow.
type subscriber struct {
	ch     chan Event
	flowID string
}

// Broadcaster manages SSE event distribution via a hub pattern.
// Clients subscribe to receive events for a specific flow ID.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[string][]subscriber
}

// NewBroadcaster creates a new event broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{
		subscribers: make(map[string][]subscriber),
	}
}

// Subscribe creates a new event channel for the given flow ID.
// The caller must call Unsubscribe when done to prevent leaks.
func (b *Broadcaster) Subscribe(flowID string) chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, 64) // Buffered to avoid blocking the publisher
	sub := subscriber{ch: ch, flowID: flowID}
	b.subscribers[flowID] = append(b.subscribers[flowID], sub)

	log.WithField("flow_id", flowID).Debug("broadcaster: new subscriber")
	return ch
}

// Unsubscribe removes a channel from the subscriber list and closes it.
func (b *Broadcaster) Unsubscribe(flowID string, ch chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[flowID]
	for i, s := range subs {
		if s.ch == ch {
			close(s.ch)
			b.subscribers[flowID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}

	// Clean up empty flow entries.
	if len(b.subscribers[flowID]) == 0 {
		delete(b.subscribers, flowID)
	}
}

// Publish sends an event to all subscribers of the given flow.
func (b *Broadcaster) Publish(event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.mu.RLock()
	subs := b.subscribers[event.FlowID]
	b.mu.RUnlock()

	for _, s := range subs {
		select {
		case s.ch <- event:
		default:
			// Drop events for slow subscribers to avoid blocking.
			log.WithField("flow_id", event.FlowID).Warn("broadcaster: dropping event for slow subscriber")
		}
	}
}
