// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package loaders

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rjarry/trex-client/pkg/stl"
	"github.com/rjarry/trex-client/pkg/stl/dsl"
)

// Load reads a profile from a file, dispatching by extension. YAML/JSON files
// are further distinguished between the declarative DSL (packets described as
// layers) and the frozen snapshot format (base64 packet bytes). tunables apply
// only to the DSL path.
func Load(path string, tunables map[string]string) (stl.Profile, error) {
	switch filepath.Ext(path) {
	case ".pcap", ".cap":
		return LoadPcap(path, PcapOptions{})
	case ".yaml", ".yml", ".json":
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if isSnapshot(raw) {
			return LoadSnapshot(raw)
		}
		return dsl.Parse(raw, tunables)
	default:
		return nil, fmt.Errorf("unknown profile type %q", filepath.Ext(path))
	}
}

// isSnapshot heuristically detects the frozen format: it carries base64 packet
// bytes ("binary") and never describes packets as "layers".
func isSnapshot(raw []byte) bool {
	return bytes.Contains(raw, []byte("binary")) && !bytes.Contains(raw, []byte("layers"))
}
