// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package session

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rjarry/trex-client/pkg/rpc"
	"github.com/rjarry/trex-client/pkg/stl"
)

// DefaultProfileID is the server's default per-port profile id.
const DefaultProfileID = "_"

// maskAll selects all DP cores for a port (server MASK_ALL).
const maskAll uint64 = (1 << 64) - 1

// Mult is a start/update traffic multiplier. Type is one of raw, bps, pps or
// percentage.
type Mult struct {
	Type  string  `json:"type"`
	Value float64 `json:"value"`
}

// Mult type constants.
const (
	MultRaw        = "raw"
	MultBPS        = "bps"
	MultPPS        = "pps"
	MultPercentage = "percentage"
)

// Port controls a single server port: acquisition, stream loading and traffic.
type Port struct {
	client  *Client
	id      int
	handler json.RawMessage
}

// Port returns a handle for the given port id on this client.
func (c *Client) Port(id int) *Port {
	return &Port{client: c, id: id}
}

func (p *Port) conn() *rpc.Connection { return p.client.conn }

// ID returns the port id.
func (p *Port) ID() int { return p.id }

// Acquire takes ownership of the port and stores the returned handler used on
// all later mutating calls.
func (p *Port) Acquire(force bool) error {
	res, err := p.conn().Call("acquire", map[string]any{
		"port_id":    p.id,
		"user":       p.client.username,
		"session_id": p.client.sessionID,
		"force":      force,
	})
	if err != nil {
		return fmt.Errorf("acquire port %d: %w", p.id, err)
	}
	p.handler = res
	return nil
}

// Release relinquishes the port.
func (p *Port) Release() error {
	_, err := p.conn().Call("release", map[string]any{
		"port_id": p.id,
		"handler": p.handler,
	})
	if err != nil {
		return fmt.Errorf("release port %d: %w", p.id, err)
	}
	p.handler = nil
	return nil
}

// RemoveAllStreams clears the port's default profile.
func (p *Port) RemoveAllStreams() error {
	_, err := p.conn().Call("remove_all_streams", map[string]any{
		"handler":    p.handler,
		"port_id":    p.id,
		"profile_id": DefaultProfileID,
	})
	return err
}

// AddStreams uploads a resolved profile as a single batched add_stream call.
func (p *Port) AddStreams(streams []stl.ResolvedStream) error {
	batch := make([]rpc.BatchCall, len(streams))
	for i, s := range streams {
		batch[i] = rpc.BatchCall{
			Method: "add_stream",
			Params: map[string]any{
				"handler":    p.handler,
				"port_id":    p.id,
				"profile_id": DefaultProfileID,
				"stream_id":  s.ID,
				"stream":     s.JSON,
			},
		}
	}
	resps, err := p.conn().CallBatch(batch)
	if err != nil {
		return fmt.Errorf("add_streams port %d: %w", p.id, err)
	}
	for i, r := range resps {
		if r.Error != nil {
			return fmt.Errorf("add_stream %d (id %d): %w", i, streams[i].ID, r.Error)
		}
	}
	return nil
}

// Start begins transmission. duration <= 0 runs until stopped; a positive
// duration makes the server stop itself after that many seconds and emit the
// job-done event. startAtTs synchronizes multi-port starts (0 = immediate).
func (p *Port) Start(mul Mult, duration float64, force bool, startAtTs float64) error {
	_, err := p.conn().Call("start_traffic", map[string]any{
		"handler":     p.handler,
		"port_id":     p.id,
		"profile_id":  DefaultProfileID,
		"mul":         mul,
		"duration":    duration,
		"force":       force,
		"core_mask":   maskAll,
		"start_at_ts": startAtTs,
	})
	if err != nil {
		return fmt.Errorf("start_traffic port %d: %w", p.id, err)
	}
	return nil
}

// Stop halts transmission.
func (p *Port) Stop() error {
	_, err := p.conn().Call("stop_traffic", map[string]any{
		"handler":    p.handler,
		"port_id":    p.id,
		"profile_id": DefaultProfileID,
	})
	if err != nil {
		return fmt.Errorf("stop_traffic port %d: %w", p.id, err)
	}
	return nil
}

// Pause suspends transmission.
func (p *Port) Pause() error {
	_, err := p.conn().Call("pause_traffic", map[string]any{
		"handler":    p.handler,
		"port_id":    p.id,
		"profile_id": DefaultProfileID,
	})
	return err
}

// Resume continues a paused transmission.
func (p *Port) Resume() error {
	_, err := p.conn().Call("resume_traffic", map[string]any{
		"handler":    p.handler,
		"port_id":    p.id,
		"profile_id": DefaultProfileID,
	})
	return err
}

// Update changes the transmit multiplier of a running port. NDR uses this for
// rate ramp-up.
func (p *Port) Update(mul Mult, force bool) error {
	_, err := p.conn().Call("update_traffic", map[string]any{
		"handler":    p.handler,
		"port_id":    p.id,
		"profile_id": DefaultProfileID,
		"mul":        mul,
		"force":      force,
	})
	if err != nil {
		return fmt.Errorf("update_traffic port %d: %w", p.id, err)
	}
	return nil
}

// WaitJobDone returns a channel that receives once this port's timed traffic
// finishes (the EVENT_PORT_JOB_DONE server event), plus a cancel function.
// Subscribe before calling Start so the event cannot be missed.
func (p *Port) WaitJobDone() (<-chan struct{}, func()) {
	events, unsub := p.client.events.Subscribe(EventPortJobDone)
	done := make(chan struct{}, 1)
	go func() {
		for ev := range events {
			if eventPortID(ev.Data) == p.id {
				done <- struct{}{}
				return
			}
		}
	}()
	return done, unsub
}

// WaitOnTraffic blocks until this port's timed traffic finishes or ctx is done.
func (p *Port) WaitOnTraffic(ctx context.Context) error {
	done, cancel := p.WaitJobDone()
	defer cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// eventPortID extracts a port_id from an event payload, or -1 if absent.
func eventPortID(data json.RawMessage) int {
	var d struct {
		PortID *int `json:"port_id"`
	}
	if err := json.Unmarshal(data, &d); err != nil || d.PortID == nil {
		return -1
	}
	return *d.PortID
}
