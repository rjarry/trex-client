// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package ndr

import (
	"context"
	"fmt"
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
	pgids  []int
}

// NewEngine builds an engine for the given ports. The profile must already be
// loaded and flow-stat tagged so per-pgid counters exist.
func NewEngine(client *session.Client, ports []*session.Port, cfg Config) *Engine {
	return &Engine{client: client, ports: ports, cfg: cfg}
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
	results := &Results{Config: e.cfg, FirstRun: first}
	if first.Pass {
		results.NDRPercent = 100
		results.NDRRun = first
		return results, nil
	}

	ndr, run, err := e.binarySearch(ctx, 0, 100, results)
	if err != nil {
		return nil, err
	}
	results.NDRPercent = ndr
	results.NDRRun = run
	return results, nil
}

// binarySearch narrows the [low, high] percentage bracket until it is smaller
// than PDRError or MaxIterations is reached, returning the highest passing rate.
func (e *Engine) binarySearch(ctx context.Context, low, high float64, results *Results) (float64, RunResult, error) {
	var runErr error
	measure := func(pct float64) (RunResult, bool) {
		run, err := e.perfRun(ctx, pct, e.cfg.IterationDuration)
		if err != nil {
			runErr = err
			return RunResult{}, false
		}
		results.Iterations = append(results.Iterations, run)
		return run, true
	}
	best, bestRun := search(low, high, e.cfg.PDRError, e.cfg.MaxIterations, measure)
	return best, bestRun, runErr
}

// search is the pure binary-search core: it repeatedly measures the midpoint,
// raising the floor on a pass and lowering the ceiling on a fail, until the
// bracket is smaller than pdrError or maxIter runs elapse. measure returns
// (result, ok); ok=false aborts the search (used to surface measurement errors).
func search(low, high, pdrError float64, maxIter int, measure func(pct float64) (RunResult, bool)) (float64, RunResult) {
	best := low
	var bestRun RunResult
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
// given server-timed duration.
func (e *Engine) perfRun(ctx context.Context, pct, duration float64) (RunResult, error) {
	if err := e.stopAll(); err != nil {
		return RunResult{}, err
	}

	base, err := stats.GetPgidStats(e.client.Conn(), e.pgids)
	if err != nil {
		return RunResult{}, err
	}
	baseQF, _ := e.client.Stats().GlobalCounter(queueFullKey)

	// Subscribe to job-done before starting so the completion cannot be missed.
	waits := make([]<-chan struct{}, len(e.ports))
	cancels := make([]func(), len(e.ports))
	for i, p := range e.ports {
		waits[i], cancels[i] = p.WaitJobDone()
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	mul := session.Mult{Type: session.MultPercentage, Value: pct}
	if e.cfg.RampUpSteps > 0 {
		if err := e.rampUp(mul, duration); err != nil {
			return RunResult{}, err
		}
	}
	for _, p := range e.ports {
		if err := p.Start(mul, duration, true, 0); err != nil {
			return RunResult{}, err
		}
	}

	// Block on the server completion event, with a margin over the timed run.
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(duration+10)*time.Second)
	defer cancel()
	for i := range waits {
		select {
		case <-waits[i]:
		case <-runCtx.Done():
			return RunResult{}, fmt.Errorf("timeout waiting for port %d to finish", e.ports[i].ID())
		}
	}

	// Allow RX to drain before reading counters.
	select {
	case <-time.After(time.Duration(e.cfg.RxDelayMs) * time.Millisecond):
	case <-ctx.Done():
		return RunResult{}, ctx.Err()
	}

	final, err := stats.GetPgidStats(e.client.Conn(), e.pgids)
	if err != nil {
		return RunResult{}, err
	}
	finalQF, _ := e.client.Stats().GlobalCounter(queueFullKey)

	return e.evaluate(pct, base, final, uint64(baseQF), uint64(finalQF)), nil
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

// rampUp geometrically raises the transmit rate to the target before a measured
// run, avoiding an initial burst drop. It starts a brief continuous phase and
// steps the multiplier up via update_traffic, then stops so the measured run
// begins clean.
func (e *Engine) rampUp(target session.Mult, duration float64) error {
	for _, p := range e.ports {
		if err := p.Start(session.Mult{Type: target.Type, Value: target.Value / float64(int(1)<<e.cfg.RampUpSteps)}, 0, true, 0); err != nil {
			return err
		}
	}
	step := time.Duration(float64(time.Second) * duration / float64(e.cfg.RampUpSteps+1))
	for i := e.cfg.RampUpSteps - 1; i >= 0; i-- {
		time.Sleep(step)
		v := target.Value / float64(int(1)<<i)
		for _, p := range e.ports {
			if err := p.Update(session.Mult{Type: target.Type, Value: v}, true); err != nil {
				return err
			}
		}
	}
	return e.stopAll()
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
