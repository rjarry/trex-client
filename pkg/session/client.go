// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
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
// the stats store or event bus.
func (c *Client) Dispatch(ev rpc.AsyncEvent) {
	switch ev.Name {
	case "trex-global", "flow_stats", "latency_stats":
		c.stats.Dispatch(ev)
	case "trex-event":
		c.events.Publish(ServerEvent{ID: ServerEventID(ev.Type), Data: ev.Data})
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

// Barrier forces a fresh async snapshot and waits until the subscriber records
// it, guaranteeing the stats store holds current data. It calls publish_now
// (which, in interactive mode, synchronously publishes the stats then an empty
// trigger) and then waits for the store's generation to advance. If baseline is
// true the server emits a full snapshot even for unchanged counters.
func (c *Client) Barrier(timeout time.Duration, baseline bool) error {
	gen := c.stats.Generation()

	deadline := time.After(timeout)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()

	for {
		if _, err := c.conn.Call("publish_now", map[string]any{"key": rand.Uint32(), "baseline": baseline}); err != nil {
			return fmt.Errorf("publish_now: %w", err)
		}
		select {
		case <-deadline:
			return fmt.Errorf("barrier timeout after %s: no async data from server", timeout)
		case <-tick.C:
			if c.stats.Generation() > gen {
				return nil
			}
		}
	}
}
