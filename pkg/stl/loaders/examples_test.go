// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package loaders

import (
	"path/filepath"
	"testing"
)

// TestExampleProfiles ensures the shipped example DSL profiles load and resolve
// to valid wire streams, so they stay working documentation.
func TestExampleProfiles(t *testing.T) {
	matches, err := filepath.Glob("../../../examples/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	ndr, err := filepath.Glob("../../../examples/ndr/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	matches = append(matches, ndr...)
	if len(matches) == 0 {
		t.Fatal("no example profiles found")
	}
	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			prof, err := Load(path, map[string]string{"size": "128"})
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(prof) == 0 {
				t.Fatal("profile has no streams")
			}
			if _, err := prof.Resolve(); err != nil {
				t.Fatalf("resolve: %v", err)
			}
		})
	}
}
