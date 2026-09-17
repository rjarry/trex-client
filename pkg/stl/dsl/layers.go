// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package dsl

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

// layerEntry is one item in a packet's "layers" list: a single-key mapping from
// a layer name to its fields.
type layerEntry map[string]map[string]any

// buildLayers turns the DSL layer list into ordered gopacket layers, filling in
// linkage fields (ethertype, IP protocol, ...) from the following layer when
// not set explicitly, and wiring L4 checksums to the enclosing L3 layer.
func buildLayers(entries []layerEntry) ([]gopacket.SerializableLayer, error) {
	kinds := make([]string, len(entries))
	fields := make([]map[string]any, len(entries))
	for i, e := range entries {
		if len(e) != 1 {
			return nil, fmt.Errorf("layer %d must have exactly one type key", i)
		}
		for k, v := range e {
			kinds[i] = k
			fields[i] = v
		}
	}

	built := make([]gopacket.SerializableLayer, len(entries))
	var l3 gopacket.NetworkLayer
	for i, kind := range kinds {
		next := ""
		if i+1 < len(kinds) {
			next = kinds[i+1]
		}
		l, err := buildLayer(kind, fields[i], next)
		if err != nil {
			return nil, fmt.Errorf("layer %d (%s): %w", i, kind, err)
		}
		switch v := l.(type) {
		case *layers.IPv4:
			l3 = v
		case *layers.IPv6:
			l3 = v
		case *layers.UDP:
			if cks, ok := fields[i]["chksum"]; ok {
				// An explicit checksum is written verbatim, skipping
				// computation. UDP checksums are optional over IPv4, where 0
				// disables them; they are mandatory over IPv6, so do not force a
				// zero there.
				l = &noChecksumUDP{SrcPort: v.SrcPort, DstPort: v.DstPort, Checksum: uint16(toInt(cks))}
			} else if l3 != nil {
				if err := v.SetNetworkLayerForChecksum(l3); err != nil {
					return nil, fmt.Errorf("udp checksum: %w", err)
				}
			}
		case *layers.TCP:
			if l3 != nil {
				if err := v.SetNetworkLayerForChecksum(l3); err != nil {
					return nil, fmt.Errorf("tcp checksum: %w", err)
				}
			}
		}
		built[i] = l
	}
	return built, nil
}

func buildLayer(kind string, f map[string]any, next string) (gopacket.SerializableLayer, error) {
	switch normLayer(kind) {
	case "eth":
		return buildEth(f, next)
	case "dot1q":
		return buildDot1Q(f, next)
	case "ip":
		return buildIPv4(f, next)
	case "ipv6":
		return buildIPv6(f, next)
	case "udp":
		return buildUDP(f)
	case "tcp":
		return buildTCP(f)
	case "vxlan":
		return buildVXLAN(f)
	case "srh":
		return buildSRH(f, next)
	default:
		return nil, fmt.Errorf("unknown layer type %q", kind)
	}
}

func normLayer(k string) string {
	switch k {
	case "eth", "ether", "Ether", "Eth":
		return "eth"
	case "dot1q", "Dot1Q", "vlan", "VLAN":
		return "dot1q"
	case "ip", "IP", "ipv4", "IPv4":
		return "ip"
	case "ipv6", "IPv6":
		return "ipv6"
	case "udp", "UDP":
		return "udp"
	case "tcp", "TCP":
		return "tcp"
	case "vxlan", "VXLAN":
		return "vxlan"
	case "srh", "SRH", "srv6", "SRv6":
		return "srh"
	default:
		return k
	}
}

func buildEth(f map[string]any, next string) (*layers.Ethernet, error) {
	eth := &layers.Ethernet{
		SrcMAC: net.HardwareAddr{0, 0, 0, 0, 0, 1},
		DstMAC: net.HardwareAddr{0, 0, 0, 0, 0, 2},
	}
	if s, ok := f["src"]; ok {
		mac, err := net.ParseMAC(toStr(s))
		if err != nil {
			return nil, fmt.Errorf("src mac: %w", err)
		}
		eth.SrcMAC = mac
	}
	if s, ok := f["dst"]; ok {
		mac, err := net.ParseMAC(toStr(s))
		if err != nil {
			return nil, fmt.Errorf("dst mac: %w", err)
		}
		eth.DstMAC = mac
	}
	switch normLayer(next) {
	case "ip":
		eth.EthernetType = layers.EthernetTypeIPv4
	case "ipv6":
		eth.EthernetType = layers.EthernetTypeIPv6
	case "dot1q":
		eth.EthernetType = layers.EthernetTypeDot1Q
	}
	return eth, nil
}

func buildDot1Q(f map[string]any, next string) (*layers.Dot1Q, error) {
	d := &layers.Dot1Q{}
	if v, ok := f["vlan"]; ok {
		d.VLANIdentifier = uint16(toInt(v))
	}
	if v, ok := f["prio"]; ok {
		d.Priority = uint8(toInt(v))
	}
	switch normLayer(next) {
	case "ip":
		d.Type = layers.EthernetTypeIPv4
	case "ipv6":
		d.Type = layers.EthernetTypeIPv6
	case "dot1q":
		d.Type = layers.EthernetTypeDot1Q
	}
	return d, nil
}

