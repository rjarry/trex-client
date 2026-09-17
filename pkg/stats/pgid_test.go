// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package stats

import "testing"

func TestParsePgidStats(t *testing.T) {
	// Two flow groups across two ports plus a global rx_err block.
	res := []byte(`{
		"flow_stats": {
			"7":  {"tp": {"0": 1000, "1": 0}, "rp": {"0": 0, "1": 990}, "tb": {"0": 64000}, "rb": {"1": 63360}},
			"8":  {"tp": {"0": 500},  "rp": {"0": 500}},
			"g":  {"rx_err": {"0": 3, "1": 2}, "tx_err": {"0": 0}}
		}
	}`)
	snap, err := parsePgidStats(res)
	if err != nil {
		t.Fatalf("parsePgidStats: %v", err)
	}
	if got := snap.Flow[7].TxPkts; got != 1000 {
		t.Errorf("pgid 7 tx = %d, want 1000", got)
	}
	if got := snap.Flow[7].RxPkts; got != 990 {
		t.Errorf("pgid 7 rx = %d, want 990", got)
	}
	if snap.SumTx() != 1500 || snap.SumRx() != 1490 {
		t.Errorf("sums = tx %d rx %d, want 1500/1490", snap.SumTx(), snap.SumRx())
	}
	if snap.Drop() != 10 {
		t.Errorf("drop = %d, want 10", snap.Drop())
	}
	if snap.RxErr != 5 {
		t.Errorf("rx_err = %d, want 5", snap.RxErr)
	}
}

func TestPgidDropNeverNegative(t *testing.T) {
	// rx can momentarily exceed tx from prior in-flight packets; drop clamps.
	s := &PgidSnapshot{Flow: map[int]PgidCounters{1: {TxPkts: 100, RxPkts: 105}}}
	if s.Drop() != 0 {
		t.Errorf("drop = %d, want 0", s.Drop())
	}
}
