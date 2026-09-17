// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package stl

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/gopacket/gopacket/layers"
)

// udpPacket builds a standard Ether/IPv4/UDP test packet.
func udpPacket(t *testing.T) *Packet {
	t.Helper()
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0, 0, 0, 1, 0, 0},
		DstMAC:       net.HardwareAddr{0, 0, 0, 2, 0, 0},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolUDP,
		SrcIP:    net.IPv4(16, 0, 0, 1),
		DstIP:    net.IPv4(48, 0, 0, 1),
	}
	udp := &layers.UDP{SrcPort: 1025, DstPort: 12}
	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatalf("checksum setup: %v", err)
	}
	pkt, err := BuildPacket(eth, ip, udp, Payload(18))
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	return pkt
}

func TestResolveOffset(t *testing.T) {
	pkt := udpPacket(t)
	cases := map[string]int{
		"Ether.dst": 0,
		"Ether.src": 6,
		"IP":        14,
		"IP.src":    26, // 14 + 12
		"IP.dst":    30, // 14 + 16
		"IP.ttl":    22, // 14 + 8
		"UDP":       34, // 14 + 20
		"UDP.sport": 34,
		"UDP.dport": 36,
	}
	for ref, want := range cases {
		got, err := pkt.ResolveOffset(ref)
		if err != nil {
			t.Errorf("ResolveOffset(%q): %v", ref, err)
			continue
		}
		if got != want {
			t.Errorf("ResolveOffset(%q) = %d, want %d", ref, got, want)
		}
	}
}

func TestResolveOffsetErrors(t *testing.T) {
	pkt := udpPacket(t)
	for _, ref := range []string{"TCP.sport", "IP.nope", "Bogus.x"} {
		if _, err := pkt.ResolveOffset(ref); err == nil {
			t.Errorf("ResolveOffset(%q): expected error", ref)
		}
	}
}

func TestParseRate(t *testing.T) {
	cases := []struct {
		in  string
		typ string
		val float64
	}{
		{"100%", RatePercentage, 100},
		{"50.5%", RatePercentage, 50.5},
		{"1mpps", RatePPS, 1e6},
		{"10kpps", RatePPS, 1e4},
		{"1gbps", RateBPSL1, 1e9},
		{"1mbpsl2", RateBPSL2, 1e6},
		{"1000", RatePPS, 1000},
	}
	for _, c := range cases {
		r, err := ParseRate(c.in)
		if err != nil {
			t.Errorf("ParseRate(%q): %v", c.in, err)
			continue
		}
		if r.Type != c.typ || r.Value != c.val {
			t.Errorf("ParseRate(%q) = %+v, want {%s %v}", c.in, r, c.typ, c.val)
		}
	}
}

func TestFlowStatsMarshal(t *testing.T) {
	off, _ := json.Marshal(FlowStats{})
	if string(off) != `{"enabled":false}` {
		t.Errorf("disabled flow stats = %s", off)
	}
	on, _ := json.Marshal(FlowStats{Enabled: true, PGID: 7, RuleType: FSLatency})
	var m map[string]any
	if err := json.Unmarshal(on, &m); err != nil {
		t.Fatal(err)
	}
	if m["enabled"] != true || m["stream_id"] != float64(7) || m["rule_type"] != "latency" {
		t.Errorf("latency flow stats = %s", on)
	}
}

func TestProfileResolve(t *testing.T) {
	p := Profile{
		{Name: "a", Next: "b", SelfStart: true, Enabled: true, Packet: udpPacket(t), Mode: Continuous(Rate{Type: RatePercentage, Value: 100})},
		{Name: "b", Enabled: true, Packet: udpPacket(t), Mode: SingleBurst(Rate{Type: RatePPS, Value: 1000}, 50)},
	}
	res, err := p.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d streams, want 2", len(res))
	}

	var s0 map[string]any
	if err := json.Unmarshal(res[0].JSON, &s0); err != nil {
		t.Fatal(err)
	}
	// stream "a" chains to "b" which was allocated id 1.
	if s0["next_stream_id"] != float64(1) {
		t.Errorf("next_stream_id = %v, want 1", s0["next_stream_id"])
	}
	if s0["self_start"] != true {
		t.Errorf("self_start = %v, want true", s0["self_start"])
	}
	mode := s0["mode"].(map[string]any)
	if mode["type"] != "continuous" {
		t.Errorf("mode.type = %v", mode["type"])
	}

	var s1 map[string]any
	json.Unmarshal(res[1].JSON, &s1)
	m1 := s1["mode"].(map[string]any)
	if m1["type"] != "single_burst" || m1["total_pkts"] != float64(50) {
		t.Errorf("burst mode = %v", m1)
	}
	if s1["next_stream_id"] != float64(-1) {
		t.Errorf("terminal next_stream_id = %v, want -1", s1["next_stream_id"])
	}
}

func TestProfileResolveDanglingNext(t *testing.T) {
	p := Profile{{Name: "a", Next: "ghost", Packet: udpPacket(t), Mode: Continuous(Rate{Type: RatePPS, Value: 1})}}
	if _, err := p.Resolve(); err == nil {
		t.Fatal("expected dangling next error")
	}
}
