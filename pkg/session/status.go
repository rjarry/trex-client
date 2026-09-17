// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package session

import (
	"encoding/json"
	"fmt"
)

// PortInfo is the static per-port identity from get_system_info.
type PortInfo struct {
	Index   int    `json:"index"`
	Driver  string `json:"driver"`
	Numa    int    `json:"numa"`
	PciAddr string `json:"pci_addr"`
	HwMac   string `json:"hw_mac"`
}

// PortInfos decodes the ports array of a get_system_info reply.
func (s SystemInfo) PortInfos() ([]PortInfo, error) {
	if len(s.Ports) == 0 {
		return nil, nil
	}
	var ports []PortInfo
	if err := json.Unmarshal(s.Ports, &ports); err != nil {
		return nil, fmt.Errorf("parse ports: %w", err)
	}
	return ports, nil
}

// PortStatus is the dynamic per-port state from get_port_status: current owner,
// state (IDLE/STREAMS/TX/PAUSE/DOWN...), link and negotiated speed in Gbps.
type PortStatus struct {
	Owner  string
	State  string
	LinkUp bool
	Speed  float64
}

// Status reads the current port status.
func (p *Port) Status() (PortStatus, error) {
	res, err := p.conn().Call("get_port_status", map[string]any{"port_id": p.id})
	if err != nil {
		return PortStatus{}, fmt.Errorf("get_port_status port %d: %w", p.id, err)
	}
	var out struct {
		Owner string `json:"owner"`
		State string `json:"state"`
		Attr  struct {
			Link struct {
				Up bool `json:"up"`
			} `json:"link"`
			Speed float64 `json:"speed"`
		} `json:"attr"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return PortStatus{}, fmt.Errorf("parse port %d status: %w", p.id, err)
	}
	return PortStatus{
		Owner:  out.Owner,
		State:  out.State,
		LinkUp: out.Attr.Link.Up,
		Speed:  out.Attr.Speed,
	}, nil
}
