// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package stats holds async statistics pushed by the subscriber and exposes
// snapshots and reference-point deltas for callers (TUI, NDR).
package stats

import (
	"encoding/json"
	"sync"

	"github.com/rjarry/trex-client/pkg/rpc"
)

// Store keeps the latest raw payload for each async stats channel, updated by
// the subscriber goroutine and read by consumers. It is safe for concurrent
// use. Payloads are kept raw here; typed decoding lives with each consumer so
// Phase 0 does not need the full stats model.
type Store struct {
	mu     sync.RWMutex
	latest map[string]json.RawMessage
	// ref holds the reference-point snapshot captured by Clear, used to
	// compute deltas relative to the last clear.
	ref map[string]json.RawMessage
	// gen counts payloads received, letting callers wait for fresh data.
	gen uint64
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{
		latest: make(map[string]json.RawMessage),
		ref:    make(map[string]json.RawMessage),
	}
}

// Dispatch implements rpc.Dispatcher: it records the latest payload for the
// event's channel name.
func (s *Store) Dispatch(ev rpc.AsyncEvent) {
	s.mu.Lock()
	s.latest[ev.Name] = ev.Data
	s.gen++
	s.mu.Unlock()
}

// Generation returns a counter that advances every time a payload is recorded.
// Callers can snapshot it and wait for it to change to detect fresh async data.
func (s *Store) Generation() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gen
}

// Latest returns the most recent raw payload for a channel and whether one has
// been seen.
func (s *Store) Latest(name string) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.latest[name]
	return v, ok
}

// Clear captures the current values as the reference point for later deltas.
// This mirrors the client-side "clear stats" that baselines counters without
// touching the server.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ref = make(map[string]json.RawMessage, len(s.latest))
	for k, v := range s.latest {
		s.ref[k] = v
	}
}

// Reference returns the reference-point payload captured by the last Clear.
func (s *Store) Reference(name string) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.ref[name]
	return v, ok
}
