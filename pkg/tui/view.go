// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package tui

import (
	"fmt"
	"math"
	"strings"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/border"

	"github.com/rjarry/trex-client/pkg/session"
	"github.com/rjarry/trex-client/pkg/stats"
)

// index color palette.
const (
	colRed     uint8 = 1
	colGreen   uint8 = 2
	colYellow  uint8 = 3
	colMagenta uint8 = 5
	colGrey    uint8 = 8
)

var (
	styleBold   = vaxis.Style{Attribute: vaxis.AttrBold}
	styleDim    = vaxis.Style{Foreground: vaxis.IndexColor(colGrey)}
	styleLabel  = vaxis.Style{Attribute: vaxis.AttrDim}
	styleNormal = vaxis.Style{}
)

func fg(c uint8) vaxis.Style { return vaxis.Style{Foreground: vaxis.IndexColor(c)} }

// draw renders the whole screen: a header, the global statistics panel, the
// per-port panel, then the status message and command input line.
func (a *App) draw() {
	win := a.vx.Window()
	win.Clear()
	w, h := win.Size()

	win.Println(0, seg(fmt.Sprintf(" TRex client   %s @ %s   host %s",
		orDash(a.version.Mode), orDash(a.version.Version), a.client.Host()), styleBold))

	const globalH = 8
	a.drawGlobal(win.New(0, 1, w, globalH))

	py := 1 + globalH
	if ph := h - py - 2; ph > 3 {
		a.drawPorts(win.New(0, py, w, ph))
	}

	if a.message != "" {
		win.Println(h-2, seg("• "+strings.TrimRight(a.message, "\n"), fg(colYellow)))
	}
	win.Println(h-1, seg("> "+a.input, styleNormal))
	win.ShowCursor(2+len([]rune(a.input)), h-1, vaxis.CursorDefault)
	a.drawMenu(win)
	a.vx.Render()
}

// drawMenu renders the completion menu as a bordered popup whose bottom sits just
// above the input line. It is drawn last so it overlays the panels.
func (a *App) drawMenu(win vaxis.Window) {
	if !a.menuActive || len(a.menuCands) == 0 {
		return
	}
	w, h := win.Size()

	maxH := h - 4
	if maxH < 1 {
		return
	}
	innerH := min(len(a.menuCands), min(maxH, 10))

	innerW := 0
	for _, c := range a.menuCands {
		if len(c.Value) > innerW {
			innerW = len(c.Value)
		}
	}
	if innerW > w-4 {
		innerW = w - 4
	}

	boxW, boxH := innerW+2, innerH+2
	top := h - 1 - boxH
	box := win.New(0, top, boxW, boxH)
	box.Fill(vaxis.Cell{Character: vaxis.Character{Grapheme: " ", Width: 1}})
	a.menu.Draw(border.All(box, styleDim))
}

type kv struct {
	key   string
	value string
	style vaxis.Style
}

// drawGlobal renders the global statistics in two key/value columns.
func (a *App) drawGlobal(panel vaxis.Window) {
	inner := border.All(panel, styleDim)
	title(panel, " global statistics ")
	st := a.client.Stats()

	left := []kv{
		{key: "cpu util", value: pct(gv(st, "m_cpu_util"))},
		{key: "rx cpu util", value: pct(gv(st, "m_rx_cpu_util"))},
		{key: "rx core", value: pps(gv(st, "m_rx_core_pps"))},
		{key: "bw per core", value: bps(gv(st, "m_bw_per_core"))},
		{key: "active flows", value: human(gv(st, "m_active_flows"))},
		{key: "open flows", value: human(gv(st, "m_open_flows"))},
	}

	dropRate := gv(st, "m_rx_drop_bps")
	queueFull := gv(st, "m_total_queue_full")
	right := []kv{
		{key: "total tx", value: bps(gv(st, "m_tx_bps"))},
		{key: "total rx", value: bps(gv(st, "m_rx_bps"))},
		{key: "total tx pps", value: pps(gv(st, "m_tx_pps"))},
		{key: "total rx pps", value: pps(gv(st, "m_rx_pps"))},
		{key: "drop rate", value: bps(dropRate), style: warnIf(dropRate > 0)},
		{key: "queue full", value: pkts(queueFull), style: warnIf(queueFull > 0)},
	}

	w, _ := inner.Size()
	half := w / 2
	drawKVs(inner, 0, half, left)
	drawKVs(inner.New(half, 0, w-half, -1), 0, w-half, right)
}

// drawKVs prints a column of aligned key/value pairs starting at column col.
func drawKVs(win vaxis.Window, col, width int, rows []kv) {
	keyW := 0
	for _, r := range rows {
		if len(r.key) > keyW {
			keyW = len(r.key)
		}
	}
	for i, r := range rows {
		printAt(win, col, i, styleLabel, fmt.Sprintf("%-*s :", keyW, r.key))
		style := r.style
		printAt(win, col+keyW+2, i, style, r.value)
	}
}

// portRow is one labelled row of the ports table. When cell is nil the row is a
// section separator (blank).
type portRow struct {
	label string
	cell  func(a *App, st *stats.Store, pi session.PortInfo) (string, vaxis.Style)
}

