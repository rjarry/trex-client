// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package stl

import "encoding/json"

// VMInstruction is one field-engine instruction. Concrete types marshal to the
// exact JSON the server expects; each carries its own "type" tag.
type VMInstruction interface {
	instruction()
}

// VM is the field-engine program attached to a stream's packet.
type VM struct {
	Instructions []VMInstruction `json:"instructions"`
	Cache        *int            `json:"cache,omitempty"`
}

// RawInstruction carries an already-serialized instruction object verbatim. It
// lets the snapshot loader pass through a frozen VM without re-deriving it.
type RawInstruction json.RawMessage

func (RawInstruction) instruction() {}

// MarshalJSON emits the raw instruction bytes unchanged.
func (r RawInstruction) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	return r, nil
}

// L4 checksum types for fix_checksum_hw.
const (
	L4TypeUDP = 11
	L4TypeTCP = 13
	L4TypeIP  = 17
)

// FlowVar is the "flow_var" instruction: a counter/random variable. Use
// NewFlowVar for a min/max range variable or NewFlowVarList for an explicit
// value list.
type FlowVar struct {
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Size         int      `json:"size"`
	Op           string   `json:"op"`
	Step         int      `json:"step"`
	SplitToCores bool     `json:"split_to_cores"`
	NextVar      string   `json:"next_var,omitempty"`
	InitValue    *uint64  `json:"init_value,omitempty"`
	MinValue     *uint64  `json:"min_value,omitempty"`
	MaxValue     *uint64  `json:"max_value,omitempty"`
	ValueList    []uint64 `json:"value_list,omitempty"`
}

func (FlowVar) instruction() {}

// FlowVar op constants.
const (
	OpInc    = "inc"
	OpDec    = "dec"
	OpRandom = "random"
)

// NewFlowVar builds a range flow variable. step must be > 0 and size one of
// 1,2,4,8.
func NewFlowVar(name string, size int, op string, init, min, max uint64, step int, splitToCores bool) FlowVar {
	return FlowVar{
		Type:         "flow_var",
		Name:         name,
		Size:         size,
		Op:           op,
		Step:         step,
		SplitToCores: splitToCores,
		InitValue:    &init,
		MinValue:     &min,
		MaxValue:     &max,
	}
}

// NewFlowVarList builds a flow variable that cycles through an explicit list.
func NewFlowVarList(name string, size int, op string, values []uint64, step int, splitToCores bool) FlowVar {
	return FlowVar{
		Type:         "flow_var",
		Name:         name,
		Size:         size,
		Op:           op,
		Step:         step,
		SplitToCores: splitToCores,
		ValueList:    values,
	}
}

// FlowVarRandLimit is "flow_var_rand_limit": a repeatable random variable with
// a bounded number of distinct values.
type FlowVarRandLimit struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Size         int    `json:"size"`
	Limit        int    `json:"limit"`
	Seed         int    `json:"seed"`
	MinValue     uint64 `json:"min_value"`
	MaxValue     uint64 `json:"max_value"`
	SplitToCores bool   `json:"split_to_cores"`
	NextVar      string `json:"next_var,omitempty"`
}

func (FlowVarRandLimit) instruction() {}

// NewFlowVarRandLimit builds a repeatable random flow variable.
func NewFlowVarRandLimit(name string, size, limit, seed int, min, max uint64, splitToCores bool) FlowVarRandLimit {
	return FlowVarRandLimit{
		Type:         "flow_var_rand_limit",
		Name:         name,
		Size:         size,
		Limit:        limit,
		Seed:         seed,
		MinValue:     min,
		MaxValue:     max,
		SplitToCores: splitToCores,
	}
}

// WriteFlowVar is "write_flow_var": write a variable into the packet at an offset.
type WriteFlowVar struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	PktOffset   int    `json:"pkt_offset"`
	AddValue    int64  `json:"add_value"`
	IsBigEndian bool   `json:"is_big_endian"`
}

func (WriteFlowVar) instruction() {}

// NewWriteFlowVar writes variable name at pkt_offset.
func NewWriteFlowVar(name string, pktOffset int, addValue int64, bigEndian bool) WriteFlowVar {
	return WriteFlowVar{
		Type:        "write_flow_var",
		Name:        name,
		PktOffset:   pktOffset,
		AddValue:    addValue,
		IsBigEndian: bigEndian,
	}
}

