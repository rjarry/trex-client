// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-zeromq/zmq4"
)

// DefaultAsyncPort is the TRex asynchronous publisher port.
const DefaultAsyncPort = 4500

// AsyncEvent is one entry in a get_async_events reply. The async SUB channel is
// only a trigger; the real payloads are pulled over the REQ channel and arrive
// as a list of these.
type AsyncEvent struct {
	Name     string          `json:"name"`
	Type     int             `json:"type"`
	Seq      uint64          `json:"seq"`
	Baseline bool            `json:"baseline"`
	Data     json.RawMessage `json:"data"`
}

// Dispatcher consumes decoded async events (e.g. StatsStore + EventBus).
type Dispatcher interface {
	Dispatch(ev AsyncEvent)
}

// DispatchFunc adapts a function to the Dispatcher interface.
type DispatchFunc func(ev AsyncEvent)

func (f DispatchFunc) Dispatch(ev AsyncEvent) { f(ev) }

// Subscriber runs the async publisher channel. On each trigger it pulls the
// pending events over the shared REQ connection (respecting its lock) and
// dispatches them. It is a single goroutine, matching the server's model.
type Subscriber struct {
	conn      *Connection
	sock      zmq4.Socket
	sessionID uint32
	dispatch  Dispatcher
	onError   func(error)

	seq uint64
}

// NewSubscriber dials the publisher port and subscribes to all messages.
// onError may be nil; it is called for non-fatal errors (seq gaps, decode
// failures) so the loop can keep running.
func NewSubscriber(ctx context.Context, host string, port int, conn *Connection, sessionID uint32, dispatch Dispatcher, onError func(error)) (*Subscriber, error) {
	sock := zmq4.NewSub(ctx)
	endpoint := fmt.Sprintf("tcp://%s:%d", host, port)
	if err := sock.Dial(endpoint); err != nil {
		return nil, fmt.Errorf("dial %s: %w", endpoint, err)
	}
	if err := sock.SetOption(zmq4.OptionSubscribe, ""); err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	if onError == nil {
		onError = func(error) {}
	}
	return &Subscriber{
		sock:      sock,
		conn:      conn,
		sessionID: sessionID,
		dispatch:  dispatch,
		onError:   onError,
	}, nil
}

// Close releases the subscriber socket, unblocking a pending Recv.
func (s *Subscriber) Close() error {
	return s.sock.Close()
}

// Run blocks until ctx is cancelled or the socket is closed, servicing async
// triggers. Run it in its own goroutine.
func (s *Subscriber) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		// The message content is irrelevant: any publish is a trigger to
		// pull the real events over the REQ channel.
		if _, err := s.sock.Recv(); err != nil {
			if ctx.Err() != nil {
				return
			}
			s.onError(fmt.Errorf("subscriber recv: %w", err))
			return
		}
		s.pull()
	}
}

// pull fetches and dispatches the pending events after a trigger.
func (s *Subscriber) pull() {
	result, err := s.conn.Call("get_async_events", map[string]any{
		"session_id": s.sessionID,
		"user":       "sub",
	})
	if err != nil {
		s.onError(fmt.Errorf("get_async_events: %w", err))
		return
	}

	var out struct {
		Data []AsyncEvent `json:"data"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		s.onError(fmt.Errorf("decode async events: %w", err))
		return
	}

	for _, ev := range out.Data {
		next, ok := checkSeq(s.seq, ev.Seq)
		if !ok {
			s.onError(fmt.Errorf("async seq gap: expected %d, got %d (event %q)", s.seq, ev.Seq, ev.Name))
		}
		s.seq = next
		s.dispatch.Dispatch(ev)
	}
}

// checkSeq validates an incoming sequence number against the expected one and
// returns the next expected value. A seq of 0 on either side is a wildcard
// (server reset / first message), matching the reference client. ok is false
// when a real gap is detected; the caller still advances to resync.
func checkSeq(expected, got uint64) (next uint64, ok bool) {
	if expected != 0 && got != 0 && got != expected {
		return got + 1, false
	}
	return got + 1, true
}
