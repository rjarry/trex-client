// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package dsl

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/rjarry/trex-client/pkg/stl"
)

type dslMode struct {
	Type         string  `yaml:"type"`
	Rate         dslRate `yaml:"rate"`
	TotalPkts    int     `yaml:"total_pkts"`
	PktsPerBurst int     `yaml:"pkts_per_burst"`
	IBG          float64 `yaml:"ibg"`
	Count        int     `yaml:"count"`
}

// dslRate accepts either a scalar string ("100%", "1gbps") or a mapping
// {type, value}.
type dslRate struct {
	rate stl.Rate
	set  bool
}

func (r *dslRate) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		parsed, err := stl.ParseRate(node.Value)
		if err != nil {
			return err
		}
		r.rate = parsed
	case yaml.MappingNode:
		var m struct {
			Type  string  `yaml:"type"`
			Value float64 `yaml:"value"`
		}
		if err := node.Decode(&m); err != nil {
			return err
		}
		r.rate = stl.Rate{Type: m.Type, Value: m.Value}
	default:
		return fmt.Errorf("rate must be a string or a {type, value} mapping")
	}
	r.set = true
	return nil
}

func (m dslMode) build() (stl.Mode, error) {
	rate := m.Rate.rate
	if !m.Rate.set {
		rate = stl.Rate{Type: stl.RatePercentage, Value: 100}
	}
	switch m.Type {
	case "", stl.ModeContinuous:
		return stl.Continuous(rate), nil
	case stl.ModeSingleBurst:
		return stl.SingleBurst(rate, m.TotalPkts), nil
	case stl.ModeMultiBurst:
		return stl.MultiBurst(rate, m.PktsPerBurst, m.IBG, m.Count), nil
	default:
		return stl.Mode{}, fmt.Errorf("unknown mode type %q", m.Type)
	}
}
