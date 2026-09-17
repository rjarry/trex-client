// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package ndr

import (
	"math"
	"testing"
)

func TestSearchConverges(t *testing.T) {
	// True NDR is 37.5%; a run passes iff its rate is at or below that.
	const trueNDR = 37.5
	calls := 0
	measure := func(pct float64) (RunResult, bool) {
		calls++
		return RunResult{Percentage: pct, Pass: pct <= trueNDR}, true
	}

	best, run := search(0, 100, 0, RunResult{}, 1.0, 20, measure)
	if math.Abs(best-trueNDR) > 1.0 {
		t.Errorf("converged to %.3f, want within 1%% of %.1f", best, trueNDR)
	}
	if !run.Pass {
		t.Error("reported NDR run should be a passing run")
	}
	if best > trueNDR {
		t.Errorf("NDR %.3f exceeds true no-drop rate %.1f", best, trueNDR)
	}
}

func TestSearchRespectsMaxIterations(t *testing.T) {
	calls := 0
	measure := func(pct float64) (RunResult, bool) {
		calls++
		return RunResult{Percentage: pct, Pass: pct <= 50}, true
	}
	// Tiny error window forces convergence by iteration cap, not bracket width.
	search(0, 100, 0, RunResult{}, 0.0001, 5, measure)
	if calls != 5 {
		t.Errorf("ran %d iterations, want 5", calls)
	}
}

func TestSearchAbortsOnError(t *testing.T) {
	measure := func(pct float64) (RunResult, bool) {
		return RunResult{}, false
	}
	best, _ := search(0, 100, 0, RunResult{}, 1.0, 20, measure)
	if best != 0 {
		t.Errorf("aborted search should return the initial floor, got %.3f", best)
	}
}
