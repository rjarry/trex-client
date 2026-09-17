// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/rjarry/trex-client/pkg/ndr"
	"github.com/rjarry/trex-client/pkg/session"
	"github.com/rjarry/trex-client/pkg/stats"
	"github.com/rjarry/trex-client/pkg/stl"
	"github.com/rjarry/trex-client/pkg/stl/loaders"
)

// Commands groups the traffic commands shared by the non-interactive CLI and
// the TUI's embedded command line, so a command is defined once and behaves
// identically in either front-end.
type Commands struct {
	Streams StreamsCmd `cmd:"" help:"Load a profile onto ports."`
	Start   StartCmd   `cmd:"" help:"Start traffic on acquired ports."`
	Stop    StopCmd    `cmd:"" help:"Stop traffic on all acquired ports."`
	Stats   StatsCmd   `cmd:"" help:"Show global and flow-group stats."`
	Clear   ClearCmd   `cmd:"" help:"Baseline stats counters."`
	NDR     NDRCmd     `cmd:"" name:"ndr" help:"Run an NDR search."`
}

// acquirePorts acquires the given ports on the client and records them on the
// context, replacing any previously acquired set. When force is set the ports
// are taken even if owned by another user or session.
func (c *Context) acquirePorts(ids []int, force bool) error {
	ports := make([]*session.Port, 0, len(ids))
	for _, id := range ids {
		p := c.Client.Port(id)
		if err := p.Acquire(force); err != nil {
			return err
		}
		ports = append(ports, p)
	}
	c.Ports = ports
	return nil
}

// ReleasePorts relinquishes every acquired port, best-effort. It is called when
// the process exits (including when the interactive shell quits) so ports are
// not left owned by a session that is going away.
func (c *Context) ReleasePorts() {
	for _, p := range c.Ports {
		_ = p.Release()
	}
	c.Ports = nil
}

type StreamsCmd struct {
	Profile  string            `arg:"" completion:"file" help:"Profile to load (.py, .yaml, .pcap or snapshot)."`
	Ports    []int             `short:"p" default:"0,1" help:"Ports to load the profile onto."`
	Tunables map[string]string `short:"t" help:"Profile tunable key=value (repeatable)."`
	Force    bool              `short:"f" help:"Force-acquire ports even if owned by another session."`
}

func (c *StreamsCmd) Run(cc *Context) error {
	profile, err := loaders.Load(c.Profile, c.Tunables)
	if err != nil {
		return err
	}
	// Resolve once up front so a broken profile fails before ports are taken.
	if _, err := profile.Resolve(); err != nil {
		return err
	}
	if err := cc.acquirePorts(c.Ports, c.Force); err != nil {
		return err
	}
	total := 0
	for _, p := range cc.Ports {
		n, err := loadPort(p, profile)
		if err != nil {
			return err
		}
		total += n
	}
	fmt.Fprintf(cc.Out, "loaded %d streams onto ports %v\n", total, c.Ports)
	return nil
}

// loadPort clears the port and uploads the sub-profile matching its direction,
// returning the number of streams loaded. Per-port resolution keeps each
// direction's streams (and their flow-stat pg ids) independent.
func loadPort(p *session.Port, profile stl.Profile) (int, error) {
	resolved, err := profile.ForPort(p.ID()).Resolve()
	if err != nil {
		return 0, err
	}
	if err := p.RemoveAllStreams(); err != nil {
		return 0, err
	}
	if len(resolved) == 0 {
		return 0, nil
	}
	if err := p.AddStreams(resolved); err != nil {
		return 0, err
	}
	return len(resolved), nil
}

type StartCmd struct {
	Mult     float64 `short:"m" default:"100" help:"Rate as percentage of line rate."`
	Duration float64 `short:"d" default:"0" help:"Duration in seconds (0 = until stopped)."`
}

func (c *StartCmd) Run(cc *Context) error {
	if len(cc.Ports) == 0 {
		return fmt.Errorf("no ports acquired; run streams first")
	}
	mul := session.Mult{Type: session.MultPercentage, Value: c.Mult}
	for _, p := range cc.Ports {
		if err := p.Start(mul, c.Duration, true, 0); err != nil {
			return err
		}
	}
	fmt.Fprintf(cc.Out, "started traffic at %.2f%%\n", c.Mult)
	return nil
}

type StopCmd struct {
	Ports []int `short:"p" default:"0,1" help:"Ports to stop when none are held in this session."`
	Force bool  `short:"f" default:"true" negatable:"" help:"Force-acquire the ports in order to stop them."`
}

func (c *StopCmd) Run(cc *Context) error {
	// Each non-interactive invocation is a fresh process with no acquired
	// ports, so stopping must (force-)acquire the target ports first. In the
	// interactive shell the already-held ports are reused.
	if len(cc.Ports) == 0 {
		if err := cc.acquirePorts(c.Ports, c.Force); err != nil {
			return err
		}
	}
	for _, p := range cc.Ports {
		if err := p.Stop(); err != nil {
			return err
		}
	}
	fmt.Fprintln(cc.Out, "stopped")
	return nil
}

type StatsCmd struct{}

