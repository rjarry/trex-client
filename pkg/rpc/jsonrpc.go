// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
)

// Request is a single JSON-RPC 2.0 request object.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a single JSON-RPC 2.0 response object. Exactly one of Result or
// Error is set on a well-formed reply.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
}

// RPCError is a JSON-RPC error object and satisfies the error interface. The
// TRex server carries the human-readable reason in the non-standard
// "specific_err" field rather than "data".
type RPCError struct {
	Code        int             `json:"code"`
	Message     string          `json:"message"`
	SpecificErr string          `json:"specific_err,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	detail := e.SpecificErr
	if detail == "" && len(e.Data) > 0 {
		detail = string(e.Data)
	}
	if detail != "" {
		return fmt.Sprintf("rpc error %d: %s: %s", e.Code, e.Message, detail)
	}
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

var idCounter atomic.Uint64

// nextID returns a fresh, process-unique request id.
func nextID() string {
	return strconv.FormatUint(idCounter.Add(1), 10)
}

// BuildRequest marshals a single JSON-RPC request. params may be nil.
func BuildRequest(method string, params any) (Request, []byte, error) {
	req := Request{JSONRPC: "2.0", ID: nextID(), Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return Request{}, nil, fmt.Errorf("marshal params for %s: %w", method, err)
		}
		req.Params = raw
	}
	buf, err := json.Marshal(req)
	if err != nil {
		return Request{}, nil, fmt.Errorf("marshal request %s: %w", method, err)
	}
	return req, buf, nil
}

// BuildBatch marshals a slice of requests as a JSON array batch.
func BuildBatch(reqs []Request) ([]byte, error) {
	buf, err := json.Marshal(reqs)
	if err != nil {
		return nil, fmt.Errorf("marshal batch: %w", err)
	}
	return buf, nil
}

// ParseResponse decodes a single (already de-framed) JSON-RPC response.
func ParseResponse(data []byte) (*Response, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &resp, nil
}

// ParseBatchResponse decodes a de-framed JSON-RPC batch reply. The server
// returns results in the same order as the batch was sent, but callers should
// match by id when order matters. When a batch holds a single request the TRex
// server replies with a bare response object rather than a one-element array, so
// both encodings are accepted.
func ParseBatchResponse(data []byte) ([]Response, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var resp Response
		if err := json.Unmarshal(trimmed, &resp); err != nil {
			return nil, fmt.Errorf("parse batch response: %w", err)
		}
		return []Response{resp}, nil
	}
	var resps []Response
	if err := json.Unmarshal(data, &resps); err != nil {
		return nil, fmt.Errorf("parse batch response: %w", err)
	}
	return resps, nil
}
