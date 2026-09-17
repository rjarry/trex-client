// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/go-zeromq/zmq4"
)

// DefaultSyncPort is the TRex synchronous RPC port.
const DefaultSyncPort = 4501

// Connection owns the synchronous REQ socket and serializes every request
// through it. The server's REQ/REP channel is strict lock-step, so all callers
// (RPCs, batches, barriers, and the subscriber's get_async_events pull) must
// funnel through the same mutex; holding it across send/recv preserves the
// lock-step invariant with no races.
type Connection struct {
	mu   sync.Mutex
	sock zmq4.Socket
	ctx  context.Context

	apiH string // api handle from api_sync_v2, injected into every later call
}

// Dial connects a REQ socket to the server's synchronous RPC port.
func Dial(ctx context.Context, host string, port int) (*Connection, error) {
	sock := zmq4.NewReq(ctx)
	endpoint := fmt.Sprintf("tcp://%s:%d", host, port)
	if err := sock.Dial(endpoint); err != nil {
		return nil, fmt.Errorf("dial %s: %w", endpoint, err)
	}
	return &Connection{sock: sock, ctx: ctx}, nil
}

// Close releases the underlying socket.
func (c *Connection) Close() error {
	return c.sock.Close()
}

// APIHandle returns the handshake-negotiated api handle (empty before Handshake).
func (c *Connection) APIHandle() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.apiH
}

// roundTrip frames, sends, receives and de-frames one payload. The caller must
// hold c.mu.
func (c *Connection) roundTrip(payload []byte) ([]byte, error) {
	framed, err := Frame(payload)
	if err != nil {
		return nil, err
	}
	if err := c.sock.Send(zmq4.NewMsg(framed)); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}
	msg, err := c.sock.Recv()
	if err != nil {
		return nil, fmt.Errorf("recv: %w", err)
	}
	if len(msg.Frames) == 0 {
		return nil, fmt.Errorf("recv: empty message")
	}
	return Deframe(msg.Frames[0])
}

// Call sends a single JSON-RPC request and returns its result payload. The
// negotiated api handle is injected into params automatically. params may be
// nil; if non-nil it must be a JSON object (map) so api_h can be added.
func (c *Connection) Call(method string, params map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callLocked(method, params)
}

// callLocked performs a Call assuming c.mu is held.
func (c *Connection) callLocked(method string, params map[string]any) (json.RawMessage, error) {
	p := c.withAPIHandle(params)
	_, buf, err := BuildRequest(method, p)
	if err != nil {
		return nil, err
	}
	reply, err := c.roundTrip(buf)
	if err != nil {
		return nil, err
	}
	resp, err := ParseResponse(reply)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// CallBatch sends several requests as one JSON array and returns the responses.
// The api handle is injected into each request's params.
func (c *Connection) CallBatch(reqs []BatchCall) ([]Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	built := make([]Request, len(reqs))
	for i, bc := range reqs {
		req, _, err := BuildRequest(bc.Method, c.withAPIHandle(bc.Params))
		if err != nil {
			return nil, err
		}
		built[i] = req
	}
	buf, err := BuildBatch(built)
	if err != nil {
		return nil, err
	}
	reply, err := c.roundTrip(buf)
	if err != nil {
		return nil, err
	}
	return ParseBatchResponse(reply)
}

// BatchCall is one entry in a batched request.
type BatchCall struct {
	Method string
	Params map[string]any
}

// withAPIHandle returns a copy of params with api_h set, when the handshake has
// completed. It never mutates the caller's map.
func (c *Connection) withAPIHandle(params map[string]any) map[string]any {
	if c.apiH == "" {
		return params
	}
	out := make(map[string]any, len(params)+1)
	for k, v := range params {
		out[k] = v
	}
	out["api_h"] = c.apiH
	return out
}

// Handshake performs api_sync_v2 and stores the returned api handle, which is
// then injected into every subsequent call. It must run before any other call.
// name is the API class ("STL", "ASTF", ...) with its major/minor version.
func (c *Connection) Handshake(name string, major, minor int) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	result, err := c.callLocked("api_sync_v2", map[string]any{
		"name":  name,
		"major": major,
		"minor": minor,
	})
	if err != nil {
		return fmt.Errorf("api_sync_v2: %w", err)
	}

	var out struct {
		APIH string `json:"api_h"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return fmt.Errorf("parse api_sync_v2 result: %w", err)
	}
	if out.APIH == "" {
		return fmt.Errorf("api_sync_v2: no api handle in reply: %s", result)
	}
	c.apiH = out.APIH
	return nil
}
