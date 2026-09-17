// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package tui renders a live alt-screen dashboard of the server ports and
// statistics, with an embedded command line that routes to the same command
// registry as the non-interactive CLI.
package tui

import (
	"context"
	"sort"
	"sync"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/list"

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
	message string

	// completion menu state
	menu       list.List
	menuActive bool
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
		a.message = "error: " + err.Error()
		return
	}
	ports, err := info.PortInfos()
	if err != nil {
		a.message = "error: " + err.Error()
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
		return false
	case vaxis.Key:
		switch {
		case e.Matches('c', vaxis.ModCtrl), e.Matches('d', vaxis.ModCtrl):
			return true
		case e.Keycode == vaxis.KeyTab && e.Modifiers&vaxis.ModShift != 0:
			a.complete(false)
		case e.Keycode == vaxis.KeyTab:
			a.complete(true)
		case e.Keycode == vaxis.KeyEsc:
			if a.menuActive {
				a.input = a.menuOrig
			}
			a.closeMenu()
		case a.menuActive && e.Keycode == vaxis.KeyUp:
			a.menu.Up()
			a.applyMenu()
		case a.menuActive && e.Keycode == vaxis.KeyDown:
			a.menu.Down()
			a.applyMenu()
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
			a.menu.Down()
		} else {
			a.menu.Up()
		}
		a.applyMenu()
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
	items := make([]string, len(cands))
	for i, c := range cands {
		items[i] = c.Value
	}
	a.menu.SetItems(items)
	a.menu.Home()
	a.menuActive = true
	a.applyMenu()
}

// applyMenu previews the highlighted candidate in the input line.
func (a *App) applyMenu() {
	i := a.menu.Index()
	if i >= 0 && i < len(a.menuCands) {
		a.input = a.menuHead + a.menuCands[i].Value
	}
}

func (a *App) closeMenu() {
	a.menuActive = false
	a.menuCands = nil
}

// runInput dispatches the current command line to the shared registry.
func (a *App) runInput(ctx context.Context) {
	line := a.input
	a.input = ""
	if line == "" {
		return
	}
	if err := cli.Dispatch(ctx, a.cctx, line); err != nil {
		a.message = "error: " + err.Error()
	}
}

// lineWriter captures command output into the app's status message.
type lineWriter struct{ app *App }

func (w *lineWriter) Write(p []byte) (int, error) {
	w.app.message = string(p)
	return len(p), nil
}
