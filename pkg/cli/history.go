// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package cli

import (
	"bufio"
	"os"
	"strings"
	"sync"
)

// History is a command history shared by the interactive shell and the TUI
// command line and persisted to a file. It implements the reeflective/readline
// history Source interface (Write/GetLine/Len/Dump) so the shell records into it
// directly, while the TUI uses Add/Lines for its own up/down recall.
type History struct {
	mu    sync.Mutex
	path  string
	lines []string
}

// NewHistory loads the history file at path (missing file is fine) and returns a
// store that appends new entries back to it.
func NewHistory(path string) *History {
	h := &History{path: path}
	f, err := os.Open(path)
	if err != nil {
		return h
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		if line := scan.Text(); line != "" {
			h.lines = append(h.lines, line)
		}
	}
	return h
}

// Add records a command line (ignoring blanks and consecutive duplicates) and
// appends it to the history file.
func (h *History) Add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.appendLocked(line)
}

func (h *History) appendLocked(line string) {
	// Don't record the shell's own exit builtins.
	if line == "exit" || line == "quit" {
		return
	}
	if n := len(h.lines); n > 0 && h.lines[n-1] == line {
		return
	}
	h.lines = append(h.lines, line)
	if h.path == "" {
		return
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.WriteString(line + "\n")
	_ = f.Close()
}

// Lines returns a copy of the history, oldest first.
func (h *History) Lines() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.lines...)
}

// Write implements readline history Source: it records an accepted line.
func (h *History) Write(line string) (int, error) {
	line = strings.TrimSpace(line)
	h.mu.Lock()
	defer h.mu.Unlock()
	if line != "" {
		h.appendLocked(line)
	}
	return len(h.lines), nil
}

// GetLine implements readline history Source: line at pos (0 == oldest).
func (h *History) GetLine(pos int) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if pos < 0 || pos >= len(h.lines) {
		return "", os.ErrInvalid
	}
	return h.lines[pos], nil
}

// Len implements readline history Source.
func (h *History) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.lines)
}

// Dump implements readline history Source.
func (h *History) Dump() any {
	return h.Lines()
}
