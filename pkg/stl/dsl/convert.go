// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package dsl

import (
	"encoding/binary"
	"net"
	"strconv"
	"strings"
)

// The YAML decoder yields int/float64/string; these helpers coerce them for the
// typed stl model. Missing keys arrive as nil and coerce to the zero value.

func toStr(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		return strconv.FormatInt(toI64(v), 10)
	}
}

func toInt(v any) int { return int(toI64(v)) }

func toI64(v any) int64 {
	switch x := v.(type) {
	case nil:
		return 0
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(x, 0, 64)
		return n
	default:
		return 0
	}
}

func toU64(v any) uint64 {
	if s, ok := v.(string); ok {
		n, _ := strconv.ParseUint(s, 0, 64)
		return n
	}
	return uint64(toI64(v))
}

// toU64Value coerces a value that may be an integer or a dotted IPv4 address
// (as used for IP-range flow variables) into a uint64.
func toU64Value(v any) uint64 {
	if s, ok := v.(string); ok && strings.Contains(s, ".") {
		if ip := net.ParseIP(s).To4(); ip != nil {
			return uint64(binary.BigEndian.Uint32(ip))
		}
	}
	return toU64(v)
}

func toU64List(v any) []uint64 {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]uint64, len(items))
	for i, it := range items {
		out[i] = toU64Value(it)
	}
	return out
}

func boolFrom(f map[string]any, key string, def bool) bool {
	v, ok := f[key]
	if !ok {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}
