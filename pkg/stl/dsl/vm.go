// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package dsl

import (
	"fmt"

	"github.com/rjarry/trex-client/pkg/stl"
)

// buildVM turns the DSL vm list into stl instructions, resolving symbolic
// packet offsets ("IP.src") against the built packet.
func buildVM(entries []dslVMEntry, pkt *stl.Packet) (stl.VM, error) {
	vm := stl.VM{Instructions: []stl.VMInstruction{}}
	for i, e := range entries {
		if len(e) != 1 {
			return vm, fmt.Errorf("vm instruction %d must have exactly one type key", i)
		}
		for kind, f := range e {
			ins, err := buildInstruction(kind, f, pkt)
			if err != nil {
				return vm, fmt.Errorf("vm instruction %d (%s): %w", i, kind, err)
			}
			vm.Instructions = append(vm.Instructions, ins)
		}
	}
	return vm, nil
}

func buildInstruction(kind string, f map[string]any, pkt *stl.Packet) (stl.VMInstruction, error) {
	switch kind {
	case "flow_var":
		return buildFlowVar(f)
	case "write_flow_var":
		off, err := resolveOffset(pkt, f["offset"])
		if err != nil {
			return nil, err
		}
		return stl.NewWriteFlowVar(toStr(f["name"]), off, toI64(f["add_value"]), boolFrom(f, "big_endian", true)), nil
	case "write_mask_flow_var":
		off, err := resolveOffset(pkt, f["offset"])
		if err != nil {
			return nil, err
		}
		return stl.WriteMaskFlowVar{
			Type: "write_mask_flow_var", Name: toStr(f["name"]), PktOffset: off,
			PktCastSize: toInt(f["cast_size"]), Mask: toU64(f["mask"]),
			Shift: toInt(f["shift"]), AddValue: toI64(f["add_value"]),
			IsBigEndian: boolFrom(f, "big_endian", true),
		}, nil
	case "fix_checksum_ipv4":
		off, err := resolveOffset(pkt, f["offset"])
		if err != nil {
			return nil, err
		}
		return stl.NewFixChecksumIPv4(off), nil
	case "fix_checksum_hw":
		return stl.NewFixChecksumHw(toInt(f["l2_len"]), toInt(f["l3_len"]), l4Type(f["l4_type"])), nil
	case "fix_checksum_icmpv6":
		return stl.FixChecksumICMPv6{Type: "fix_checksum_icmpv6", L2Len: toInt(f["l2_len"]), L3Len: toInt(f["l3_len"])}, nil
	case "trim_pkt_size":
		return stl.NewTrimPktSize(toStr(f["name"])), nil
	case "tuple_flow_var":
		return stl.TupleGen{
			Type: "tuple_flow_var", Name: toStr(f["name"]),
			IPMin: toU64Value(f["ip_min"]), IPMax: toU64Value(f["ip_max"]),
			PortMin: toInt(f["port_min"]), PortMax: toInt(f["port_max"]),
			LimitFlows: toInt(f["limit_flows"]), Flags: toInt(f["flags"]),
		}, nil
	case "flow_var_rand_limit":
		return stl.NewFlowVarRandLimit(
			toStr(f["name"]), sizeOr(f, 4), toInt(f["limit"]), toInt(f["seed"]),
			toU64Value(f["min"]), toU64Value(f["max"]), boolFrom(f, "split_to_cores", false),
		), nil
	default:
		return nil, fmt.Errorf("unknown vm instruction type %q", kind)
	}
}

func buildFlowVar(f map[string]any) (stl.VMInstruction, error) {
	name := toStr(f["name"])
	size := sizeOr(f, 4)
	op := toStr(f["op"])
	if op == "" {
		op = stl.OpInc
	}
	step := toInt(f["step"])
	if step == 0 {
		step = 1
	}
	split := boolFrom(f, "split_to_cores", false)

	if vl, ok := f["value_list"]; ok {
		return stl.NewFlowVarList(name, size, op, toU64List(vl), step, split), nil
	}
	min := toU64Value(f["min"])
	max := toU64Value(f["max"])
	init := min
	if v, ok := f["init"]; ok {
		init = toU64Value(v)
	}
	return stl.NewFlowVar(name, size, op, init, min, max, step, split), nil
}

// resolveOffset accepts a numeric offset or a symbolic field reference.
func resolveOffset(pkt *stl.Packet, v any) (int, error) {
	switch x := v.(type) {
	case nil:
		return 0, fmt.Errorf("missing offset")
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case float64:
		return int(x), nil
	case string:
		return pkt.ResolveOffset(x)
	default:
		return 0, fmt.Errorf("invalid offset %v", v)
	}
}

func l4Type(v any) int {
	switch toStr(v) {
	case "udp", "UDP":
		return stl.L4TypeUDP
	case "tcp", "TCP":
		return stl.L4TypeTCP
	case "ip", "IP":
		return stl.L4TypeIP
	default:
		return toInt(v)
	}
}

func sizeOr(f map[string]any, def int) int {
	if v, ok := f["size"]; ok {
		return toInt(v)
	}
	return def
}
