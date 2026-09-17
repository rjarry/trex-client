// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package rpc

import (
	"encoding/json"
	"testing"
)

func TestBuildRequestUniqueIDs(t *testing.T) {
	_, _, err := BuildRequest("get_version", nil)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	r1, _, _ := BuildRequest("ping", map[string]any{"a": 1})
	r2, _, _ := BuildRequest("ping", map[string]any{"a": 1})
	if r1.ID == r2.ID {
		t.Fatalf("expected unique ids, both %q", r1.ID)
	}
	if r1.JSONRPC != "2.0" {
		t.Fatalf("jsonrpc version = %q, want 2.0", r1.JSONRPC)
	}
}

func TestParseResponseResult(t *testing.T) {
	resp, err := ParseResponse([]byte(`{"jsonrpc":"2.0","id":"1","result":"v3.06"}`))
	if err != nil {
		t.Fatalf("ParseResponse: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	var v string
	if err := json.Unmarshal(resp.Result, &v); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if v != "v3.06" {
		t.Fatalf("result = %q, want v3.06", v)
	}
}

func TestParseBatchWithPerItemError(t *testing.T) {
	data := []byte(`[{"jsonrpc":"2.0","id":"1","result":true},` +
		`{"jsonrpc":"2.0","id":"2","error":{"code":-32000,"message":"bad handler"}}]`)
	resps, err := ParseBatchResponse(data)
	if err != nil {
		t.Fatalf("ParseBatchResponse: %v", err)
	}
	if len(resps) != 2 {
		t.Fatalf("got %d responses, want 2", len(resps))
	}
	if resps[0].Error != nil {
		t.Fatalf("item 0 should succeed, got %v", resps[0].Error)
	}
	if resps[1].Error == nil {
		t.Fatal("item 1 should carry an error")
	}
	if resps[1].Error.Code != -32000 {
		t.Fatalf("item 1 code = %d, want -32000", resps[1].Error.Code)
	}
}
