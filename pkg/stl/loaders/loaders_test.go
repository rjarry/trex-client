// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package loaders

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"

	"github.com/rjarry/trex-client/pkg/stl"
)

func samplePacket(t *testing.T) *stl.Packet {
	t.Helper()
	eth := &layers.Ethernet{
		SrcMAC:       net.HardwareAddr{0, 0, 0, 0, 0, 1},
		DstMAC:       net.HardwareAddr{0, 0, 0, 0, 0, 2},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{
		Version: 4, TTL: 64, Protocol: layers.IPProtocolUDP,
		SrcIP: net.IPv4(16, 0, 0, 1), DstIP: net.IPv4(48, 0, 0, 1),
	}
	udp := &layers.UDP{SrcPort: 1025, DstPort: 12}
	udp.SetNetworkLayerForChecksum(ip)
	pkt, err := stl.BuildPacket(eth, ip, udp, stl.Payload(18))
	if err != nil {
		t.Fatalf("BuildPacket: %v", err)
	}
	return pkt
}

func TestLoadSnapshot(t *testing.T) {
	pkt := samplePacket(t)
	snap := fmt.Sprintf(`[{
		"name": "s1", "enabled": true, "self_start": true, "isg": 0,
		"mode": {"rate": {"type": "percentage", "value": 100}, "type": "continuous"},
		"packet": {"binary": %q},
		"vm": {"instructions": [{"type": "fix_checksum_ipv4", "pkt_offset": 14}]},
		"flow_stats": {"enabled": true, "stream_id": 5, "rule_type": "stats"}
	}]`, pkt.Base64())

	prof, err := LoadSnapshot([]byte(snap))
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	res, err := prof.Resolve()
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(res[0].JSON, &s); err != nil {
		t.Fatal(err)
	}
	if s["self_start"] != true || s["packet"].(map[string]any)["binary"] != pkt.Base64() {
		t.Errorf("snapshot packet not passed through: %v", s["packet"])
	}
	// The raw VM instruction is preserved verbatim.
	ins := s["vm"].(map[string]any)["instructions"].([]any)[0].(map[string]any)
	if ins["type"] != "fix_checksum_ipv4" || ins["pkt_offset"] != float64(14) {
		t.Errorf("vm instruction not preserved: %v", ins)
	}
	fs := s["flow_stats"].(map[string]any)
	if fs["stream_id"] != float64(5) || fs["rule_type"] != "stats" {
		t.Errorf("flow_stats not preserved: %v", fs)
	}
}

func TestLoadPcap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.pcap")
	writePcap(t, path, 3)

	prof, err := LoadPcap(path, PcapOptions{})
	if err != nil {
		t.Fatalf("LoadPcap: %v", err)
	}
	if len(prof) != 3 {
		t.Fatalf("got %d streams, want 3", len(prof))
	}
	if !prof[0].SelfStart || prof[1].SelfStart {
		t.Error("only the first stream should self-start")
	}
	if prof[0].Next != "pkt_1" || prof[2].Next != "" {
		t.Errorf("chaining wrong: %q ... %q", prof[0].Next, prof[2].Next)
	}
	if _, err := prof.Resolve(); err != nil {
		t.Fatalf("Resolve pcap profile: %v", err)
	}
}

func writePcap(t *testing.T, path string, n int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := pcapgo.NewWriter(f)
	if err := w.WriteFileHeader(65536, layers.LinkTypeEthernet); err != nil {
		t.Fatal(err)
	}
	buf := gopacket.NewSerializeBuffer()
	eth := &layers.Ethernet{SrcMAC: net.HardwareAddr{0, 0, 0, 0, 0, 1}, DstMAC: net.HardwareAddr{0, 0, 0, 0, 0, 2}, EthernetType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{Version: 4, TTL: 64, Protocol: layers.IPProtocolUDP, SrcIP: net.IPv4(1, 1, 1, 1), DstIP: net.IPv4(2, 2, 2, 2)}
	udp := &layers.UDP{SrcPort: 1, DstPort: 2}
	udp.SetNetworkLayerForChecksum(ip)
	gopacket.SerializeLayers(buf, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, eth, ip, udp, stl.Payload(20))
	data := buf.Bytes()
	for i := 0; i < n; i++ {
		ci := gopacket.CaptureInfo{Timestamp: time.Unix(0, int64(i)*1000_000), CaptureLength: len(data), Length: len(data)}
		if err := w.WritePacket(ci, data); err != nil {
			t.Fatal(err)
		}
	}
}
