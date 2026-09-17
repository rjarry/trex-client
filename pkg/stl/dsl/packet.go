// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package dsl

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gopacket/gopacket"

	"github.com/rjarry/trex-client/pkg/stl"
)

// buildPacket assembles a stl.Packet from the DSL packet description: the layer
// stack plus an optional payload given either as explicit bytes or a target
// size to pad to.
func buildPacket(pk dslPacket) (*stl.Packet, error) {
	if len(pk.Layers) == 0 {
		return nil, fmt.Errorf("packet has no layers")
	}
	built, err := buildLayers(pk.Layers)
	if err != nil {
		return nil, err
	}

	payload, err := payloadBytes(pk)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		built = append(built, payload)
	}
	return stl.BuildPacket(built...)
}

// payloadBytes returns the payload layer, or nil when no payload is requested.
// An explicit payload (ascii or 0x-prefixed hex) takes precedence over
// payload_size padding.
func payloadBytes(pk dslPacket) (gopacket.SerializableLayer, error) {
	if pk.Payload != "" {
		if strings.HasPrefix(pk.Payload, "0x") {
			b, err := hex.DecodeString(pk.Payload[2:])
			if err != nil {
				return nil, fmt.Errorf("payload hex: %w", err)
			}
			return gopacket.Payload(b), nil
		}
		return gopacket.Payload([]byte(pk.Payload)), nil
	}
	if pk.PayloadSize > 0 {
		return stl.Payload(pk.PayloadSize), nil
	}
	return nil, nil
}
