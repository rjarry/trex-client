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
// context, replacing any previously acquired set.
func (c *Context) acquirePorts(ids []int) error {
	ports := make([]*session.Port, 0, len(ids))
	for _, id := range ids {
		p := c.Client.Port(id)
		if err := p.Acquire(false); err != nil {
			return err
		}
		ports = append(ports, p)
	}
	c.Ports = ports
	return nil
}

type StreamsCmd struct {
	Profile  string            `arg:"" completion:"file" help:"Profile to load (.py, .yaml, .pcap or snapshot)."`
	Ports    []int             `short:"p" default:"0,1" help:"Ports to load the profile onto."`
	Tunables map[string]string `short:"t" help:"Profile tunable key=value (repeatable)."`
}

func (c *StreamsCmd) Run(cc *Context) error {
	profile, err := loaders.Load(c.Profile, c.Tunables)
	if err != nil {
		return err
	}
	resolved, err := profile.Resolve()
	if err != nil {
		return err
	}
	if err := cc.acquirePorts(c.Ports); err != nil {
		return err
	}
	for _, p := range cc.Ports {
		if err := p.RemoveAllStreams(); err != nil {
			return err
		}
		if err := p.AddStreams(resolved); err != nil {
			return err
		}
	}
	fmt.Fprintf(cc.Out, "loaded %d streams onto ports %v\n", len(resolved), c.Ports)
	return nil
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

type StopCmd struct{}

func (c *StopCmd) Run(cc *Context) error {
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
	if v, ok := cc.Client.Stats().GlobalCounter("m_total_queue_full"); ok {
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
	Profile  string            `arg:"" completion:"file" help:"Profile to load (.py, .yaml, .pcap or snapshot)."`
	Duration float64           `short:"d" default:"20" help:"Per-iteration duration in seconds."`
	Out      string            `short:"o" help:"Write full results as JSON to this file."`
	Ports    []int             `short:"p" default:"0,1" help:"Ports to run the search on."`
	Tunables map[string]string `short:"t" help:"Profile tunable key=value (repeatable)."`
}

func (c *NDRCmd) Run(ctx context.Context, cc *Context) error {
	profile, err := loaders.Load(c.Profile, c.Tunables)
	if err != nil {
		return err
	}
	// NDR needs every test stream flow-stat tagged for exact per-pgid counts.
	profile.EnsureFlowStats(1)
	resolved, err := profile.Resolve()
	if err != nil {
		return err
	}
	if err := cc.acquirePorts(c.Ports); err != nil {
		return err
	}
	for _, p := range cc.Ports {
		if err := p.RemoveAllStreams(); err != nil {
			return err
		}
		if err := p.AddStreams(resolved); err != nil {
			return err
		}
	}

	cfg := ndr.DefaultConfig()
	cfg.IterationDuration = c.Duration
	cfg.FirstRunDuration = c.Duration
	cfg.Ports = c.Ports

	engine := ndr.NewEngine(cc.Client, cc.Ports, cfg)
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
