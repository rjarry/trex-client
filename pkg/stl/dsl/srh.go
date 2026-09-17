// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package dsl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

// srhLayerType identifies our SRv6 Segment Routing Header (IPv6 routing type 4,
// RFC 8754). gopacket's built-in IPv6Routing only serializes and decodes the
// deprecated type 0 header, so we register a custom layer and take over decoding
// of the IPv6 routing extension header (protocol 43). The client never emits a
// type-0 source-routing header, so replacing the decoder is safe.
var srhLayerType = gopacket.RegisterLayerType(1400, gopacket.LayerTypeMetadata{
	Name:    "SRH",
	Decoder: gopacket.DecodeFunc(decodeSRH),
})

func init() {
	layers.IPProtocolMetadata[layers.IPProtocolIPv6Routing] = layers.EnumMetadata{
		DecodeWith: gopacket.DecodeFunc(decodeSRH),
		Name:       "IPv6Routing",
		LayerType:  srhLayerType,
	}
}

// srh is an SRv6 Segment Routing Header. Segments are stored in wire order, with
// Segments[0] the last segment of the path and Segments[LastEntry] the first;
// the active segment is Segments[SegmentsLeft].
type srh struct {
	layers.BaseLayer
	NextHeader   layers.IPProtocol
	SegmentsLeft uint8
	LastEntry    uint8
	Flags        uint8
	Tag          uint16
	Segments     []net.IP
}

// LayerType returns the SRH layer type.
func (s *srh) LayerType() gopacket.LayerType { return srhLayerType }

// SerializeTo writes the SRH bytes, implementing gopacket.SerializableLayer.
func (s *srh) SerializeTo(b gopacket.SerializeBuffer, opts gopacket.SerializeOptions) error {
	n := 8 + len(s.Segments)*16
	bytes, err := b.PrependBytes(n)
	if err != nil {
		return err
	}
	bytes[0] = byte(s.NextHeader)
	bytes[1] = byte((n - 8) / 8) // Hdr Ext Len, in 8-octet units, first 8 excluded
	bytes[2] = 4                 // Routing Type: Segment Routing Header
	bytes[3] = s.SegmentsLeft
	bytes[4] = s.LastEntry
	bytes[5] = s.Flags
	binary.BigEndian.PutUint16(bytes[6:8], s.Tag)
	for i, seg := range s.Segments {
		copy(bytes[8+i*16:8+i*16+16], seg.To16())
	}
	return nil
}

// decodeSRH decodes an SRH so the packet walker can record the offsets of the
// layers that follow it (the field engine targets them by name).
func decodeSRH(data []byte, p gopacket.PacketBuilder) error {
	if len(data) < 8 {
		return errors.New("srh: header too short")
	}
	total := (int(data[1]) + 1) * 8
	if len(data) < total {
		return errors.New("srh: truncated segment list")
	}
	s := &srh{
		NextHeader:   layers.IPProtocol(data[0]),
		SegmentsLeft: data[3],
		LastEntry:    data[4],
		Flags:        data[5],
		Tag:          binary.BigEndian.Uint16(data[6:8]),
	}
	for off := 8; off+16 <= total; off += 16 {
		s.Segments = append(s.Segments, net.IP(data[off:off+16]))
	}
	s.BaseLayer = layers.BaseLayer{Contents: data[:total], Payload: data[total:]}
	p.AddLayer(s)
	return p.NextDecoder(s.NextHeader)
}

// buildSRH builds an SRH layer from the DSL fields. The following layer sets the
// header's next-header field.
func buildSRH(f map[string]any, next string) (*srh, error) {
	raw, ok := f["segments"]
	if !ok {
		return nil, fmt.Errorf("srh: missing segments")
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("srh: segments must be a non-empty list")
	}
	s := &srh{}
	for _, item := range list {
		ip := net.ParseIP(toStr(item))
		if ip == nil {
			return nil, fmt.Errorf("srh: invalid segment %q", toStr(item))
		}
		s.Segments = append(s.Segments, ip)
	}
	s.LastEntry = uint8(len(s.Segments) - 1)
	if v, ok := f["segleft"]; ok {
		s.SegmentsLeft = uint8(toInt(v))
	} else {
		s.SegmentsLeft = s.LastEntry
	}
	if v, ok := f["flags"]; ok {
		s.Flags = uint8(toInt(v))
	}
	if v, ok := f["tag"]; ok {
		s.Tag = uint16(toInt(v))
	}
	switch normLayer(next) {
	case "udp":
		s.NextHeader = layers.IPProtocolUDP
	case "tcp":
		s.NextHeader = layers.IPProtocolTCP
	case "ip":
		s.NextHeader = layers.IPProtocolIPv4
	case "ipv6":
		s.NextHeader = layers.IPProtocolIPv6
	default:
		s.NextHeader = layers.IPProtocolNoNextHeader
	}
	return s, nil
}