func (c *StatsCmd) Run(cc *Context) error {
	if v, ok := cc.Client.Stats().GlobalCounter("m_tx_bps"); ok {
		fmt.Fprintf(cc.Out, "tx: %.0f bps  ", v)
	}
	if v, ok := cc.Client.Stats().GlobalCounter("m_rx_bps"); ok {
		fmt.Fprintf(cc.Out, "rx: %.0f bps  ", v)
	}
	if v, ok := cc.Client.Stats().GlobalDelta("m_total_queue_full"); ok {
		fmt.Fprintf(cc.Out, "queue_full: %.0f", v)
	}
	fmt.Fprintln(cc.Out)

	lat, flow, err := stats.GetActivePGIDs(cc.Client.Conn())
	if err != nil {
		return err
	}
	pgids := append(append([]int{}, flow...), lat...)
	if len(pgids) == 0 {
		return nil
	}
	snap, err := stats.GetPgidStats(cc.Client.Conn(), pgids)
	if err != nil {
		return err
	}
	fmt.Fprintf(cc.Out, "flow groups: tx=%d rx=%d drop=%d rx_err=%d\n",
		snap.SumTx(), snap.SumRx(), snap.Drop(), snap.RxErr)
	return nil
}

type ClearCmd struct{}

func (c *ClearCmd) Run(cc *Context) error {
	if err := cc.Client.Barrier(5*time.Second, true); err != nil {
		return err
	}
	cc.Client.Stats().Clear()
	fmt.Fprintln(cc.Out, "cleared")
	return nil
}

type NDRCmd struct {
	Profile       string            `arg:"" completion:"file" help:"Profile to load (.py, .yaml, .pcap or snapshot)."`
	Duration      float64           `short:"d" default:"20" help:"Per-iteration run duration in seconds."`
	FirstDuration float64           `default:"0" help:"First 100% probe duration in seconds (0 = same as --duration)."`
	PDR           float64           `default:"0" help:"Acceptable drop rate as percent of traffic; 0 = strict no-drop."`
	Window        float64           `short:"w" default:"1" help:"Search resolution: stop once the rate bracket is narrower than this (percent of line rate)."`
	QFull         float64           `short:"q" default:"2" help:"Max queue-full (percent of tx) before a run counts as generator-limited, not a valid DUT result."`
	MaxIterations int               `short:"x" default:"20" help:"Maximum number of binary-search iterations."`
	OptBinSearch  bool              `help:"Optimized binary search: seed the interval from the 100% drop rate instead of [0,100]."`
	OptBinPercent float64           `name:"opt-bin-search-percent" default:"5" help:"Half-width (percent) of the optimized search interval."`
	RampUpTime    float64           `default:"1" help:"Seconds to ramp traffic up to the target at the start of each run (0 = start straight at target)."`
	Title         string            `short:"T" help:"Title recorded in the results and JSON report."`
	Verbose       bool              `short:"v" help:"Print each iteration's result as the search runs."`
	Out           string            `short:"o" help:"Write full results as JSON to this file."`
	Ports         []int             `short:"p" default:"0,1" help:"Ports to run the search on."`
	Tunables      map[string]string `short:"t" help:"Profile tunable key=value (repeatable)."`
	Force         bool              `short:"f" help:"Force-acquire ports even if owned by another session."`
}

func (c *NDRCmd) Run(ctx context.Context, cc *Context) error {
	profile, err := loaders.Load(c.Profile, c.Tunables)
	if err != nil {
		return err
	}
	// NDR needs every test stream flow-stat tagged for exact per-pgid counts.
	// Tag before splitting by direction so pg ids stay unique across ports.
	profile.EnsureFlowStats(1)
	if _, err := profile.Resolve(); err != nil {
		return err
	}
	if err := cc.acquirePorts(c.Ports, c.Force); err != nil {
		return err
	}
	for _, p := range cc.Ports {
		if _, err := loadPort(p, profile); err != nil {
			return err
		}
	}

	cfg := ndr.DefaultConfig()
	cfg.IterationDuration = c.Duration
	cfg.FirstRunDuration = c.Duration
	if c.FirstDuration > 0 {
		cfg.FirstRunDuration = c.FirstDuration
	}
	cfg.PDR = c.PDR
	cfg.PDRError = c.Window
	cfg.QFullResolution = c.QFull
	cfg.MaxIterations = c.MaxIterations
	cfg.OptBinSearch = c.OptBinSearch
	cfg.OptBinSearchPercent = c.OptBinPercent
	cfg.RampUpTime = c.RampUpTime
	cfg.Verbose = c.Verbose
	cfg.Title = c.Title
	cfg.Ports = c.Ports

	engine := ndr.NewEngine(cc.Client, cc.Ports, cfg, cc.Out)
	results, err := engine.Find(ctx)
	if err != nil {
		return err
	}
	fmt.Fprint(cc.Out, results.String())
	if c.Out != "" {
		if err := results.WriteJSON(c.Out); err != nil {
			return err
		}
		fmt.Fprintf(cc.Out, "wrote %s\n", c.Out)
	}
	return nil
}
