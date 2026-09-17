// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package stl

// Mode is a stream transmit mode serialized as the stream's "mode" object. Only
// the fields relevant to Type are emitted.
type Mode struct {
	Rate Rate   `json:"rate"`
	Type string `json:"type"`

	// single_burst
	TotalPkts *int `json:"total_pkts,omitempty"`

	// multi_burst
	PktsPerBurst *int     `json:"pkts_per_burst,omitempty"`
	IBG          *float64 `json:"ibg,omitempty"`
	Count        *int     `json:"count,omitempty"`
}

// Mode type constants.
const (
	ModeContinuous  = "continuous"
	ModeSingleBurst = "single_burst"
	ModeMultiBurst  = "multi_burst"
)

// Continuous returns a continuous transmit mode at the given rate.
func Continuous(rate Rate) Mode {
	return Mode{Rate: rate, Type: ModeContinuous}
}

// SingleBurst returns a single-burst mode of totalPkts packets at the given rate.
func SingleBurst(rate Rate, totalPkts int) Mode {
	return Mode{Rate: rate, Type: ModeSingleBurst, TotalPkts: &totalPkts}
}

// MultiBurst returns a multi-burst mode: count bursts of pktsPerBurst packets
// each, separated by ibg microseconds, at the given rate.
func MultiBurst(rate Rate, pktsPerBurst int, ibg float64, count int) Mode {
	return Mode{
		Rate:         rate,
		Type:         ModeMultiBurst,
		PktsPerBurst: &pktsPerBurst,
		IBG:          &ibg,
		Count:        &count,
	}
}
