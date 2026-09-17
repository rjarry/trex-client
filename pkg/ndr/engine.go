// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package ndr

import (
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/rjarry/trex-client/pkg/session"
	"github.com/rjarry/trex-client/pkg/stats"
)

// queueFullKey is the raw global counter for packets the generator could not
// enqueue: when non-zero the generator, not the DUT, was the bottleneck.
const queueFullKey = "m_total_queue_full"

// Engine runs the NDR binary search over a set of acquired, stream-loaded ports.
type Engine struct {
	client *session.Client
	ports  []*session.Port
	cfg    Config
	out    io.Writer
	pgids  []int
}

// NewEngine builds an engine for the given ports. The profile must already be
// loaded and flow-stat tagged so per-pgid counters exist. out receives the
// verbose per-iteration log when Config.Verbose is set (nil discards it).
func NewEngine(client *session.Client, ports []*session.Port, cfg Config, out io.Writer) *Engine {
	if out == nil {
		out = io.Discard
	}
	return &Engine{client: client, ports: ports, cfg: cfg, out: out}
}

// RunResult is a single measurement at a given rate percentage.
type RunResult struct {
	Percentage float64
	TxPkts     uint64
	RxPkts     uint64
	Drop       uint64
	DropPct    float64
	QueueFull  uint64
	QFullPct   float64
	RxErr      uint64
	Pass       bool
}

// Find runs the search and returns the NDR result. It discovers the active
// pgids, probes at 100%, then binary-searches for the highest passing rate.
func (e *Engine) Find(ctx context.Context) (*Results, error) {
	lat, fs, err := stats.GetActivePGIDs(e.client.Conn())
	if err != nil {
		return nil, fmt.Errorf("get active pgids: %w", err)
	}

	e.pgids = slices.Concat(fs, lat)
	if len(e.pgids) == 0 {
		return nil, fmt.Errorf("no active flow-stat pgids; tag streams with flow_stats before NDR")
	}

	first, err := e.perfRun(ctx, 100, e.cfg.FirstRunDuration)
	if err != nil {
		return nil, err
	}
	e.report(first)
	results := &Results{Config: e.cfg, Title: e.cfg.Title, FirstRun: first}
	if first.Pass {
		results.NDRPercent = 100
		results.NDRRun = first
		return results, nil
	}

	var ndr float64
	var run RunResult
	if e.cfg.OptBinSearch {
		ndr, run, err = e.optBinarySearch(ctx, first, results)
	} else {
		ndr, run, err = e.binarySearch(ctx, 0, 100, 0, RunResult{}, results)
	}
	if err != nil {
		return nil, err
	}
	results.NDRPercent = ndr
	results.NDRRun = run
	return results, nil
}

// optBinarySearch seeds the search interval from the 100% probe's drop rate: the
// no-drop rate is assumed near (100 - drop%), so it searches a narrow window
// around it, widening down or up when the window's edges disprove that guess.
func (e *Engine) optBinarySearch(ctx context.Context, first RunResult, results *Results) (float64, RunResult, error) {
	p := e.cfg.OptBinSearchPercent
	assumed := 100 - first.DropPct
	lo := clampPct(assumed - p)
	hi := clampPct(assumed + p)

	loRun, err := e.measure(ctx, lo, results)
	if err != nil {
		return 0, RunResult{}, err
	}
	if !loRun.Pass {
		// The assumed rate was too high: search below the window.
		return e.binarySearch(ctx, 0, lo, 0, RunResult{}, results)
	}

	hiRun, err := e.measure(ctx, hi, results)
	if err != nil {
		return 0, RunResult{}, err
	}
	if hiRun.Pass {
		// The DUT is better than assumed: search above the window.
		return e.binarySearch(ctx, hi, 100, hi, hiRun, results)
	}

	// The no-drop rate lies within the window; lo is a known-good floor.
	return e.binarySearch(ctx, lo, hi, lo, loRun, results)
}

// measure runs one iteration, records it and logs it when verbose.
func (e *Engine) measure(ctx context.Context, pct float64, results *Results) (RunResult, error) {
	run, err := e.perfRun(ctx, pct, e.cfg.IterationDuration)
	if err != nil {
		return RunResult{}, err
	}
	results.Iterations = append(results.Iterations, run)
	e.report(run)
	return run, nil
}

