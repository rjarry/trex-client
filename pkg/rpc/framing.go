// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

// Package rpc implements the TRex JSON-RPC transport: zlib framing, request
// building, the synchronous REQ connection and the asynchronous subscriber.
package rpc

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
)

// Magic prefixes a zlib-compressed frame. The server frames replies the same
// way once the payload exceeds its threshold; smaller replies arrive raw.
const Magic uint32 = 0xABE85CEA

// headerLen is the size of the frame header: magic (u32) + uncompressed length
// (u32), both big-endian.
const headerLen = 8

// Frame wraps a JSON payload in the zlib frame the server expects: an 8-byte
// big-endian header (magic, uncompressed length) followed by the zlib stream.
// The client always compresses outbound requests.
func Frame(payload []byte) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(headerLen + len(payload)/2)

	var header [headerLen]byte
	binary.BigEndian.PutUint32(header[0:4], Magic)
	binary.BigEndian.PutUint32(header[4:8], uint32(len(payload)))
	buf.Write(header[:])

	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		return nil, fmt.Errorf("zlib compress: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("zlib close: %w", err)
	}
	return buf.Bytes(), nil
}

// IsFramed reports whether data starts with the frame magic.
func IsFramed(data []byte) bool {
	return len(data) >= headerLen && binary.BigEndian.Uint32(data[0:4]) == Magic
}

// Deframe returns the JSON payload from data. Framed data is decompressed and
// length-checked; unframed data is returned unchanged. This makes it safe to
// call on every reply regardless of how the server chose to encode it.
func Deframe(data []byte) ([]byte, error) {
	if !IsFramed(data) {
		return data, nil
	}
	want := binary.BigEndian.Uint32(data[4:8])

	zr, err := zlib.NewReader(bytes.NewReader(data[headerLen:]))
	if err != nil {
		return nil, fmt.Errorf("zlib reader: %w", err)
	}
	defer zr.Close()

	out, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("zlib decompress: %w", err)
	}
	if uint32(len(out)) != want {
		return nil, fmt.Errorf("framed length mismatch: header says %d, got %d", want, len(out))
	}
	return out, nil
}
