// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package stats

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/rjarry/trex-client/pkg/rpc"
)

// PgidCounters holds a flow group's packet/byte counts summed across all ports.
type PgidCounters struct {
	TxPkts  uint64
	RxPkts  uint64
	TxBytes uint64
	RxBytes uint64
}

// PgidSnapshot is a reduced get_pgid_stats reading: per-pgid counters summed
// over ports plus the global rx error count. These are the exact,
// test-traffic-only counts NDR uses, immune to unrelated traffic.
type PgidSnapshot struct {
	Flow  map[int]PgidCounters
	RxErr uint64
}

// SumTx returns the total transmitted packets across all flow groups.
func (s *PgidSnapshot) SumTx() uint64 {
	var t uint64
	for _, c := range s.Flow {
		t += c.TxPkts
	}
	return t
}

// SumRx returns the total received packets across all flow groups.
func (s *PgidSnapshot) SumRx() uint64 {
	var t uint64
	for _, c := range s.Flow {
		t += c.RxPkts
	}
	return t
}

// Drop returns the packet drop count: transmitted minus received across all
// flow groups. It is the NDR drop metric.
func (s *PgidSnapshot) Drop() uint64 {
	tx, rx := s.SumTx(), s.SumRx()
	if rx > tx {
		return 0
	}
	return tx - rx
}

// GetActivePGIDs returns the active latency and flow-stat pgids.
func GetActivePGIDs(conn *rpc.Connection) (latency, flowStats []int, err error) {
	res, err := conn.Call("get_active_pgids", map[string]any{})
	if err != nil {
		return nil, nil, err
	}
	var out struct {
		IDs struct {
			Latency   []int `json:"latency"`
			FlowStats []int `json:"flow_stats"`
		} `json:"ids"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, nil, fmt.Errorf("parse active pgids: %w", err)
	}
	return out.IDs.Latency, out.IDs.FlowStats, nil
}

// GetPgidStats reads and reduces per-pgid counters for the given pgids.
func GetPgidStats(conn *rpc.Connection, pgids []int) (*PgidSnapshot, error) {
	res, err := conn.Call("get_pgid_stats", map[string]any{"pgids": pgids})
	if err != nil {
		return nil, err
	}
	return parsePgidStats(res)
}

// parsePgidStats reduces a get_pgid_stats reply into a PgidSnapshot.
func parsePgidStats(res json.RawMessage) (*PgidSnapshot, error) {
	var out struct {
		FlowStats map[string]json.RawMessage `json:"flow_stats"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("parse pgid stats: %w", err)
	}

	snap := &PgidSnapshot{Flow: make(map[int]PgidCounters)}
	for key, raw := range out.FlowStats {
		if key == "g" {
			snap.RxErr = sumPortMap(raw, "rx_err")
			continue
		}
		id, convErr := strconv.Atoi(key)
		if convErr != nil {
			continue // non-numeric keys like "g" already handled
		}
		var fields map[string]map[string]float64
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, fmt.Errorf("parse pgid %s: %w", key, err)
		}
		snap.Flow[id] = PgidCounters{
			TxPkts:  sumPorts(fields["tp"]),
			RxPkts:  sumPorts(fields["rp"]),
			TxBytes: sumPorts(fields["tb"]),
			RxBytes: sumPorts(fields["rb"]),
		}
	}
	return snap, nil
}

// sumPorts sums a per-port counter map.
func sumPorts(m map[string]float64) uint64 {
	var t float64
	for _, v := range m {
		t += v
	}
	return uint64(t)
}

// sumPortMap extracts a named per-port sub-object from a raw global object and
// sums it.
func sumPortMap(raw json.RawMessage, field string) uint64 {
	var g map[string]map[string]float64
	if err := json.Unmarshal(raw, &g); err != nil {
		return 0
	}
	return sumPorts(g[field])
}
