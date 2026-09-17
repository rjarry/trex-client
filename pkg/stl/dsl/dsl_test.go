// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package dsl

import (
	"encoding/json"
	"testing"
)

const profileYAML = `
size: 64
---
- name: s1
  packet:
    layers:
      - eth: {}
      - ip: {src: 16.0.0.1, dst: 48.0.0.1}
      - udp: {sport: 1025, dport: 12}
    payload_size: {{.size}}
  mode: {type: continuous, rate: {type: percentage, value: 100}}
  vm:
    - flow_var: {name: ip_src, op: inc, min: 16.0.0.1, max: 16.0.0.255, size: 4}
    - write_flow_var: {name: ip_src, offset: IP.src}
    - fix_checksum_ipv4: {offset: IP}
  flow_stats: {rule_type: latency, pg_id: 7}
`

func TestDSLParse(t *testing.T) {
	prof, err := Parse([]byte(profileYAML), map[string]string{"size": "128"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(prof) != 1 {
		t.Fatalf("got %d streams, want 1", len(prof))
	}

	res, err := prof.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(res[0].JSON, &s); err != nil {
		t.Fatal(err)
	}

	// tunable override: payload 128 -> total frame 14+20+8+128 = 170.
	fs := s["flow_stats"].(map[string]any)
	if fs["rule_type"] != "latency" || fs["stream_id"] != float64(7) {
		t.Errorf("flow_stats = %v", fs)
	}

	vm := s["vm"].(map[string]any)
	instrs := vm["instructions"].([]any)
	if len(instrs) != 3 {
		t.Fatalf("got %d vm instructions, want 3", len(instrs))
	}
	write := instrs[1].(map[string]any)
	if write["type"] != "write_flow_var" || write["pkt_offset"] != float64(26) {
		t.Errorf("write_flow_var offset = %v, want 26", write["pkt_offset"])
	}
	fix := instrs[2].(map[string]any)
	if fix["type"] != "fix_checksum_ipv4" || fix["pkt_offset"] != float64(14) {
		t.Errorf("fix_checksum offset = %v, want 14", fix["pkt_offset"])
	}
	fv := instrs[0].(map[string]any)
	// 16.0.0.1 -> 0x10000001 = 268435457
	if fv["min_value"] != float64(268435457) {
		t.Errorf("flow_var min_value = %v, want 268435457", fv["min_value"])
	}
}

const imixYAML = `
- imix:
    - {size: 64, weight: 7}
    - {size: 594, weight: 4}
    - {size: 1518, weight: 1}
  packet:
    layers:
      - eth: {}
      - ip: {}
      - udp: {}
  mode: {rate: {type: percentage, value: 100}}
`

func TestDSLImix(t *testing.T) {
	prof, err := Parse([]byte(imixYAML), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(prof) != 3 {
		t.Fatalf("imix expanded to %d streams, want 3", len(prof))
	}
	// Rates split by weight (7:4:1 of 100%).
	got := prof[0].Mode.Rate.Value
	if want := 100.0 * 7 / 12; got < want-0.001 || got > want+0.001 {
		t.Errorf("first imix rate = %v, want %v", got, want)
	}
}

func TestDSLMissingTunable(t *testing.T) {
	// {{.size}} with no default and no override must error, not silently blank.
	if _, err := Parse([]byte("- packet:\n    layers: [{eth: {}}]\n    payload_size: {{.size}}\n"), nil); err == nil {
		t.Fatal("expected missing tunable error")
	}
}
