// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Command trexc is the Go TRex client: a single static binary that drives the
// TRex server over its JSON-RPC/ZMQ API. It runs one-shot commands, an NDR
// search, an interactive shell (when invoked with no command) or an alt-screen
// TUI, all over the shared command grammar. Bash completion is served by the
// binary itself (complete -C trexc).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/rjarry/trex-client/pkg/cli"
	"github.com/rjarry/trex-client/pkg/session"
	"github.com/rjarry/trex-client/pkg/tui"
)

// CLI is the top-level kong grammar. It embeds the traffic commands shared with
// the TUI and adds the client-only commands and the global server flag.
type CLI struct {
	Host string `short:"s" default:"127.0.0.1" help:"TRex server host."`

	Version versionCmd `cmd:"" help:"Print the server version."`
	Sysinfo sysinfoCmd `cmd:"" help:"Print server system information."`
	Ping    pingCmd    `cmd:"" help:"Ping the server."`
	Tui     tuiCmd     `cmd:"" help:"Run the interactive dashboard."`
	Shell   shellCmd   `cmd:"" default:"1" hidden:"" help:"Run the interactive shell (default with no command)."`

	cli.Commands
}

type versionCmd struct{}

func (c *versionCmd) Run(cc *cli.Context) error {
	v, err := cc.Client.GetVersion()
	if err != nil {
		return err
	}
	fmt.Fprintf(cc.Out, "version: %s  build: %s %s  by: %s  mode: %s\n",
		v.Version, v.BuildDate, v.BuildTime, v.BuiltBy, v.Mode)
	return nil
}

type sysinfoCmd struct{}

func (c *sysinfoCmd) Run(cc *cli.Context) error {
	s, err := cc.Client.GetSystemInfo()
	if err != nil {
		return err
	}
	fmt.Fprintf(cc.Out, "host: %s  uptime: %s  dp cores: %d  core: %s  ports: %d\n",
		s.Hostname, s.Uptime, s.DPCoreCount, s.CoreType, s.PortCount)
	return nil
}

type pingCmd struct{}

func (c *pingCmd) Run(cc *cli.Context) error {
	res, err := cc.Client.Ping()
	if err != nil {
		return err
	}
	fmt.Fprintf(cc.Out, "ping: %s\n", res)
	return nil
}

type tuiCmd struct{}

func (c *tuiCmd) Run(ctx context.Context, cc *cli.Context) error {
	return tui.Run(ctx, cc)
}

// shellRunning guards against re-entering the interactive shell: a flags-only
// line typed at the prompt would otherwise resolve to this default command and
// start a nested shell.
var shellRunning bool

type shellCmd struct{}

func (c *shellCmd) Run(ctx context.Context, cc *cli.Context) error {
	if shellRunning {
		return nil
	}
	shellRunning = true
	defer func() { shellRunning = false }()
	sh := &cli.Shell{
		Cc:         cc,
		Prompt:     "trexc> ",
		NewGrammar: func() any { return &CLI{} },
	}
	return sh.Run(ctx)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var app CLI
	kctx := cli.Parse(&app, "Go TRex client driving a TRex server over JSON-RPC/ZMQ.")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c, err := session.Connect(ctx, app.Host, "")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close()

	kctx.BindTo(ctx, (*context.Context)(nil))
	kctx.Bind(&cli.Context{Client: c, Out: os.Stdout})
	return kctx.Run()
}
