// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package loaders

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/rjarry/trex-client/pkg/stl"
)

// snapshotStream is the frozen stream object as produced by the GUI/RPC capture.
type snapshotStream struct {
	Name      string          `json:"name"`
	Next      string          `json:"next"`
	Enabled   bool            `json:"enabled"`
	SelfStart bool            `json:"self_start"`
	ISG       float64         `json:"isg"`
	Flags     int             `json:"flags"`
	CoreID    int             `json:"core_id"`
	Mode      snapshotMode    `json:"mode"`
	Packet    snapshotPacket  `json:"packet"`
	VM        snapshotVM      `json:"vm"`
	FlowStats snapshotFlowSts `json:"flow_stats"`
}

type snapshotMode struct {
	Rate         stl.Rate `json:"rate"`
	Type         string   `json:"type"`
	TotalPkts    int      `json:"total_pkts"`
	PktsPerBurst int      `json:"pkts_per_burst"`
	IBG          float64  `json:"ibg"`
	Count        int      `json:"count"`
}

type snapshotPacket struct {
	Binary string `json:"binary"`
}

type snapshotVM struct {
	Instructions []json.RawMessage `json:"instructions"`
	Cache        *int              `json:"cache"`
}

type snapshotFlowSts struct {
	Enabled  bool   `json:"enabled"`
	StreamID int    `json:"stream_id"`
	RuleType string `json:"rule_type"`
	VXLAN    bool   `json:"vxlan"`
	IEEE1588 bool   `json:"ieee_1588"`
}

// LoadSnapshot parses the frozen JSON/YAML stream list (GUI export or RPC
// capture) into a profile, passing the packet bytes and field-engine
// instructions through verbatim.
func LoadSnapshot(raw []byte) (stl.Profile, error) {
	// Accept both JSON and YAML by converting YAML to JSON-compatible values.
	var top []json.RawMessage
	if err := yamlToJSONList(raw, &top); err != nil {
		return nil, err
	}

	profile := make(stl.Profile, 0, len(top))
	for i, item := range top {
		// Peek to detect the GUI {name, stream:{...}} wrapper.
		var probe struct {
			Name   string           `json:"name"`
			Next   string           `json:"next"`
			Stream *json.RawMessage `json:"stream"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			return nil, fmt.Errorf("stream %d: %w", i, err)
		}

		body := item
		if probe.Stream != nil {
			body = *probe.Stream
		}
		var s snapshotStream
		if err := json.Unmarshal(body, &s); err != nil {
			return nil, fmt.Errorf("stream %d: %w", i, err)
		}
		// The GUI wrapper carries name/next outside the inner object.
		if s.Name == "" {
			s.Name = probe.Name
		}
		if s.Next == "" {
			s.Next = probe.Next
		}

		stream, err := s.toStream()
		if err != nil {
			return nil, fmt.Errorf("stream %d (%q): %w", i, s.Name, err)
		}
		profile = append(profile, stream)
	}
	return profile, nil
}

func (s snapshotStream) toStream() (*stl.Stream, error) {
	bin, err := base64.StdEncoding.DecodeString(s.Packet.Binary)
	if err != nil {
		return nil, fmt.Errorf("packet binary: %w", err)
	}
	pkt, err := stl.RawPacket(bin)
	if err != nil {
		return nil, err
	}

	vm := stl.VM{Instructions: make([]stl.VMInstruction, len(s.VM.Instructions)), Cache: s.VM.Cache}
	for i, raw := range s.VM.Instructions {
		vm.Instructions[i] = stl.RawInstruction(raw)
	}

	stream := &stl.Stream{
		Name:      s.Name,
		Next:      s.Next,
		Packet:    pkt,
		Mode:      s.Mode.toMode(),
		VM:        vm,
		Enabled:   s.Enabled,
		SelfStart: s.SelfStart,
		ISG:       s.ISG,
		CoreID:    s.CoreID,
	}
	if s.FlowStats.Enabled {
		stream.Stats = stl.FlowStats{
			Enabled:  true,
			PGID:     s.FlowStats.StreamID,
			RuleType: s.FlowStats.RuleType,
			VXLAN:    s.FlowStats.VXLAN,
			IEEE1588: s.FlowStats.IEEE1588,
		}
	}
	return stream, nil
}

func (m snapshotMode) toMode() stl.Mode {
	switch m.Type {
	case stl.ModeSingleBurst:
		return stl.SingleBurst(m.Rate, m.TotalPkts)
	case stl.ModeMultiBurst:
		return stl.MultiBurst(m.Rate, m.PktsPerBurst, m.IBG, m.Count)
	default:
		return stl.Continuous(m.Rate)
	}
}

// yamlToJSONList decodes a YAML or JSON list into a slice of raw JSON messages
// so downstream code can use json tags uniformly.
func yamlToJSONList(raw []byte, out *[]json.RawMessage) error {
	var generic []any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return fmt.Errorf("parse snapshot: %w", err)
	}
	for _, item := range generic {
		b, err := json.Marshal(normalizeYAML(item))
		if err != nil {
			return err
		}
		*out = append(*out, b)
	}
	return nil
}

// normalizeYAML converts yaml's map[any]any into map[string]any so it can be
// JSON-marshaled.
func normalizeYAML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[k] = normalizeYAML(val)
		}
		return m
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[fmt.Sprint(k)] = normalizeYAML(val)
		}
		return m
	case []any:
		s := make([]any, len(x))
		for i, val := range x {
			s[i] = normalizeYAML(val)
		}
		return s
	default:
		return v
	}
}
