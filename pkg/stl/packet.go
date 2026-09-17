// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package stl

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

// Packet is a built L2 packet plus the layer offsets needed to resolve
// symbolic field engine targets like "IP.src" to numeric byte offsets. It
// replaces the Scapy introspection the Python client relies on: because we
// serialize the packet ourselves, we can decode it once and record where each
// layer begins.
type Packet struct {
	Binary      []byte
	layerStarts map[string]int // canonical layer name -> byte offset of its header
}

// Base64 returns the packet's L2 bytes base64-encoded for the wire.
func (p *Packet) Base64() string {
	return base64.StdEncoding.EncodeToString(p.Binary)
}

// Payload returns a zero-filled payload layer of the given size, used to pad a
// packet to a target length (the DSL's payload_size).
func Payload(size int) gopacket.Payload {
	return gopacket.Payload(make([]byte, size))
}

// BuildPacket serializes the given layers into a Packet, fixing lengths and
// computing checksums, then records each layer's start offset for later field
// resolution.
func BuildPacket(ls ...gopacket.SerializableLayer) (*Packet, error) {
	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := gopacket.SerializeLayers(buf, opts, ls...); err != nil {
		return nil, fmt.Errorf("serialize packet: %w", err)
	}
	bin := buf.Bytes()

	starts, err := decodeLayerStarts(bin)
	if err != nil {
		return nil, err
	}
	return &Packet{Binary: bin, layerStarts: starts}, nil
}

// RawPacket wraps pre-built bytes (e.g. from a pcap) and decodes their layer
// offsets so a VM can still target fields by name.
func RawPacket(bin []byte) (*Packet, error) {
	starts, err := decodeLayerStarts(bin)
	if err != nil {
		return nil, err
	}
	return &Packet{Binary: bin, layerStarts: starts}, nil
}

// decodeLayerStarts walks a serialized Ethernet frame and records the byte
// offset at which each recognized layer begins, keyed by canonical name.
func decodeLayerStarts(bin []byte) (map[string]int, error) {
	pkt := gopacket.NewPacket(bin, layers.LayerTypeEthernet, gopacket.Default)
	if err := pkt.ErrorLayer(); err != nil {
		return nil, fmt.Errorf("decode packet: %w", err.Error())
	}
	starts := make(map[string]int)
	offset := 0
	for _, l := range pkt.Layers() {
		if name, ok := canonicalLayerName(l.LayerType()); ok {
			// Keep the first occurrence for a given name (outermost header).
			if _, seen := starts[name]; !seen {
				starts[name] = offset
			}
		}
		offset += len(l.LayerContents())
	}
	return starts, nil
}

// ResolveOffset turns a symbolic field reference ("IP.src", "UDP.sport") or a
// bare layer name ("IP") into a numeric byte offset into the packet. A bare
// layer name resolves to the layer's start offset. Numeric strings are returned
// as-is by the caller; this function only handles symbolic names.
func (p *Packet) ResolveOffset(ref string) (int, error) {
	layerName, field, hasField := strings.Cut(ref, ".")
	canon, ok := aliasLayerName(layerName)
	if !ok {
		return 0, fmt.Errorf("unknown layer %q in field reference %q", layerName, ref)
	}
	start, ok := p.layerStarts[canon]
	if !ok {
		return 0, fmt.Errorf("layer %q not present in packet", canon)
	}
	if !hasField {
		return start, nil
	}
	fields, ok := fieldOffsets[canon]
	if !ok {
		return 0, fmt.Errorf("no field table for layer %q", canon)
	}
	foffset, ok := fields[field]
	if !ok {
		return 0, fmt.Errorf("unknown field %q for layer %q", field, canon)
	}
	return start + foffset, nil
}

// canonicalLayerName maps a gopacket layer type to our canonical name.
func canonicalLayerName(t gopacket.LayerType) (string, bool) {
	switch t {
	case layers.LayerTypeEthernet:
		return "Ether", true
	case layers.LayerTypeDot1Q:
		return "Dot1Q", true
	case layers.LayerTypeIPv4:
		return "IP", true
	case layers.LayerTypeIPv6:
		return "IPv6", true
	case layers.LayerTypeUDP:
		return "UDP", true
	case layers.LayerTypeTCP:
		return "TCP", true
	default:
		return "", false
	}
}

// aliasLayerName maps user-facing layer names (including Scapy aliases) to our
// canonical names.
func aliasLayerName(name string) (string, bool) {
	switch name {
	case "Ether", "Eth", "eth", "ether":
		return "Ether", true
	case "Dot1Q", "VLAN", "vlan":
		return "Dot1Q", true
	case "IP", "IPv4", "ip", "ipv4":
		return "IP", true
	case "IPv6", "ipv6":
		return "IPv6", true
	case "UDP", "udp":
		return "UDP", true
	case "TCP", "tcp":
		return "TCP", true
	default:
		return "", false
	}
}

// fieldOffsets gives the byte offset of each addressable field within a layer's
// header, matching Scapy's field names used by existing profiles.
var fieldOffsets = map[string]map[string]int{
	"Ether": {"dst": 0, "src": 6, "type": 12},
	"Dot1Q": {"vlan": 0, "type": 2},
	"IP": {
		"tos": 1, "len": 2, "id": 4, "flags": 6, "frag": 6,
		"ttl": 8, "proto": 9, "chksum": 10, "src": 12, "dst": 16,
	},
	"IPv6": {"src": 8, "dst": 24},
	"UDP":  {"sport": 0, "dport": 2, "len": 4, "chksum": 6},
	"TCP": {
		"sport": 0, "dport": 2, "seq": 4, "ack": 8,
		"flags": 13, "window": 14, "chksum": 16, "urgptr": 18,
	},
}
