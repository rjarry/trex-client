// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package ndr

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Results is the outcome of an NDR search.
type Results struct {
	Config     Config      `json:"config"`
	FirstRun   RunResult   `json:"first_run"`
	Iterations []RunResult `json:"iterations"`
	NDRPercent float64     `json:"ndr_percent"`
	NDRRun     RunResult   `json:"ndr_run"`
}

// String renders a human-readable summary.
func (r *Results) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "NDR result\n")
	fmt.Fprintf(&b, "  rate:        %.3f%% of line rate\n", r.NDRPercent)
	fmt.Fprintf(&b, "  iterations:  %d\n", len(r.Iterations))
	fmt.Fprintf(&b, "  tx packets:  %d\n", r.NDRRun.TxPkts)
	fmt.Fprintf(&b, "  rx packets:  %d\n", r.NDRRun.RxPkts)
	fmt.Fprintf(&b, "  drop:        %d (%.4f%%)\n", r.NDRRun.Drop, r.NDRRun.DropPct)
	fmt.Fprintf(&b, "  queue full:  %d (%.4f%%)\n", r.NDRRun.QueueFull, r.NDRRun.QFullPct)
	fmt.Fprintf(&b, "  rx errors:   %d\n", r.NDRRun.RxErr)
	return b.String()
}

// WriteJSON writes the full results as indented JSON to path.
func (r *Results) WriteJSON(path string) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
