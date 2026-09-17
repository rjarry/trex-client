// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Command trex is the Go TRex client. Phase 0 exposes the transport spine:
// connect, version/system info, ping, barrier and a live trex-global stats
// watch used as the pure-Go ZMQ interop gate.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/rjarry/trex-client/pkg/session"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() error {
	fmt.Fprintln(os.Stderr, "usage: trex <host> <version|sysinfo|ping|barrier|watch>")
	return fmt.Errorf("missing arguments")
}

func run(args []string) error {
	if len(args) < 2 {
		return usage()
	}
	host, cmd := args[0], args[1]

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c, err := session.Connect(ctx, host, "")
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer c.Close()

	switch cmd {
	case "version":
		v, err := c.GetVersion()
		if err != nil {
			return err
		}
		fmt.Printf("version:    %s\nbuild date: %s %s\nbuilt by:   %s\nmode:       %s\n",
			v.Version, v.BuildDate, v.BuildTime, v.BuiltBy, v.Mode)
	case "sysinfo":
		s, err := c.GetSystemInfo()
		if err != nil {
			return err
		}
		fmt.Printf("hostname:   %s\nuptime:     %s\ndp cores:   %d\ncore type:  %s\nports:      %d\n",
			s.Hostname, s.Uptime, s.DPCoreCount, s.CoreType, s.PortCount)
	case "ping":
		res, err := c.Ping()
		if err != nil {
			return err
		}
		fmt.Printf("ping: %s\n", res)
	case "barrier":
		if err := c.Barrier(5*time.Second, true); err != nil {
			return err
		}
		fmt.Println("barrier: ok")
	case "watch":
		return watch(ctx, c)
	default:
		return usage()
	}
	return nil
}

// watch confirms live async stats flow: it forces a baseline snapshot then
// prints the trex-global payload once a second.
func watch(ctx context.Context, c *session.Client) error {
	if err := c.Barrier(5*time.Second, true); err != nil {
		return err
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			raw, ok := c.Stats().Latest("trex-global")
			if !ok {
				fmt.Println("trex-global: (no data yet)")
				continue
			}
			fmt.Printf("trex-global: %s\n", raw)
		}
	}
}