// WriteMaskFlowVar is "write_mask_flow_var": masked/shifted write.
type WriteMaskFlowVar struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	PktOffset   int    `json:"pkt_offset"`
	PktCastSize int    `json:"pkt_cast_size"`
	Mask        uint64 `json:"mask"`
	Shift       int    `json:"shift"`
	AddValue    int64  `json:"add_value"`
	IsBigEndian bool   `json:"is_big_endian"`
}

func (WriteMaskFlowVar) instruction() {}

// FixChecksumIPv4 is "fix_checksum_ipv4".
type FixChecksumIPv4 struct {
	Type      string `json:"type"`
	PktOffset int    `json:"pkt_offset"`
}

func (FixChecksumIPv4) instruction() {}

// NewFixChecksumIPv4 fixes the IPv4 header checksum at the given offset.
func NewFixChecksumIPv4(pktOffset int) FixChecksumIPv4 {
	return FixChecksumIPv4{Type: "fix_checksum_ipv4", PktOffset: pktOffset}
}

// FixChecksumHw is "fix_checksum_hw": NIC-offloaded L3/L4 checksum fixup.
type FixChecksumHw struct {
	Type   string `json:"type"`
	L2Len  int    `json:"l2_len"`
	L3Len  int    `json:"l3_len"`
	L4Type int    `json:"l4_type"`
}

func (FixChecksumHw) instruction() {}

// NewFixChecksumHw requests a hardware checksum fixup.
func NewFixChecksumHw(l2Len, l3Len, l4Type int) FixChecksumHw {
	return FixChecksumHw{Type: "fix_checksum_hw", L2Len: l2Len, L3Len: l3Len, L4Type: l4Type}
}

// FixChecksumICMPv6 is "fix_checksum_icmpv6".
type FixChecksumICMPv6 struct {
	Type  string `json:"type"`
	L2Len int    `json:"l2_len"`
	L3Len int    `json:"l3_len"`
}

func (FixChecksumICMPv6) instruction() {}

// TrimPktSize is "trim_pkt_size": trim the packet to the variable's value.
type TrimPktSize struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

func (TrimPktSize) instruction() {}

// NewTrimPktSize trims the packet size to the value of variable name.
func NewTrimPktSize(name string) TrimPktSize {
	return TrimPktSize{Type: "trim_pkt_size", Name: name}
}

// TupleGen is "tuple_flow_var": generates correlated IP/port tuples.
type TupleGen struct {
	Type       string `json:"type"`
	Name       string `json:"name"`
	IPMin      uint64 `json:"ip_min"`
	IPMax      uint64 `json:"ip_max"`
	PortMin    int    `json:"port_min"`
	PortMax    int    `json:"port_max"`
	LimitFlows int    `json:"limit_flows"`
	Flags      int    `json:"flags"`
}

func (TupleGen) instruction() {}

// FlowStats is the per-stream flow-statistics rule. A zero FlowStats (Enabled
// false) serializes to {"enabled": false}.
type FlowStats struct {
	Enabled  bool
	PGID     int
	RuleType string // "stats", "latency" or "tpg"
	VXLAN    bool
	IEEE1588 bool
}

// Flow stats rule types.
const (
	FSStats   = "stats"
	FSLatency = "latency"
	FSTpg     = "tpg"
)

// MarshalJSON emits the enabled/disabled flow-stats object.
func (f FlowStats) MarshalJSON() ([]byte, error) {
	if !f.Enabled {
		return []byte(`{"enabled":false}`), nil
	}
	type wire struct {
		Enabled  bool   `json:"enabled"`
		StreamID int    `json:"stream_id"`
		VXLAN    bool   `json:"vxlan"`
		IEEE1588 bool   `json:"ieee_1588"`
		RuleType string `json:"rule_type"`
	}
	return json.Marshal(wire{
		Enabled:  true,
		StreamID: f.PGID,
		VXLAN:    f.VXLAN,
		IEEE1588: f.IEEE1588,
		RuleType: f.RuleType,
	})
}
