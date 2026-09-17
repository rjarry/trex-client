// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package stl

import (
	"encoding/json"
	"fmt"
)

// Dst-MAC override modes (bits 1-2 of the stream flags).
const (
	DstMACCfgFile = 0
	DstMACPkt     = 1
	DstMACArp     = 2
)

// Stream is a single stateless stream. Fields mirror the server's stream object;
// zero values give a sane default (disabled=false etc.), so callers set only
// what they need.
type Stream struct {
	Name   string
	Next   string
	Packet *Packet
	Mode   Mode
	VM     VM
	Stats  FlowStats

	Enabled     bool
	SelfStart   bool
	StartPaused bool
	ISG         float64
	CoreID      int
	ActionCount int
	RandomSeed  int

	MacSrcOverride bool
	MacDstMode     int
	Dummy          bool

	// StreamID, when non-nil, forces a specific server stream id instead of a
	// positional one.
	StreamID *int

	// Direction, when non-nil, restricts the stream to ports whose id parity
	// matches it (0 -> even ports, 1 -> odd), mirroring the reference client's
	// per-direction profiles. A nil direction loads the stream onto every port.
	// It is a client-side loading hint and is not sent to the server.
	Direction *int
}

// Profile is an ordered list of streams.
type Profile []*Stream

// ForPort returns the sub-profile to load onto the given port: every stream
// with no direction, plus those whose direction matches the port's parity
// (portID % 2).
func (p Profile) ForPort(portID int) Profile {
	dir := portID % 2
	out := make(Profile, 0, len(p))
	for _, s := range p {
		if s.Direction == nil || *s.Direction == dir {
			out = append(out, s)
		}
	}
	return out
}

type wirePacket struct {
	Binary string `json:"binary"`
	Meta   string `json:"meta"`
}

type wireStream struct {
	Flags        int        `json:"flags"`
	ActionCount  int        `json:"action_count"`
	Enabled      bool       `json:"enabled"`
	SelfStart    bool       `json:"self_start"`
	StartPaused  bool       `json:"start_paused"`
	ISG          float64    `json:"isg"`
	CoreID       int        `json:"core_id"`
	RandomSeed   *int       `json:"random_seed,omitempty"`
	Mode         Mode       `json:"mode"`
	Packet       wirePacket `json:"packet"`
	VM           VM         `json:"vm"`
	FlowStats    FlowStats  `json:"flow_stats"`
	Name         string     `json:"name,omitempty"`
	Next         string     `json:"next,omitempty"`
	NextStreamID int        `json:"next_stream_id"`
}

// ResolvedStream is a stream ready for the add_stream RPC: its allocated id and
// the marshaled stream object (including the resolved next_stream_id).
type ResolvedStream struct {
	ID   int
	JSON json.RawMessage
}

// EnsureFlowStats assigns per-stream flow-statistics rules to any non-dummy
// stream that lacks them, allocating consecutive pg ids from startPGID. NDR
// needs every test stream tagged so exact per-pgid tx/rx counts are available.
// It returns the next free pg id.
func (p Profile) EnsureFlowStats(startPGID int) int {
	pg := startPGID
	for _, s := range p {
		if s.Dummy || s.Stats.Enabled {
			continue
		}
		s.Stats = FlowStats{Enabled: true, PGID: pg, RuleType: FSStats}
		pg++
	}
	return pg
}

// flags packs the mac-override and dummy bits the same way the server expects.
func (s *Stream) flags() int {
	f := 0
	if s.MacSrcOverride {
		f |= 1
	}
	f |= (s.MacDstMode & 3) << 1
	if s.Dummy {
		f |= 1 << 3
	}
	return f
}

// Resolve assigns stream ids, resolves next-stream references by name and
// marshals each stream to its wire object. Stream ids are positional (0..N-1)
// unless a stream sets StreamID. It errors on duplicate names/ids or a dangling
// next reference, matching the reference client.
func (p Profile) Resolve() ([]ResolvedStream, error) {
	// Allocate ids and build the name -> id lookup.
	ids := make([]int, len(p))
	lookup := make(map[string]int, len(p))
	used := make(map[int]bool, len(p))
	nextAuto := 0
	for i, s := range p {
		id := nextAuto
		if s.StreamID != nil {
			id = *s.StreamID
		}
		for used[id] {
			nextAuto++
			id = nextAuto
		}
		used[id] = true
		nextAuto = id + 1
		ids[i] = id
		if s.Name != "" {
			if _, dup := lookup[s.Name]; dup {
				return nil, fmt.Errorf("duplicate stream name %q", s.Name)
			}
			lookup[s.Name] = id
		}
	}

	out := make([]ResolvedStream, len(p))
	for i, s := range p {
		if s.Packet == nil {
			return nil, fmt.Errorf("stream %d (%q) has no packet", i, s.Name)
		}
		nextID := -1
		if s.Next != "" {
			id, ok := lookup[s.Next]
			if !ok {
				return nil, fmt.Errorf("stream %q references unknown next %q", s.Name, s.Next)
			}
			nextID = id
		}

		ws := wireStream{
			Flags:        s.flags(),
			ActionCount:  s.ActionCount,
			Enabled:      s.Enabled,
			SelfStart:    s.SelfStart,
			StartPaused:  s.StartPaused,
			ISG:          s.ISG,
			CoreID:       s.CoreID,
			Mode:         s.Mode,
			Packet:       wirePacket{Binary: s.Packet.Base64(), Meta: ""},
			VM:           s.VM,
			FlowStats:    s.Stats,
			Name:         s.Name,
			Next:         s.Next,
			NextStreamID: nextID,
		}
		if s.VM.Instructions == nil {
			ws.VM.Instructions = []VMInstruction{}
		}
		if s.RandomSeed != 0 {
			ws.RandomSeed = &s.RandomSeed
		}

		raw, err := json.Marshal(ws)
		if err != nil {
			return nil, fmt.Errorf("marshal stream %q: %w", s.Name, err)
		}
		out[i] = ResolvedStream{ID: ids[i], JSON: raw}
	}
	return out, nil
}
