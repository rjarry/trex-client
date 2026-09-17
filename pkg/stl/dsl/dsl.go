// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package dsl loads the declarative YAML/JSON traffic-profile format into the
// stl model. It replaces hand-written Python profiles: packets are described as
// a list of layers (built with gopacket), the field engine is a list of
// instructions with symbolic offsets, and load-time tunables are applied by
// templating the document before parsing.
package dsl

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"github.com/rjarry/trex-client/pkg/stl"
)

// Load reads a DSL profile file, applies tunables and returns the stl profile.
// tunables override the document's default params (declared before a "---"
// separator line).
func Load(path string, tunables map[string]string) (stl.Profile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw, tunables)
}

// Parse is Load's byte-oriented core, exposed for testing.
func Parse(raw []byte, tunables map[string]string) (stl.Profile, error) {
	params, body, err := splitParams(raw)
	if err != nil {
		return nil, err
	}
	for k, v := range tunables {
		params[k] = coerce(v)
	}
	rendered, err := render(body, params)
	if err != nil {
		return nil, err
	}

	var entries []dslStream
	if err := yaml.Unmarshal(rendered, &entries); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}

	var profile stl.Profile
	for i, e := range entries {
		streams, err := e.build()
		if err != nil {
			return nil, fmt.Errorf("stream %d: %w", i, err)
		}
		profile = append(profile, streams...)
	}
	return profile, nil
}

// splitParams separates an optional leading params document (plain YAML mapping)
// from the stream body. They are separated by a line containing only "---". The
// params section must not use templates; the body may.
func splitParams(raw []byte) (map[string]any, []byte, error) {
	params := map[string]any{}
	lines := bytes.Split(raw, []byte("\n"))
	sep := -1
	for i, l := range lines {
		if strings.TrimSpace(string(l)) == "---" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return params, raw, nil
	}
	head := bytes.Join(lines[:sep], []byte("\n"))
	body := bytes.Join(lines[sep+1:], []byte("\n"))
	if len(bytes.TrimSpace(head)) > 0 {
		if err := yaml.Unmarshal(head, &params); err != nil {
			return nil, nil, fmt.Errorf("parse params: %w", err)
		}
	}
	return params, body, nil
}

// render applies text/template with the params map. Missing keys are an error so
// typos surface instead of silently producing empty values.
func render(body []byte, params map[string]any) ([]byte, error) {
	tmpl, err := template.New("profile").Option("missingkey=error").Parse(string(body))
	if err != nil {
		return nil, fmt.Errorf("template parse: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return nil, fmt.Errorf("template execute: %w", err)
	}
	return buf.Bytes(), nil
}

// coerce turns a tunable string into an int, float or string so templated
// numeric substitutions render without quotes.
func coerce(s string) any {
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// dslStream is one YAML stream entry.
type dslStream struct {
	Name      string        `yaml:"name"`
	Next      string        `yaml:"next"`
	Packet    dslPacket     `yaml:"packet"`
	Mode      dslMode       `yaml:"mode"`
	VM        []dslVMEntry  `yaml:"vm"`
	FlowStats *dslFlowStats `yaml:"flow_stats"`
	Enabled   *bool         `yaml:"enabled"`
	SelfStart *bool         `yaml:"self_start"`
	ISG       float64       `yaml:"isg"`
	Direction *int          `yaml:"direction"`

	// Imix sugar: expands to one stream per row, splitting the mode rate by
	// weight. When set, Packet/Mode on this entry are ignored.
	Imix []dslImixRow `yaml:"imix"`
}

type dslPacket struct {
	Layers      []layerEntry `yaml:"layers"`
	PayloadSize int          `yaml:"payload_size"`
	Payload     string       `yaml:"payload"`
}

type dslVMEntry map[string]map[string]any

type dslFlowStats struct {
	RuleType string `yaml:"rule_type"`
	PGID     int    `yaml:"pg_id"`
	VXLAN    bool   `yaml:"vxlan"`
	IEEE1588 bool   `yaml:"ieee_1588"`
}

type dslImixRow struct {
	Size   int     `yaml:"size"`
	Weight float64 `yaml:"weight"`
	PGID   *int    `yaml:"pg_id"`
}

// build turns a DSL stream entry into one or more stl streams.
func (e dslStream) build() ([]*stl.Stream, error) {
	if len(e.Imix) > 0 {
		return e.buildImix()
	}
	mode, err := e.Mode.build()
	if err != nil {
		return nil, err
	}
	s, err := e.buildOne(e.Packet, mode, e.Name, e.Next, e.FlowStats)
	if err != nil {
		return nil, err
	}
	return []*stl.Stream{s}, nil
}

func (e dslStream) buildOne(pk dslPacket, mode stl.Mode, name, next string, fs *dslFlowStats) (*stl.Stream, error) {
	pkt, err := buildPacket(pk)
	if err != nil {
		return nil, err
	}
	vm, err := buildVM(e.VM, pkt)
	if err != nil {
		return nil, err
	}

	s := &stl.Stream{
		Name:      name,
		Next:      next,
		Packet:    pkt,
		Mode:      mode,
		VM:        vm,
		Enabled:   boolOr(e.Enabled, true),
		SelfStart: boolOr(e.SelfStart, true),
		ISG:       e.ISG,
		Direction: e.Direction,
	}
	if fs != nil {
		s.Stats = stl.FlowStats{
			Enabled:  true,
			PGID:     fs.PGID,
			RuleType: fs.RuleType,
			VXLAN:    fs.VXLAN,
			IEEE1588: fs.IEEE1588,
		}
	}
	return s, nil
}

// buildImix expands an imix entry into one continuous stream per size row, with
// the base rate split proportionally by weight.
func (e dslStream) buildImix() ([]*stl.Stream, error) {
	total := 0.0
	for _, r := range e.Imix {
		total += r.Weight
	}
	if total == 0 {
		return nil, fmt.Errorf("imix weights sum to zero")
	}
	base, err := e.Mode.build()
	if err != nil {
		return nil, err
	}

	out := make([]*stl.Stream, 0, len(e.Imix))
	for i, r := range e.Imix {
		pk := e.Packet
		pk.PayloadSize = payloadForSize(r.Size, pk)
		name := fmt.Sprintf("%s_%d", orDefault(e.Name, "imix"), i)

		var fs *dslFlowStats
		if r.PGID != nil {
			fs = &dslFlowStats{RuleType: stl.FSStats, PGID: *r.PGID}
		}
		s, err := e.buildOneImix(pk, base, r.Weight/total, name, fs)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (e dslStream) buildOneImix(pk dslPacket, base stl.Mode, fraction float64, name string, fs *dslFlowStats) (*stl.Stream, error) {
	md := base
	md.Rate = stl.Rate{Type: base.Rate.Type, Value: base.Rate.Value * fraction}
	// imix rows are independent streams with no chaining.
	return e.buildOne(pk, md, name, "", fs)
}

// payloadForSize returns the payload_size needed to reach a target frame size
// given the header layers already in pk.
func payloadForSize(target int, pk dslPacket) int {
	hdr := headerLen(pk.Layers)
	if target-hdr < 0 {
		return 0
	}
	return target - hdr
}

func headerLen(entries []layerEntry) int {
	total := 0
	for _, e := range entries {
		for k := range e {
			total += layerHeaderLen(normLayer(k))
		}
	}
	return total
}

func layerHeaderLen(kind string) int {
	switch kind {
	case "eth":
		return 14
	case "dot1q":
		return 4
	case "ip":
		return 20
	case "ipv6":
		return 40
	case "udp":
		return 8
	case "tcp":
		return 20
	default:
		return 0
	}
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
