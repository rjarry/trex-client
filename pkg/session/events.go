// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package session implements the client-facing control layer: the connection
// lifecycle, port acquisition and traffic control on top of the rpc transport.
package session

import (
	"encoding/json"
	"sync"
)

// ServerEventID identifies an async server event carried on the trex-event
// channel. Values are in sync with the server.
type ServerEventID int

const (
	EventPortStarted  ServerEventID = 0
	EventPortStopped  ServerEventID = 1
	EventPortPaused   ServerEventID = 2
	EventPortResumed  ServerEventID = 3
	EventPortJobDone  ServerEventID = 4 // timed traffic finished (wait_on_traffic)
	EventPortAcquired ServerEventID = 5
	EventPortReleased ServerEventID = 6
	EventPortError    ServerEventID = 7
	EventPortAttrChg  ServerEventID = 8

	EventProfileStarted    ServerEventID = 10
	EventProfileStopped    ServerEventID = 11
	EventProfilePaused     ServerEventID = 12
	EventProfileResumed    ServerEventID = 13
	EventProfileFinishedTx ServerEventID = 14
	EventProfileError      ServerEventID = 17

	EventServerStopped ServerEventID = 100
)

// ServerEvent is a decoded trex-event message.
type ServerEvent struct {
	ID   ServerEventID
	Data json.RawMessage
}

// EventBus fans server events out to subscribers. A subscriber registers for a
// specific event id and receives matching events until it unsubscribes. It is
// safe for concurrent use.
type EventBus struct {
	mu   sync.Mutex
	next int
	subs map[ServerEventID]map[int]chan ServerEvent
}

// NewEventBus returns an empty bus.
func NewEventBus() *EventBus {
	return &EventBus{subs: make(map[ServerEventID]map[int]chan ServerEvent)}
}

// Subscribe returns a channel that receives events of the given id and a
// cancel function that unsubscribes and closes the channel. The channel is
// buffered so a slow consumer does not block the dispatcher.
func (b *EventBus) Subscribe(id ServerEventID) (<-chan ServerEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan ServerEvent, 8)
	if b.subs[id] == nil {
		b.subs[id] = make(map[int]chan ServerEvent)
	}
	key := b.next
	b.next++
	b.subs[id][key] = ch

	cancel := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if m := b.subs[id]; m != nil {
			if c, ok := m[key]; ok {
				delete(m, key)
				close(c)
			}
		}
	}
	return ch, cancel
}

// Publish delivers an event to all subscribers of its id. Delivery is
// non-blocking: if a subscriber's buffer is full the event is dropped for that
// subscriber (control events are edge-triggered and re-derivable from stats).
func (b *EventBus) Publish(ev ServerEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs[ev.ID] {
		select {
		case ch <- ev:
		default:
		}
	}
}
