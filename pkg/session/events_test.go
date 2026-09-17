// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package session

import (
	"testing"
	"time"
)

func TestEventBusDelivery(t *testing.T) {
	bus := NewEventBus()
	ch, cancel := bus.Subscribe(EventPortJobDone)
	defer cancel()

	bus.Publish(ServerEvent{ID: EventPortStarted}) // wrong id, ignored
	bus.Publish(ServerEvent{ID: EventPortJobDone, Data: []byte(`{"port_id":2}`)})

	select {
	case ev := <-ch:
		if eventPortID(ev.Data) != 2 {
			t.Errorf("port id = %d, want 2", eventPortID(ev.Data))
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive job-done event")
	}
}

func TestEventBusUnsubscribe(t *testing.T) {
	bus := NewEventBus()
	ch, cancel := bus.Subscribe(EventPortStopped)
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after cancel")
	}
	// Publishing after unsubscribe must not panic.
	bus.Publish(ServerEvent{ID: EventPortStopped})
}

func TestEventPortID(t *testing.T) {
	if got := eventPortID([]byte(`{"port_id":5}`)); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
	if got := eventPortID([]byte(`{}`)); got != -1 {
		t.Errorf("missing port_id got %d, want -1", got)
	}
}
