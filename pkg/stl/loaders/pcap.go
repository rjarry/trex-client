// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package loaders reads non-DSL profile inputs: raw pcap captures and the
// frozen GUI/RPC snapshot format.
package loaders

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/gopacket/gopacket/pcapgo"

	"github.com/rjarry/trex-client/pkg/stl"
)

// PcapOptions controls how a capture is turned into streams.
type PcapOptions struct {
	// IPGMicros overrides the inter-packet gap; when zero the gaps from the
	// capture timestamps are used.
	IPGMicros float64
	// MinIPGMicros clamps the smallest gap when using capture timestamps.
	MinIPGMicros float64
	// Speedup divides the capture gaps (>1 sends faster). Ignored when
	// IPGMicros is set.
	Speedup float64
}

// LoadPcap turns a pcap/cap file into a chain of single-burst streams, one per
// packet, linked by next so they replay in capture order. This mirrors the
// reference load_pcap behavior: static packets with inter-stream gaps.
func LoadPcap(path string, opts PcapOptions) (stl.Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r, err := pcapgo.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("read pcap %s: %w", path, err)
	}
	if opts.Speedup == 0 {
		opts.Speedup = 1
	}

	var pkts []capPkt
	for {
		data, ci, err := r.ReadPacketData()

		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read packet: %w", err)
		}
		b := make([]byte, len(data))
		copy(b, data)
		pkts = append(pkts, capPkt{data: b, ts: ci.Timestamp})
	}
	if len(pkts) == 0 {
		return nil, fmt.Errorf("pcap %s has no packets", path)
	}

	profile := make(stl.Profile, len(pkts))
	for i, p := range pkts {
		pkt, err := stl.RawPacket(p.data)
		if err != nil {
			return nil, fmt.Errorf("packet %d: %w", i, err)
		}
		isg := gapBefore(pkts, i, opts)
		s := &stl.Stream{
			Name:      fmt.Sprintf("pkt_%d", i),
			Packet:    pkt,
			Mode:      stl.SingleBurst(stl.Rate{Type: stl.RatePPS, Value: 1}, 1),
			Enabled:   true,
			SelfStart: i == 0,
			ISG:       isg,
		}
		if i < len(pkts)-1 {
			s.Next = fmt.Sprintf("pkt_%d", i+1)
		}
		profile[i] = s
	}
	return profile, nil
}

// capPkt is a captured packet with its timestamp.
type capPkt struct {
	data []byte
	ts   time.Time
}

// gapBefore returns the inter-stream gap (microseconds) preceding packet i.
func gapBefore(pkts []capPkt, i int, opts PcapOptions) float64 {
	if opts.IPGMicros > 0 {
		return opts.IPGMicros
	}
	if i == 0 {
		return 0
	}
	gap := float64(pkts[i].ts.Sub(pkts[i-1].ts).Microseconds()) / opts.Speedup

	return max(gap, opts.MinIPGMicros)
}
