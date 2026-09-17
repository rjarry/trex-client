// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package ndr implements a client-side, server-timed no-drop-rate search. There
// is no server NDR primitive, so the binary search runs here against exact
// per-flow-group counters with generator-health validity gates.
package ndr

// Config parameters the NDR search. Percentages are 0-100.
type Config struct {
	// PDR is the acceptable packet drop rate (percent) for a passing run.
	PDR float64
	// PDRError is the convergence window (percent of line rate): the search
	// stops once the passing/failing bracket is narrower than this.
	PDRError float64
	// IterationDuration is the server-timed length of each search run (seconds).
	IterationDuration float64
	// FirstRunDuration is the length of the initial 100% probe (seconds).
	FirstRunDuration float64
	// QFullResolution is the acceptable queue-full rate (percent of tx) before a
	// run is treated as generator-limited rather than DUT-limited.
	QFullResolution float64
	// MaxIterations caps the number of binary-search runs.
	MaxIterations int
	// RxDelayMs is the drain wait after job-done before reading RX counters.
	RxDelayMs int
	// RampUpSteps, when > 0, pre-warms traffic by ramping to the target rate in
	// this many update_traffic steps before each measured run.
	RampUpSteps int
	// Ports is the set of ports to run traffic on.
	Ports []int
}

// DefaultConfig returns sensible NDR defaults.
func DefaultConfig() Config {
	return Config{
		PDR:               0.1,
		PDRError:          1.0,
		IterationDuration: 20,
		FirstRunDuration:  20,
		QFullResolution:   2.0,
		MaxIterations:     20,
		RxDelayMs:         500,
	}
}
