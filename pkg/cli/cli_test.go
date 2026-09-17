// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package cli

import (
	"context"
	"testing"

	"github.com/alecthomas/kong"
)

func TestDispatchEmptyLine(t *testing.T) {
	if err := Dispatch(context.Background(), &Context{}, "   "); err != nil {
		t.Fatalf("empty line should be a no-op, got %v", err)
	}
}

func TestDispatchUnknown(t *testing.T) {
	if err := Dispatch(context.Background(), &Context{}, "nope"); err == nil {
		t.Fatal("expected unknown command error")
	}
}

func TestParseTunablesPorts(t *testing.T) {
	var cmds Commands
	app, err := kong.New(&cmds)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Parse([]string{"streams", "prof.yaml", "-p", "0,1,2", "-t", "size=64", "-t", "count=10"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmds.Streams.Profile != "prof.yaml" {
		t.Errorf("profile = %q", cmds.Streams.Profile)
	}
	if len(cmds.Streams.Ports) != 3 || cmds.Streams.Ports[2] != 2 {
		t.Errorf("ports = %v", cmds.Streams.Ports)
	}
	if cmds.Streams.Tunables["size"] != "64" || cmds.Streams.Tunables["count"] != "10" {
		t.Errorf("tunables = %v", cmds.Streams.Tunables)
	}
}

func TestParsePortsDefault(t *testing.T) {
	var cmds Commands
	app, err := kong.New(&cmds)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Parse([]string{"streams", "prof.yaml"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cmds.Streams.Ports) != 2 || cmds.Streams.Ports[0] != 0 || cmds.Streams.Ports[1] != 1 {
		t.Errorf("default ports = %v, want [0 1]", cmds.Streams.Ports)
	}
}
