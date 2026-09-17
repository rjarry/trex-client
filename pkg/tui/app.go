// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package tui renders a live alt-screen dashboard of the server ports and
// statistics, with an embedded command line that routes to the same command
// registry as the non-interactive CLI.
package tui

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"go.rockorager.dev/vaxis"

	"github.com/rjarry/trex-client/pkg/cli"
	"github.com/rjarry/trex-client/pkg/session"
)

// App is the terminal UI over a connected client. Live counters come from the
// async stats store (no per-frame RPC); the static port identity is fetched once
// and the dynamic port status (owner/state/link/speed) is refreshed by a
// background poller.
type App struct {
	vx     *vaxis.Vaxis
	client *session.Client
	cctx   *cli.Context

	version session.Version
	ports   []session.PortInfo

	mu     sync.RWMutex
	status map[int]session.PortStatus

	input   string
	console []string // recent command lines and their output

	histIdx int // cursor into the shared command history (== len when not recalling)

	// completion menu state
	menuActive bool
	menuIdx    int
	menuCands  []cli.Candidate
	menuHead   string // input text before the word being completed
	menuOrig   string // input as typed before completion, restored on Escape
}

// Run enters the alt-screen dashboard and blocks until the user quits (Ctrl+C or
// Ctrl+D) or ctx is cancelled. It shares cc (and thus the acquired ports) with
// the command grammar routed through cli.Dispatch.
func Run(ctx context.Context, cc *cli.Context) error {
	vx, err := vaxis.New(vaxis.Options{})
	if err != nil {
		return err
	}
	defer vx.Close()

	app := &App{
		vx:     vx,
		client: cc.Client,
		cctx:   cc,
		status: make(map[int]session.PortStatus),
	}
	if cc.History != nil {
		app.histIdx = cc.History.Len()
	}
	// Route command output to the status line while the dashboard owns the
	// screen, restoring the caller's writer (e.g. the shell's stdout) on exit.
	prevOut := cc.Out
	app.cctx.Out = &lineWriter{app: app}
	defer func() { cc.Out = prevOut }()
	app.loadStatic()
	go app.pollStatus(ctx)

	return app.loop(ctx)
}

// loadStatic fetches the server version and the static per-port identity once at
// startup. Failures are surfaced in the status line rather than fatal.
func (a *App) loadStatic() {
	if v, err := a.client.GetVersion(); err == nil {
		a.version = v
	}
	info, err := a.client.GetSystemInfo()
	if err != nil {
		a.log("error: " + err.Error())
		return
	}
	ports, err := info.PortInfos()
	if err != nil {
		a.log("error: " + err.Error())
		return
	}
	a.ports = ports
}

// pollStatus refreshes the dynamic per-port status until ctx is cancelled.
func (a *App) pollStatus(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		a.refreshStatus()
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (a *App) refreshStatus() {
	for _, pi := range a.ports {
		st, err := a.client.Port(pi.Index).Status()
		if err != nil {
			continue
		}
		a.mu.Lock()
		a.status[pi.Index] = st
		a.mu.Unlock()
	}
}

func (a *App) portStatus(id int) session.PortStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status[id]
}

func (a *App) loop(ctx context.Context) error {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()

	events := a.vx.Events()
	a.draw()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			a.draw()
		case ev := <-events:
			if quit := a.handleEvent(ctx, ev); quit {
				return nil
			}
			a.draw()
		}
	}
}

