// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package cli

import (
	"os"
	"strings"
)

// lsColors maps LS_COLORS keys ("di", "ex", "*.ext", ...) to SGR sequences,
// parsed once from the environment so file completions can be coloured like ls.
var lsColors = parseLSColors(os.Getenv("LS_COLORS"))

func parseLSColors(env string) map[string]string {
	m := make(map[string]string)
	for _, entry := range strings.Split(env, ":") {
		if key, val, ok := strings.Cut(entry, "="); ok && key != "" {
			m[key] = val
		}
	}
	return m
}

// fileSGR returns the LS_COLORS SGR sequence for a file candidate (directory,
// executable, then extension, then default file), or "" when uncoloured.
func fileSGR(c Candidate) string {
	switch {
	case c.IsDir:
		return lsColors["di"]
	case c.IsExe:
		return lsColors["ex"]
	default:
		name := strings.TrimSuffix(c.Value, "/")
		if dot := strings.LastIndex(name, "."); dot >= 0 {
			if s := lsColors["*"+name[dot:]]; s != "" {
				return s
			}
		}
		return lsColors["fi"]
	}
}
