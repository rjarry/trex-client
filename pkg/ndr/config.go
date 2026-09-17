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
	// RampUpTime, when > 0, gradually raises the rate to the target over this
	// many seconds at the start of every run (in ~100ms steps) so the DUT is not
	// hit by a sudden 0->target burst. The ramp flows into the measured hold and
	// is counted in the run's tx/rx/drop. 0 starts straight at the target.
	RampUpTime float64
	// OptBinSearch narrows the initial search interval using the 100% probe's
	// drop rate instead of starting from the full [0, 100] range.
	OptBinSearch bool
	// OptBinSearchPercent is the half-width (percent) of the optimized interval.
	OptBinSearchPercent float64
	// Verbose prints each measured run as the search progresses.
	Verbose bool
	// Title is a free-form label recorded in the results.
	Title string
	// Ports is the set of ports to run traffic on.
	Ports []int
}

// DefaultConfig returns sensible NDR defaults.
func DefaultConfig() Config {
	return Config{
		PDR:                 0.0,
		PDRError:            1.0,
		IterationDuration:   20,
		FirstRunDuration:    20,
		QFullResolution:     2.0,
		MaxIterations:       20,
		RxDelayMs:           500,
		OptBinSearchPercent: 5.0,
		RampUpTime:          1.0,
	}
}