var portRows = []portRow{
	{label: "owner", cell: func(a *App, _ *stats.Store, pi session.PortInfo) (string, vaxis.Style) {
		return orDash(a.portStatus(pi.Index).Owner), styleNormal
	}},
	{label: "driver", cell: func(_ *App, _ *stats.Store, pi session.PortInfo) (string, vaxis.Style) {
		return pi.Driver, styleNormal
	}},
	{label: "numa", cell: func(_ *App, _ *stats.Store, pi session.PortInfo) (string, vaxis.Style) {
		return fmt.Sprintf("%d", pi.Numa), styleNormal
	}},
	{label: "link", cell: func(a *App, _ *stats.Store, pi session.PortInfo) (string, vaxis.Style) {
		if a.portStatus(pi.Index).LinkUp {
			return "UP", fg(colGreen)
		}
		return "DOWN", fg(colRed)
	}},
	{label: "speed", cell: func(a *App, _ *stats.Store, pi session.PortInfo) (string, vaxis.Style) {
		return fmt.Sprintf("%g Gb/s", a.portStatus(pi.Index).Speed), styleNormal
	}},
	{label: "state", cell: func(a *App, _ *stats.Store, pi session.PortInfo) (string, vaxis.Style) {
		s := orDash(a.portStatus(pi.Index).State)
		return s, stateStyle(s)
	}},
	{label: ""},
	{label: "Tx bps", cell: portCell("m_total_tx_bps", bps)},
	{label: "Tx pps", cell: portCell("m_total_tx_pps", pps)},
	{label: "Rx bps", cell: portCell("m_total_rx_bps", bps)},
	{label: "Rx pps", cell: portCell("m_total_rx_pps", pps)},
	{label: "CPU util", cell: portCell("m_cpu_util", pct)},
	{label: ""},
	{label: "opackets", cell: portCell("opackets", pkts)},
	{label: "ipackets", cell: portCell("ipackets", pkts)},
	{label: "obytes", cell: portCell("obytes", bytesH)},
	{label: "ibytes", cell: portCell("ibytes", bytesH)},
	{label: "oerrors", cell: portErr("oerrors")},
	{label: "ierrors", cell: portErr("ierrors")},
}

// drawPorts renders the per-port table: a label column plus one column per port.
func (a *App) drawPorts(panel vaxis.Window) {
	inner := border.All(panel, styleDim)
	title(panel, " ports ")
	st := a.client.Stats()

	const labelW, colW = 12, 18
	for i, pi := range a.ports {
		printAt(inner, labelW+i*colW, 0, styleBold, fmt.Sprintf("port %d", pi.Index))
	}
	for r, row := range portRows {
		y := r + 1
		printAt(inner, 0, y, styleLabel, row.label)
		if row.cell == nil {
			continue
		}
		for i, pi := range a.ports {
			text, style := row.cell(a, st, pi)
			printAt(inner, labelW+i*colW, y, style, text)
		}
	}
}

// portCell builds a table cell reading per-port counter key formatted by f.
func portCell(key string, f func(float64) string) func(*App, *stats.Store, session.PortInfo) (string, vaxis.Style) {
	return func(_ *App, st *stats.Store, pi session.PortInfo) (string, vaxis.Style) {
		v, _ := st.PortCounter(key, pi.Index)
		return f(v), styleNormal
	}
}

// portErr builds an error-counter cell, coloured red when non-zero.
func portErr(key string) func(*App, *stats.Store, session.PortInfo) (string, vaxis.Style) {
	return func(_ *App, st *stats.Store, pi session.PortInfo) (string, vaxis.Style) {
		v, _ := st.PortCounter(key, pi.Index)
		return pkts(v), warnIf(v > 0)
	}
}

func stateStyle(state string) vaxis.Style {
	switch state {
	case "TX", "PCAP_TX":
		return fg(colGreen)
	case "PAUSE":
		return fg(colMagenta)
	case "DOWN":
		return fg(colRed)
	default:
		return styleNormal
	}
}

func warnIf(bad bool) vaxis.Style {
	if bad {
		return fg(colRed)
	}
	return fg(colGreen)
}

// gv reads a global counter, defaulting to 0 when absent.
func gv(st *stats.Store, key string) float64 {
	v, _ := st.GlobalCounter(key)
	return v
}

// printAt prints text at (col, row) within win without disturbing other cells.
func printAt(win vaxis.Window, col, row int, style vaxis.Style, text string) {
	win.New(col, row, -1, 1).Println(0, seg(text, style))
}

// title writes a panel title over its top border.
func title(panel vaxis.Window, text string) {
	panel.New(2, 0, -1, 1).Println(0, seg(text, styleBold))
}

func seg(text string, style vaxis.Style) vaxis.Segment {
	return vaxis.Segment{Text: text, Style: style}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// human formats a value with an SI magnitude suffix and a trailing space.
func human(v float64) string {
	abs := math.Abs(v)
	switch {
	case abs >= 1e9:
		return fmt.Sprintf("%.2f G", v/1e9)
	case abs >= 1e6:
		return fmt.Sprintf("%.2f M", v/1e6)
	case abs >= 1e3:
		return fmt.Sprintf("%.2f K", v/1e3)
	default:
		return fmt.Sprintf("%.0f ", v)
	}
}

func bps(v float64) string    { return human(v) + "bps" }
func pps(v float64) string    { return human(v) + "pps" }
func pkts(v float64) string   { return human(v) + "pkts" }
func bytesH(v float64) string { return human(v) + "B" }
func pct(v float64) string    { return fmt.Sprintf("%.1f %%", v) }
