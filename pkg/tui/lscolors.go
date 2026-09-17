// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package tui

import (
	"os"
	"strconv"
	"strings"

	"go.rockorager.dev/vaxis"

	"github.com/rjarry/trex-client/pkg/cli"
)

// lsColors maps LS_COLORS keys ("di", "ex", "*.ext", ...) to SGR sequences,
// parsed once from the environment so file completions can be coloured like ls.
var lsColors = parseLSColors(os.Getenv("LS_COLORS"))

func parseLSColors(env string) map[string]string {
	m := make(map[string]string)
	for _, entry := range strings.Split(env, ":") {
		key, val, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			m[key] = val
		}
	}
	return m
}

// fileStyle returns the vaxis style for a file completion candidate, honouring
// LS_COLORS (directory, executable, then extension, then default file).
func fileStyle(c cli.Candidate) vaxis.Style {
	var sgr string
	switch {
	case c.IsDir:
		sgr = lsColors["di"]
	case c.IsExe:
		sgr = lsColors["ex"]
	default:
		name := strings.TrimSuffix(c.Value, "/")
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			sgr = lsColors["*"+name[dot:]]
		}
		if sgr == "" {
			sgr = lsColors["fi"]
		}
	}
	return sgrStyle(sgr)
}

// sgrStyle converts an SGR parameter string (e.g. "01;34") into a vaxis style.
// It supports bold, underline and the 16 basic foreground colours.
func sgrStyle(sgr string) vaxis.Style {
	style := vaxis.Style{}
	if sgr == "" {
		return style
	}
	for _, part := range strings.Split(sgr, ";") {
		n, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		switch {
		case n == 1:
			style.Attribute |= vaxis.AttrBold
		case n >= 30 && n <= 37:
			style.Foreground = vaxis.IndexColor(uint8(n - 30))
		case n >= 90 && n <= 97:
			style.Foreground = vaxis.IndexColor(uint8(n - 90 + 8))
		}
	}
	return style
}