// report always logs one measured run's result.
func (e *Engine) report(run RunResult) {
	verdict := "FAIL"
	if run.Pass {
		verdict = "pass"
	}
	fmt.Fprintf(e.out, "  %6.2f%%  tx=%d rx=%d drop=%d (%.4f%%) qfull=%.4f%% rx_err=%d  %s\n",
		run.Percentage, run.TxPkts, run.RxPkts, run.Drop, run.DropPct, run.QFullPct, run.RxErr, verdict)
}

// verbose logs a client-side action (traffic control, ramp-up, RPC reads) when
// verbose output is enabled.
func (e *Engine) verbose(format string, args ...any) {
	if !e.cfg.Verbose {
		return
	}
	fmt.Fprintf(e.out, "    · "+format+"\n", args...)
}

// portIDs returns the ids of the engine's ports, for logging.
func (e *Engine) portIDs() []int {
	ids := make([]int, len(e.ports))
	for i, p := range e.ports {
		ids[i] = p.ID()
	}
	return ids
}

// clampPct bounds a percentage to [0, 100].
func clampPct(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 100:
		return 100
	default:
		return v
	}
}

// binarySearch narrows the [low, high] percentage bracket until it is smaller
// than PDRError or MaxIterations is reached, returning the highest passing rate.
// bestSeed/runSeed prime the floor with an already-known passing rate (used by
// the optimized search); pass 0/RunResult{} when there is none.
func (e *Engine) binarySearch(ctx context.Context, low, high, bestSeed float64, runSeed RunResult, results *Results) (float64, RunResult, error) {
	var runErr error
	measure := func(pct float64) (RunResult, bool) {
		run, err := e.measure(ctx, pct, results)
		if err != nil {
			runErr = err
			return RunResult{}, false
		}
		return run, true
	}
	best, bestRun := search(low, high, bestSeed, runSeed, e.cfg.PDRError, e.cfg.MaxIterations, measure)
	return best, bestRun, runErr
}

// search is the pure binary-search core: it repeatedly measures the midpoint,
// raising the floor on a pass and lowering the ceiling on a fail, until the
// bracket is smaller than pdrError or maxIter runs elapse. best/bestRun seed the
// current floor. measure returns (result, ok); ok=false aborts the search (used
// to surface measurement errors).
func search(low, high, best float64, bestRun RunResult, pdrError float64, maxIter int, measure func(pct float64) (RunResult, bool)) (float64, RunResult) {
	for i := 0; i < maxIter && (high-low) > pdrError; i++ {
		mid := (low + high) / 2
		run, ok := measure(mid)
		if !ok {
			return best, bestRun
		}
		if run.Pass {
			low = mid
			best = mid
			bestRun = run
		} else {
			high = mid
		}
	}
	return best, bestRun
}

// perfRun performs one precise measurement at pct percent of line rate for the
// given server-timed duration. A non-positive rate is a trivial no-traffic pass
// (the server rejects a zero multiplier), so the search can bottom out at 0.
func (e *Engine) perfRun(ctx context.Context, pct, duration float64) (RunResult, error) {
	if pct <= 0 {
		return RunResult{Percentage: pct, Pass: true}, nil
	}
	e.verbose("run %.2f%% for %.0fs (ramp %.1fs) on ports %v", pct, duration, e.cfg.RampUpTime, e.portIDs())
	if err := e.stopAll(); err != nil {
		return RunResult{}, err
	}

	// Baseline before any traffic so the ramp-up is counted in the run.
	e.verbose("get_pgid_stats: baseline for pgids %v", e.pgids)
	base, err := stats.GetPgidStats(e.client.Conn(), e.pgids)
	if err != nil {
		return RunResult{}, err
	}
	baseQF, _ := e.client.Stats().GlobalCounter(queueFullKey)

	// From here traffic may be running; guarantee it is stopped on every return
	// path, including cancellation (Ctrl-C).
	defer func() { _ = e.stopAll() }()

	// Ramp progressively up to the target rate so the DUT is not hit by a sudden
	// 0->target burst, then hold at the target for the run duration. The whole
	// session (ramp + hold) is measured.
	if err := e.rampTo(ctx, pct); err != nil {
		return RunResult{}, err
	}
	e.verbose("hold %.2f%% for %.0fs", pct, duration)
	if err := sleepCtx(ctx, time.Duration(duration*float64(time.Second))); err != nil {
		return RunResult{}, err
	}

	e.verbose("stop_traffic")
	if err := e.stopAll(); err != nil {
		return RunResult{}, err
	}

	// Allow RX to drain before reading counters.
	e.verbose("draining rx for %dms", e.cfg.RxDelayMs)
	if err := sleepCtx(ctx, time.Duration(e.cfg.RxDelayMs)*time.Millisecond); err != nil {
		return RunResult{}, err
	}

	e.verbose("get_pgid_stats: final for pgids %v", e.pgids)
	final, err := stats.GetPgidStats(e.client.Conn(), e.pgids)
	if err != nil {
		return RunResult{}, err
	}
	finalQF, _ := e.client.Stats().GlobalCounter(queueFullKey)

	return e.evaluate(pct, base, final, uint64(baseQF), uint64(finalQF)), nil
}

