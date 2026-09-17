// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package rpc

import (
	"bytes"
	"testing"
)

func TestFrameDeframeRoundTrip(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","method":"get_version","id":"1"}`)

	framed, err := Frame(payload)
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if !IsFramed(framed) {
		t.Fatal("framed output not recognized as framed")
	}

	got, err := Deframe(framed)
	if err != nil {
		t.Fatalf("Deframe: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round trip mismatch:\n got %q\nwant %q", got, payload)
	}
}

func TestDeframeRawReply(t *testing.T) {
	// The server sends small replies unframed; Deframe must pass them through.
	raw := []byte(`{"jsonrpc":"2.0","result":"v3.06","id":"1"}`)
	if IsFramed(raw) {
		t.Fatal("raw JSON should not look framed")
	}
	got, err := Deframe(raw)
	if err != nil {
		t.Fatalf("Deframe raw: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("raw passthrough mismatch: got %q want %q", got, raw)
	}
}

func TestDeframeLengthMismatch(t *testing.T) {
	framed, err := Frame([]byte("hello"))
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	// Corrupt the declared uncompressed length.
	framed[7]++
	if _, err := Deframe(framed); err == nil {
		t.Fatal("expected length mismatch error, got nil")
	}
}
