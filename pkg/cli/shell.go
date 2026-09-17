// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/reeflective/readline"
	"github.com/reeflective/readline/inputrc"
)

// Shell is an interactive readline REPL over the command grammar, rendered on
// the normal screen (no alt-screen). Tab opens a completion menu of subcommands,
// flags and file arguments (grouped, described and coloured like ls), built from
// a fresh grammar via NewGrammar; each line is dispatched through DispatchGrammar
// so a command behaves identically to the non-interactive CLI. Ctrl-D on an
// empty line or the "exit"/"quit" builtin quits; Ctrl-C abandons the line.
type Shell struct {
	// Cc is the shared command state bound into each dispatched command.
	Cc *Context
	// Prompt is printed before each input line (may contain ANSI colour).
	Prompt string
	// NewGrammar returns a fresh instance of the top-level command struct. A
	// new one is built per line because kong mutates it during parsing.
	NewGrammar func() any
}

// Run reads and dispatches commands until Ctrl-D or an exit request.
func (s *Shell) Run(ctx context.Context) error {
	rl := readline.NewShell()
	if s.Cc.History != nil {
		rl.History.Add("trexc", s.Cc.History)
	}
	rl.Prompt.Primary(func() string { return s.Prompt })
	rl.Completer = s.complete
	_ = rl.Config.Set("cursor-style", "default")
	// First Tab shows the completion menu (with descriptions) instead of
	// immediately inserting the first match.
	_ = rl.Config.Set("menu-complete-display-prefix", true)
	// '?' also opens the context completion menu.
	rl.Config.Binds["emacs"]["?"] = inputrc.Bind{Action: "possible-completions"}

	for {
		line, err := rl.Readline()
		switch {
		case errors.Is(err, readline.ErrInterrupt):
			continue // Ctrl-C: abandon the line, keep the shell
		case errors.Is(err, io.EOF):
			return nil // Ctrl-D on an empty line
		case err != nil:
			return err
		}
		switch strings.TrimSpace(line) {
		case "":
			continue
		case "exit", "quit":
			return nil
		}
		if err := s.dispatch(ctx, line); err != nil {
			fmt.Fprintln(s.Cc.Out, "error:", err)
		}
	}
}

// dispatch runs one command line with its own context that is cancelled on
// Ctrl-C (SIGINT), so an interrupt stops the running command (e.g. NDR traffic)
// without tearing down the shell. At the prompt the terminal is in raw mode, so
// Ctrl-C there is a keystroke (abandon line), not a signal.
func (s *Shell) dispatch(ctx context.Context, line string) error {
	cmdCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-cmdCtx.Done():
		}
	}()

	return DispatchGrammar(cmdCtx, s.Cc, s.NewGrammar(), line)
}

// complete produces grouped, described and (for files) coloured completions for
// the word under the cursor, from the shared grammar completion engine.
func (s *Shell) complete(line []rune, cursor int) readline.Completions {
	cands, _ := Complete(s.NewGrammar(), string(line[:cursor]))
	comps := make([]readline.Completion, 0, len(cands))
	for _, c := range cands {
		comps = append(comps, readline.Completion{
			Value:       c.Value,
			Description: c.Help,
			Tag:         tagLabel(c.Tag),
			Style:       fileSGR(c),
		})
	}
	// List described groups one per row (so descriptions show); files stay in a
	// coloured grid.
	return readline.CompleteRaw(comps).DisplayList("commands", "flags", "values")
}

// tagLabel turns a candidate tag into a plural group heading for the menu.
func tagLabel(tag string) string {
	switch tag {
	case "command":
		return "commands"
	case "flag":
		return "flags"
	case "value":
		return "values"
	case "file":
		return "files"
	default:
		return tag
	}
}