// sleepCtx sleeps for d unless ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// evaluate reduces baseline/final snapshots into a pass/fail RunResult.
func (e *Engine) evaluate(pct float64, base, final *stats.PgidSnapshot, baseQF, finalQF uint64) RunResult {
	tx := final.SumTx() - base.SumTx()
	rx := final.SumRx() - base.SumRx()
	drop := uint64(0)
	if tx > rx {
		drop = tx - rx
	}
	rxErr := final.RxErr - base.RxErr
	qf := finalQF - baseQF

	r := RunResult{
		Percentage: pct,
		TxPkts:     tx,
		RxPkts:     rx,
		Drop:       drop,
		QueueFull:  qf,
		RxErr:      rxErr,
	}
	if tx > 0 {
		r.DropPct = float64(drop) / float64(tx) * 100
		r.QFullPct = float64(qf) / float64(tx) * 100
	}
	// A run passes only if the DUT (not the generator) stayed healthy.
	r.Pass = r.DropPct <= e.cfg.PDR &&
		r.QFullPct <= e.cfg.QFullResolution &&
		rxErr == 0
	return r
}

// rampStep is the interval between rate increments during ramp-up.
const rampStep = 100 * time.Millisecond

// rampTo brings the transmit rate up to pct and leaves traffic running
// (continuous) for the caller to hold and stop. With RampUpTime > 0 it starts
// low and raises the rate linearly in ~100ms steps so the DUT wakes up
// gradually; otherwise it starts straight at the target.
func (e *Engine) rampTo(ctx context.Context, pct float64) error {
	steps := 1
	interval := rampStep
	if e.cfg.RampUpTime > 0 {
		steps = int(e.cfg.RampUpTime*float64(time.Second)/float64(rampStep)) + 1
		interval = time.Duration(e.cfg.RampUpTime * float64(time.Second) / float64(steps))
		e.verbose("ramp-up: to %.2f%% over %.1fs in %d steps", pct, e.cfg.RampUpTime, steps)
	}

	initial := pct / float64(steps)
	e.verbose("start_traffic %.2f%%", initial)
	for _, p := range e.ports {
		if err := p.Start(session.Mult{Type: session.MultPercentage, Value: initial}, 0, true, 0); err != nil {
			return err
		}
	}

	for i := 2; i <= steps; i++ {
		if err := sleepCtx(ctx, interval); err != nil {
			return err
		}
		v := pct * float64(i) / float64(steps)
		e.verbose("ramp-up: update_traffic %.2f%%", v)
		for _, p := range e.ports {
			if err := p.Update(session.Mult{Type: session.MultPercentage, Value: v}, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// stopAll stops traffic on every port. Stopping a port that is not
// transmitting is expected (the first run starts from a freshly loaded, idle
// port), so those errors are ignored to guarantee a clean starting state.
func (e *Engine) stopAll() error {
	for _, p := range e.ports {
		_ = p.Stop()
	}
	return nil
}
