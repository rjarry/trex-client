// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package stl builds stateless traffic profiles and serializes them to the wire
// objects the TRex server accepts (add_stream). It mirrors the behavior of the
// reference Python control plane without reusing its code.
package stl

import (
	"fmt"
	"strconv"
	"strings"
)

// Rate is a stream's transmit rate. Type is one of the server rate kinds and
// Value is interpreted accordingly.
type Rate struct {
	Type  string  `json:"type"`
	Value float64 `json:"value"`
}

// Rate type constants accepted by the server.
const (
	RatePPS        = "pps"
	RateBPSL1      = "bps_L1"
	RateBPSL2      = "bps_L2"
	RatePercentage = "percentage"
)

// ParseRate parses a human rate string into a Rate. Supported forms:
//
//	"100%"            -> percentage
//	"10kpps"/"1mpps"  -> pps (with k/m/g SI multipliers)
//	"1gbps"/"1mbps"   -> bps_L1 (line rate, includes framing overhead)
//	"1gbpsl2"         -> bps_L2 (L2 payload rate)
//	"1000"            -> pps (bare number)
func ParseRate(s string) (Rate, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return Rate{}, fmt.Errorf("empty rate")
	}

	if strings.HasSuffix(s, "%") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err != nil {
			return Rate{}, fmt.Errorf("invalid percentage rate %q: %w", s, err)
		}
		return Rate{Type: RatePercentage, Value: v}, nil
	}

	// Order matters: match the longest suffix first.
	suffixes := []struct {
		suffix string
		typ    string
	}{
		{"bpsl2", RateBPSL2},
		{"bps", RateBPSL1},
		{"pps", RatePPS},
	}
	for _, sfx := range suffixes {
		if strings.HasSuffix(s, sfx.suffix) {
			num := strings.TrimSuffix(s, sfx.suffix)
			v, err := parseSINumber(num)
			if err != nil {
				return Rate{}, fmt.Errorf("invalid rate %q: %w", s, err)
			}
			return Rate{Type: sfx.typ, Value: v}, nil
		}
	}

	// Bare number: packets per second.
	v, err := parseSINumber(s)
	if err != nil {
		return Rate{}, fmt.Errorf("invalid rate %q: %w", s, err)
	}
	return Rate{Type: RatePPS, Value: v}, nil
}

// parseSINumber parses a number with an optional k/m/g SI multiplier suffix.
func parseSINumber(s string) (float64, error) {
	mult := 1.0
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'k':
			mult = 1e3
			s = s[:len(s)-1]
		case 'm':
			mult = 1e6
			s = s[:len(s)-1]
		case 'g':
			mult = 1e9
			s = s[:len(s)-1]
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return v * mult, nil
}
