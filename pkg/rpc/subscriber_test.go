// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package rpc

import "testing"

func TestCheckSeq(t *testing.T) {
	tests := []struct {
		name          string
		expected, got uint64
		wantNext      uint64
		wantOK        bool
	}{
		{"first message wildcard", 0, 5, 6, true},
		{"server reset wildcard", 7, 0, 1, true},
		{"in order", 5, 5, 6, true},
		{"gap resyncs", 5, 9, 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next, ok := checkSeq(tt.expected, tt.got)
			if next != tt.wantNext || ok != tt.wantOK {
				t.Fatalf("checkSeq(%d,%d) = (%d,%v), want (%d,%v)",
					tt.expected, tt.got, next, ok, tt.wantNext, tt.wantOK)
			}
		})
	}
}
