// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package stats

import (
	"encoding/json"
	"fmt"
)

// GlobalCounter returns a numeric field from the latest trex-global async
// payload (raw server counters such as "m_total_queue_full", "m_tx_bps"). The
// second result is false when no global stats have arrived or the key is
// missing.
func (s *Store) GlobalCounter(key string) (float64, bool) {
	raw, ok := s.Latest("trex-global")
	if !ok {
		return 0, false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(v, &f); err != nil {
		return 0, false
	}
	return f, true
}

// PortCounter returns a per-port numeric field from the latest trex-global
// payload. The server keys per-port counters as "<key>-<port>" (e.g.
// "m_total_tx_bps-0", "opackets-1").
func (s *Store) PortCounter(key string, port int) (float64, bool) {
	return s.GlobalCounter(fmt.Sprintf("%s-%d", key, port))
}
