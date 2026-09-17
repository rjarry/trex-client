// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/rjarry/trex-client/pkg/rpc"
	"github.com/rjarry/trex-client/pkg/stats"
)

// APIClass and version negotiated with the server for the STL engine.
const (
	APIClass    = "STL"
	APIMajor    = 5
	APIMinor    = 1
	DefaultUser = "trex"
)

// Version is the get_version result.
type Version struct {
	Version   string `json:"version"`
	BuildDate string `json:"build_date"`
	BuildTime string `json:"build_time"`
	BuiltBy   string `json:"built_by"`
	Mode      string `json:"mode"`
}

// SystemInfo is the subset of get_system_info we surface.
type SystemInfo struct {
	Hostname    string          `json:"hostname"`
	Uptime      string          `json:"uptime"`
	DPCoreCount int             `json:"dp_core_count"`
	CoreType    string          `json:"core_type"`
	PortCount   int             `json:"port_count"`
	Ports       json.RawMessage `json:"ports"`
}

// Client is the top-level control-plane handle: a synchronous RPC connection
// plus the async subscriber feeding a stats Store and an EventBus.
type Client struct {
	host      string
	username  string
	sessionID uint32

	conn   *rpc.Connection
	sub    *rpc.Subscriber
	stats  *stats.Store
	events *EventBus

	subCtx    context.Context
	subCancel context.CancelFunc
	subDone   chan struct{}

	barrierMu sync.Mutex
	barriers  map[uint32]chan struct{}
}

// Connect dials both channels, performs the handshake and starts the
// subscriber goroutine.
func Connect(ctx context.Context, host, username string) (*Client, error) {
	if username == "" {
		username = DefaultUser
	}
	conn, err := rpc.Dial(ctx, host, rpc.DefaultSyncPort)
	if err != nil {
		return nil, err
	}
	if err := conn.Handshake(APIClass, APIMajor, APIMinor); err != nil {
		conn.Close()
		return nil, err
	}

	c := &Client{
		host:      host,
		username:  username,
		sessionID: rand.Uint32(),
		conn:      conn,
		stats:     stats.NewStore(),
		events:    NewEventBus(),
		subDone:   make(chan struct{}),
		barriers:  make(map[uint32]chan struct{}),
	}

	sub, err := rpc.NewSubscriber(ctx, host, rpc.DefaultAsyncPort, conn, c.sessionID, c, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	c.sub = sub
	c.subCtx, c.subCancel = context.WithCancel(ctx)
	go func() {
		defer close(c.subDone)
		sub.Run(c.subCtx)
	}()

	return c, nil
}

// Close stops the subscriber and releases both sockets.
func (c *Client) Close() error {
	c.subCancel()
	c.sub.Close()
	<-c.subDone
	return c.conn.Close()
}

// Host returns the server host this client is connected to.
func (c *Client) Host() string { return c.host }

// Stats returns the async stats store.
func (c *Client) Stats() *stats.Store { return c.stats }

// Events returns the server event bus.
func (c *Client) Events() *EventBus { return c.events }

// SessionID returns the client's session id, shared by acquire and the
// subscriber.
func (c *Client) SessionID() uint32 { return c.sessionID }

// Conn exposes the raw RPC connection for higher layers (ports, stats reads).
func (c *Client) Conn() *rpc.Connection { return c.conn }

// Dispatch implements rpc.Dispatcher: it routes async events by channel name to
// the stats store, event bus or barrier waiters.
func (c *Client) Dispatch(ev rpc.AsyncEvent) {
	switch ev.Name {
	case "trex-global", "flow_stats", "latency_stats":
		c.stats.Dispatch(ev)
	case "trex-event":
		c.events.Publish(ServerEvent{ID: ServerEventID(ev.Type), Data: ev.Data})
	case "trex-barrier":
		c.ackBarrier(uint32(ev.Type))
	}
}

// GetVersion returns the server version.
func (c *Client) GetVersion() (Version, error) {
	var v Version
	res, err := c.conn.Call("get_version", nil)
	if err != nil {
		return v, err
	}
	return v, json.Unmarshal(res, &v)
}

// GetSystemInfo returns the server system info.
func (c *Client) GetSystemInfo() (SystemInfo, error) {
	var s SystemInfo
	res, err := c.conn.Call("get_system_info", nil)
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(res, &s)
}

// Ping issues a ping and returns the server timestamp payload.
func (c *Client) Ping() (json.RawMessage, error) {
	return c.conn.Call("ping", nil)
}

// Barrier forces a full round trip through the async channel: it publishes a
// unique key and waits until the subscriber sees the matching trex-barrier
// message, guaranteeing all prior async data has been delivered. If baseline is
// true the server emits a full snapshot.
func (c *Client) Barrier(timeout time.Duration, baseline bool) error {
	key := rand.Uint32()
	ch := make(chan struct{})

	c.barrierMu.Lock()
	c.barriers[key] = ch
	c.barrierMu.Unlock()

	defer func() {
		c.barrierMu.Lock()
		delete(c.barriers, key)
		c.barrierMu.Unlock()
	}()

	deadline := time.After(timeout)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()

	for {
		if _, err := c.conn.Call("publish_now", map[string]any{"key": key, "baseline": baseline}); err != nil {
			return fmt.Errorf("publish_now: %w", err)
		}
		select {
		case <-ch:
			return nil
		case <-deadline:
			return fmt.Errorf("barrier timeout after %s: no async data from server", timeout)
		case <-tick.C:
		}
	}
}

// ackBarrier releases the waiter for key, if any.
func (c *Client) ackBarrier(key uint32) {
	c.barrierMu.Lock()
	ch, ok := c.barriers[key]
	if ok {
		delete(c.barriers, key)
	}
	c.barrierMu.Unlock()
	if ok {
		close(ch)
	}
}
