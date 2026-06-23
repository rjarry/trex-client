# Go TRex client

## Why

The prior clean-room design (`DESIGN-python.md`) was Python, which drags in a
large dependency tree (Scapy, Textual, pyzmq) mainly so it can execute existing
`.py` profiles in-process. This rewrite is **Go**: a single static,
dependency-free binary. The trade-off, taken deliberately, is dropping Python
profiles.

### Do the language-neutral formats give parity?

The server-facing capability set is fully expressible without Python: the
JSON/YAML stream schema can carry all nine field-engine VM opcodes, every rate
mode, and flow/latency/tpg stats. What Python provided was **authoring**
(tunables, Scapy packet construction, loops, IMIX tables, the HLT API) — the
frozen `.json`/`.yaml` are GUI snapshots nobody hand-writes. So the Go client
supplies its own authoring format instead of running Python.

## What replaces Python profiles

A declarative **YAML/JSON DSL** (`internal/stl/dsl`, examples in `examples/`):

- packets are a list of `layers` (eth/dot1q/ip/ipv6/udp/tcp), built to bytes
  with gopacket, with `payload_size` or an explicit `payload`;
- the field engine is a `vm` list whose offsets are symbolic (`IP.src`),
  resolved by decoding the built packet — no Scapy introspection;
- load-time tunables are applied by templating the document before parse
  (defaults before a `---` line, overridden with `-t k=v`);
- an `imix` shortcut splits a rate across weighted frame sizes.

Also supported: the frozen GUI/RPC **snapshot** format (packet bytes and VM
passed through verbatim) and **pcap** replay. HLT is dropped.

## Layout

```
cmd/trexc/          entry point: kong grammar (global flags, info/traffic commands, TUI)
internal/
  rpc/              zlib framing, JSON-RPC, REQ connection (handshake+api_h), SUB subscriber
  session/          Client (connect/version/barrier), Port (acquire/traffic/wait_on_traffic), EventBus
  stats/            async Store, global counters, get_pgid_stats reduction
  stl/              Stream/Profile -> wire, modes, rates, VM opcodes, gopacket packet + offset resolver
    dsl/            declarative profile loader
    loaders/        pcap, snapshot, extension/content dispatcher
  ndr/              client-side server-timed NDR binary search
  cli/              shared kong command grammar, bash completion, interactive shell
  tui/              vaxis alt-screen dashboard: global + per-port stats, embedded command line
```

## Transport and measurement

Same wire protocol and NDR discipline as `DESIGN-python.md` (that analysis is
language-neutral): REQ on 4501 with zlib framing + `api_sync_v2` handshake; SUB
on 4500 as a trigger pulling events via `get_async_events`; NDR is client-side,
server-timed (`duration` + the port job-done event), using exact per-pgid
tx/rx drop with `queue_full`/`rx_err` validity gates. Concurrency is goroutines:
one mutex serializes all REQ traffic to preserve the server's lock-step.

## Build and run

```
cd client
go test ./...
CGO_ENABLED=0 go build -o trexc ./cmd/trexc   # fully static binary

./trexc                                        # interactive shell (no arguments)
./trexc -s <host> version
./trexc -s <host> ndr examples/udp_simple.yaml -d 20 -o ndr.json -t size=128
./trexc -s <host> tui
```

## Status

Phase 1 (STL end-to-end: transport, model, DSL/loaders, ports, NDR, CLI, TUI)
is implemented. Phase 2 (ASTF) is future work, mirroring `stl/` with an ASTF
DSL and reusing `ndr/`. The pure-Go ZMQ dependency (`go-zeromq/zmq4`) is
isolated behind `internal/rpc`; if server interop proves lacking it can be
swapped for a cgo binding without touching the rest.
