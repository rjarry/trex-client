// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/chzyer/readline"
)

// Shell is an interactive readline REPL over the command grammar, rendered on
// the normal screen (no alt-screen). Tab completes subcommands, flags and file
// arguments against a fresh grammar from NewGrammar; each line is dispatched
// through DispatchGrammar so a command behaves identically to the
// non-interactive CLI. Ctrl-D on an empty line or the "exit"/"quit" builtin
// quits; Ctrl-C abandons the current line without quitting.
type Shell struct {
	// Cc is the shared command state bound into each dispatched command.
	Cc *Context
	// Prompt is printed before each input line.
	Prompt string
	// NewGrammar returns a fresh instance of the top-level command struct. A
	// new one is built per line because kong mutates it during parsing.
	NewGrammar func() any
}

// Run reads and dispatches commands until Ctrl-D or an exit request.
func (s *Shell) Run(ctx context.Context) error {
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          s.Prompt,
		AutoComplete:    &grammarCompleter{newGrammar: s.NewGrammar},
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return err
	}
	defer rl.Close()

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
		if err := DispatchGrammar(ctx, s.Cc, s.NewGrammar(), line); err != nil {
			fmt.Fprintln(s.Cc.Out, "error:", err)
		}
	}
}

// grammarCompleter adapts the grammar completion to readline's AutoCompleter: it
// returns, for each candidate, the suffix to insert after the current word (a
// space when the word is already complete) plus the length of that word.
type grammarCompleter struct {
	newGrammar func() any
}

func (c *grammarCompleter) Do(line []rune, pos int) ([][]rune, int) {
	candidates, prefix := Complete(c.newGrammar(), string(line[:pos]))
	out := make([][]rune, 0, len(candidates))
	for _, cand := range candidates {
		if !strings.HasPrefix(cand.Value, prefix) {
			continue
		}
		suffix := cand.Value[len(prefix):]
		if suffix == "" {
			suffix = " "
		}
		out = append(out, []rune(suffix))
	}
	return out, len([]rune(prefix))
}