// handleEvent processes one terminal event and returns true to quit.
func (a *App) handleEvent(ctx context.Context, ev vaxis.Event) bool {
	switch e := ev.(type) {
	case vaxis.Resize:
		// Apply the new terminal size to vaxis' internal buffers before the
		// loop redraws the next frame.
		a.vx.Resize(e)
		return false
	case vaxis.Key:
		switch {
		case e.Matches('c', vaxis.ModCtrl), e.Matches('d', vaxis.ModCtrl):
			return true
		case e.Keycode == vaxis.KeyTab && e.Modifiers&vaxis.ModShift != 0:
			a.complete(false)
		case e.Keycode == vaxis.KeyTab, e.Text == "?":
			// Tab and '?' (Cisco-style) open or advance the completion menu.
			a.complete(true)
		case e.Keycode == vaxis.KeyEsc:
			if a.menuActive {
				a.input = a.menuOrig
			}
			a.closeMenu()
		case a.menuActive && e.Keycode == vaxis.KeyUp:
			a.moveMenu(-1)
		case a.menuActive && e.Keycode == vaxis.KeyDown:
			a.moveMenu(1)
		case e.Keycode == vaxis.KeyUp:
			a.historyRecall(-1)
		case e.Keycode == vaxis.KeyDown:
			a.historyRecall(1)
		case e.Keycode == vaxis.KeyEnter:
			a.closeMenu()
			a.runInput(ctx)
		case e.Keycode == vaxis.KeyBackspace:
			a.closeMenu()
			if len(a.input) > 0 {
				a.input = a.input[:len(a.input)-1]
			}
		case e.Text != "":
			a.closeMenu()
			a.input += e.Text
		}
	}
	return false
}

// complete opens the completion menu for the current input or, when it is
// already open, moves the selection (forward or backward). A single candidate is
// inserted directly without a menu.
func (a *App) complete(forward bool) {
	if a.menuActive {
		if forward {
			a.moveMenu(1)
		} else {
			a.moveMenu(-1)
		}
		return
	}
	cands, prefix := cli.Complete(&cli.Commands{}, a.input)
	if len(cands) == 0 {
		return
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].Value < cands[j].Value })
	a.menuOrig = a.input
	a.menuHead = a.input[:len(a.input)-len(prefix)]
	if len(cands) == 1 {
		a.input = a.menuHead + cands[0].Value + " "
		return
	}
	a.menuCands = cands
	a.menuIdx = 0
	a.menuActive = true
	a.applyMenu()
}

// moveMenu changes the highlighted candidate by delta, wrapping around.
func (a *App) moveMenu(delta int) {
	n := len(a.menuCands)
	if n == 0 {
		return
	}
	a.menuIdx = (a.menuIdx + delta + n) % n
	a.applyMenu()
}

// applyMenu previews the highlighted candidate in the input line.
func (a *App) applyMenu() {
	if a.menuIdx >= 0 && a.menuIdx < len(a.menuCands) {
		a.input = a.menuHead + a.menuCands[a.menuIdx].Value
	}
}

func (a *App) closeMenu() {
	a.menuActive = false
	a.menuCands = nil
}

// consoleMax caps the retained console scrollback.
const consoleMax = 200

// log appends non-empty lines to the console scrollback, capped to consoleMax.
func (a *App) log(s string) {
	a.console = append(a.console, strings.Split(strings.TrimRight(s, "\n"), "\n")...)
	if len(a.console) > consoleMax {
		a.console = a.console[len(a.console)-consoleMax:]
	}
}

// runInput dispatches the current command line to the shared registry, echoing
// the command and any error into the console scrollback and recording it in the
// command history.
func (a *App) runInput(ctx context.Context) {
	line := a.input
	a.input = ""
	if line == "" {
		return
	}
	if a.cctx.History != nil {
		a.cctx.History.Add(line)
		a.histIdx = a.cctx.History.Len()
	}
	a.log("> " + line)
	if err := cli.Dispatch(ctx, a.cctx, line); err != nil {
		a.log("error: " + err.Error())
	}
}

// historyRecall walks the shared command history: delta -1 for older, +1 for
// newer. Stepping past the newest entry restores an empty line.
func (a *App) historyRecall(delta int) {
	if a.cctx.History == nil {
		return
	}
	lines := a.cctx.History.Lines()
	idx := a.histIdx + delta
	switch {
	case idx < 0:
		return
	case idx >= len(lines):
		a.histIdx = len(lines)
		a.input = ""
	default:
		a.histIdx = idx
		a.input = lines[idx]
	}
}

// lineWriter appends command output into the app's console scrollback.
type lineWriter struct{ app *App }

func (w *lineWriter) Write(p []byte) (int, error) {
	w.app.log(string(p))
	return len(p), nil
}