func buildIPv4(f map[string]any, next string) (*layers.IPv4, error) {
	ip := &layers.IPv4{Version: 4, TTL: 64, SrcIP: net.IPv4(16, 0, 0, 1), DstIP: net.IPv4(48, 0, 0, 1)}
	if v, ok := f["src"]; ok {
		ip.SrcIP = net.ParseIP(toStr(v))
		if ip.SrcIP == nil {
			return nil, fmt.Errorf("invalid src ip %q", toStr(v))
		}
	}
	if v, ok := f["dst"]; ok {
		ip.DstIP = net.ParseIP(toStr(v))
		if ip.DstIP == nil {
			return nil, fmt.Errorf("invalid dst ip %q", toStr(v))
		}
	}
	if v, ok := f["ttl"]; ok {
		ip.TTL = uint8(toInt(v))
	}
	if v, ok := f["tos"]; ok {
		ip.TOS = uint8(toInt(v))
	}
	if v, ok := f["id"]; ok {
		ip.Id = uint16(toInt(v))
	}
	switch normLayer(next) {
	case "udp":
		ip.Protocol = layers.IPProtocolUDP
	case "tcp":
		ip.Protocol = layers.IPProtocolTCP
	}
	return ip, nil
}

func buildIPv6(f map[string]any, next string) (*layers.IPv6, error) {
	ip := &layers.IPv6{Version: 6, HopLimit: 64}
	if v, ok := f["src"]; ok {
		ip.SrcIP = net.ParseIP(toStr(v))
		if ip.SrcIP == nil {
			return nil, fmt.Errorf("invalid src ip %q", toStr(v))
		}
	}
	if v, ok := f["dst"]; ok {
		ip.DstIP = net.ParseIP(toStr(v))
		if ip.DstIP == nil {
			return nil, fmt.Errorf("invalid dst ip %q", toStr(v))
		}
	}
	if v, ok := f["hlim"]; ok {
		ip.HopLimit = uint8(toInt(v))
	}
	switch normLayer(next) {
	case "udp":
		ip.NextHeader = layers.IPProtocolUDP
	case "tcp":
		ip.NextHeader = layers.IPProtocolTCP
	case "srh":
		ip.NextHeader = layers.IPProtocolIPv6Routing
	case "ip":
		ip.NextHeader = layers.IPProtocolIPv4
	case "ipv6":
		ip.NextHeader = layers.IPProtocolIPv6
	}
	return ip, nil
}

// buildVXLAN builds a VXLAN header. The 'I' (valid VNI) bit is set by default,
// matching the reference profiles' flags=0x08.
func buildVXLAN(f map[string]any) (*layers.VXLAN, error) {
	vx := &layers.VXLAN{ValidIDFlag: true}
	if v, ok := f["vni"]; ok {
		vx.VNI = uint32(toInt(v))
	}
	if v, ok := f["flags"]; ok {
		vx.ValidIDFlag = toInt(v)&0x08 != 0
	}
	return vx, nil
}

// noChecksumUDP serializes a UDP header with a fixed checksum, bypassing
// gopacket's computation (which requires a pseudo-header and always overwrites
// the field). It is used when a profile pins the UDP checksum, e.g. chksum: 0 to
// disable it over an IPv4 underlay. The length field is still fixed up from the
// serialized payload. LayerType reports UDP so the decoded packet resolves field
// offsets normally.
type noChecksumUDP struct {
	SrcPort  layers.UDPPort
	DstPort  layers.UDPPort
	Checksum uint16
}

func (u *noChecksumUDP) LayerType() gopacket.LayerType { return layers.LayerTypeUDP }

func (u *noChecksumUDP) SerializeTo(b gopacket.SerializeBuffer, opts gopacket.SerializeOptions) error {
	payload := b.Bytes()
	bytes, err := b.PrependBytes(8)
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint16(bytes[0:], uint16(u.SrcPort))
	binary.BigEndian.PutUint16(bytes[2:], uint16(u.DstPort))
	binary.BigEndian.PutUint16(bytes[4:], uint16(len(payload)+8))
	binary.BigEndian.PutUint16(bytes[6:], u.Checksum)
	return nil
}

func buildUDP(f map[string]any) (*layers.UDP, error) {
	u := &layers.UDP{}
	if v, ok := f["sport"]; ok {
		u.SrcPort = layers.UDPPort(toInt(v))
	}
	if v, ok := f["dport"]; ok {
		u.DstPort = layers.UDPPort(toInt(v))
	}
	return u, nil
}

func buildTCP(f map[string]any) (*layers.TCP, error) {
	tcp := &layers.TCP{Window: 65535}
	if v, ok := f["sport"]; ok {
		tcp.SrcPort = layers.TCPPort(toInt(v))
	}
	if v, ok := f["dport"]; ok {
		tcp.DstPort = layers.TCPPort(toInt(v))
	}
	if v, ok := f["flags"]; ok {
		applyTCPFlags(tcp, toStr(v))
	}
	if v, ok := f["seq"]; ok {
		tcp.Seq = uint32(toInt(v))
	}
	return tcp, nil
}

func applyTCPFlags(tcp *layers.TCP, flags string) {
	for _, c := range flags {
		switch c {
		case 'S', 's':
			tcp.SYN = true
		case 'A', 'a':
			tcp.ACK = true
		case 'F', 'f':
			tcp.FIN = true
		case 'R', 'r':
			tcp.RST = true
		case 'P', 'p':
			tcp.PSH = true
		case 'U', 'u':
			tcp.URG = true
		}
	}
}
