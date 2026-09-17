// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package cli holds the kong command grammar consumed by both the
// non-interactive CLI and the TUI's embedded command line, so a command is
// defined once and behaves identically in either front-end.
package cli

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/rjarry/trex-client/pkg/session"
)

// Context is the shared state a command operates on. It is bound into the kong
// grammar and passed to each command's Run method. Ports persist across
// dispatches so, e.g., start reuses the ports acquired by streams.
type Context struct {
	Client *session.Client
	Ports  []*session.Port
	Out    io.Writer
}

// Parse builds the kong application for the given grammar, handles bash
// completion (exiting the process when invoked as a completer), then parses the
// process arguments. It is used by the non-interactive CLI.
func Parse(grammar any, description string) *kong.Context {
	app, err := kong.New(grammar, kong.Description(description))
	if err != nil {
		panic(err)
	}
	if bashComplete(app) {
		os.Exit(0)
	}
	kctx, err := app.Parse(os.Args[1:])
	app.FatalIfErrorf(err)
	return kctx
}

// Dispatch parses and runs a single command line against the shared traffic
// commands, binding c so the command operates on the current state. Empty lines
// are a no-op. Used by the TUI command input.
func Dispatch(ctx context.Context, c *Context, line string) error {
	return DispatchGrammar(ctx, c, &Commands{}, line)
}

// DispatchGrammar parses and runs a single command line against grammar, a fresh
// instance of a top-level command struct, binding ctx and c so the command
// operates on the current state. Empty lines are a no-op. It is used by the
// interactive shell, which runs the full command set rather than the traffic
// subset. kong's own usage/error text is discarded; callers report the returned
// error themselves.
func DispatchGrammar(ctx context.Context, c *Context, grammar any, line string) error {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	app, err := kong.New(grammar,
		kong.Writers(io.Discard, io.Discard),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		return err
	}
	kctx, err := app.Parse(fields)
	if err != nil {
		return err
	}
	kctx.BindTo(ctx, (*context.Context)(nil))
	kctx.Bind(c)
	return kctx.Run()
}
