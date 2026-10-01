# Transit model: wire envelope, delivery classes, Session/Channel/Backend

## Where this sits

[`01-build-order.md`](01-build-order.md) names Transit a Layer 1 base
needing nothing — not a DB, not Gatehouse, not certs — and
[`02-package-boundaries.md`](02-package-boundaries.md) is explicit that
Transit is *not obviously* structure/evaluation/storage/facade-shaped:
its state (open connections, channels) is transient and
connection-scoped, never persisted domain data. This doc adopts that
honesty rather than forcing the pattern: Transit decomposes into
`wire` (shared model) and `transit` (behavior interfaces) instead.

This ports Lighthouse's own transport layer design
(`lighthouse-docs/lighthouse_transport_layer_design_plan_v1.md` in the
Lighthouse repo) close to unchanged — nothing about the envelope,
delivery-class taxonomy, or Session/Channel/Backend split is
realm-shaped or Lighthouse-specific; it's `00-overview.md`'s own claim,
restated here with the actual interfaces rather than just the name:
"the transport layer's delivery-class taxonomy... carr[ies] forward
into Archipelago unchanged." What's genuinely new in this doc is the
package split (`wire` + `transit` as two modules, not one, decomposing
the single "Transit" dependency unit the build-order table names — the
same kind of internal decomposition Gatehouse-core's own
structure/evaluation/storage/facade split already does for its
dependency unit) and one scope call explained below.

| Concept | Status |
|---|---|
| `wire` as its own module (envelope, Kind, DeliveryClass, channel-id helpers) | confirmed, ported |
| Delivery classes abstract QoS, not mechanism | confirmed, ported |
| `Session`/`Channel`/`Backend`/`Capabilities`/`PeerIdentity` interfaces | confirmed, ported |
| Initiator-scoped channel-id allocation | confirmed, ported |
| Transport stays ignorant of authority | confirmed, ported |
| WebSocket backend | confirmed, built this pass |
| WebTransport backend | designed-for via `Backend`, not built this pass — see below |
| `Signal`/datagram delivery class | designed-for, deferred — no current consumer needs lossy delivery |
| Protocol/endpoint versioning | deliberately deferred, same trigger condition as Lighthouse's own |

## Package split: `wire` vs `transit`

```text
wire      model only: Message{type,payload,id,kind,channel}, Kind +
          DeliveryClass constants, channel-id helpers, encode/decode.
          No sockets, no QUIC. Its own module — anything that needs to
          speak the envelope (an SDK, a future app) depends on this
          alone, never on goroutines/ctx/backpressure machinery it has
          no business needing.

transit   behavior: Session, Channel, Backend interfaces; the
          WebSocket backend as a subpackage (transit/websocket). Its
          own module, depending on wire. No handler, app, or
          integration imports a concrete backend package directly —
          everything depends on transit's interfaces, the same
          dependency-inversion rule that makes a second backend
          additive instead of a rewrite.
```

The load-bearing rule, unchanged from the source doc: every dependency
arrow points toward the abstraction. Nothing above `transit` may import
`transit/websocket` (or, later, `transit/webtransport`) directly.

## The wire envelope

```text
Message
- type     namespaced string, the primary routing key
- payload  opaque per-message data
- id       correlation id for one request/response exchange
- kind     request | response | event | ack | stream_open |
           stream_data | stream_end | cancel | error
- channel  ordering + flow-control scope for a long-lived logical stream
```

`id` correlates one response to one request and lives for a single
exchange; `channel` scopes ordering/flow for a long-lived logical
stream and lives for many messages. Both are additive-only fields: a
message that omits them behaves exactly as a single-exchange,
unordered send would without them.

## Delivery classes abstract over QoS, not mechanism

The trap a transport-agnostic interface must not fall into: a
`SendDatagram()`-style method is lossy on a backend with true
datagrams and lossless on one without, so a subsystem written against
it cannot be correct on both. Handlers declare **intent**; the backend
picks the mechanism.

| Class | Order | Reliable | Size | WebSocket realization | WebTransport realization |
|---|---|---|---|---|---|
| Request/Response | per-id | yes | frame cap | inline correlated reply | bidi stream / control stream |
| Event/Push | per-channel | yes | frame cap | reliable non-blocking push | unidirectional stream |
| Channel/Stream | per-channel | yes | streamed | logical mux channel | QUIC stream |
| Signal (fire-and-forget) | none | **no** | small | reliable push (over-delivers) | datagram — **deferred** |

**Every class is contracted at its weakest guarantee.** `Signal` is
documented as "may be dropped, may arrive out of order" even though the
WebSocket realization happens to over-deliver it (always reliable) —
strengthening a guarantee never breaks a correct consumer, so building
only the reliable backend first doesn't paint a future lossy backend
into a corner. `Signal` stays in the taxonomy, undelivered, until a real
consumer wants lossy delivery; every delivery need identified so far
("tell a peer something happened, no reply, can't be dropped") is
`Event/Push`, not `Signal`.

## Interfaces

```go
// transit

type Session interface {
    RemoteAddr() string
    PeerIdentity() PeerIdentity          // verified transport-level peer (mTLS); zero value if none
    Capabilities() Capabilities          // observability/realization only, never handler branching
    Done() <-chan struct{}

    Reply(ctx context.Context, msg wire.Message) error  // request/response, correlated by id
    Push(ctx context.Context, msg wire.Message) error   // reliable peer-directed event
    OpenChannel(ctx context.Context, opts ChannelOpts) (Channel, error)
}

type Channel interface {
    ID() string
    Send(ctx context.Context, msg wire.Message) error  // ordered, backpressured
    Recv(ctx context.Context) (wire.Message, error)
    Close(reason string) error
    Done() <-chan struct{}
}

type Capabilities struct {
    NativeStreams   bool // independent network-level streams (no cross-channel head-of-line blocking)
    Datagrams       bool // real unreliable datagrams
    MaxDatagramSize int
}

// PeerIdentity is the verified transport-level identity of the peer,
// populated from a presented mTLS client certificate. Present=false
// means no client cert was offered. Transit surfaces it; it never
// interprets it — whether a cert is a credential, which principal it
// binds, and how it composes with other credentials is an mTLS-
// integration concern (certstore + Gatehouse-core + Transit, per
// 01-build-order.md's Layer 2 table), not decided here.
type PeerIdentity struct {
    Present     bool
    Fingerprint string
    Subject     string
    SANs        []string
}

type Backend interface {
    Accept(ctx context.Context) (Session, error)
    Close() error
}
```

`Capabilities()` exists for logging and for Transit's own realization
choices, never for handler branching — keeping every consumer uniform
is what stops the WebSocket-only codebase from growing "if datagrams…"
branches that bit-rot unused until a second backend actually exists.

No `Value`/`Remember` session-scoped KV method: Lighthouse's own
version existed to lift a connection-scoped authority-session cache
that's an authority concern, not a transport one — carrying it into
Transit here would be exactly the "transport stays ignorant of
authority" rule being violated by the one thing being ported. A
consumer that wants connection-scoped state keeps its own map keyed by
`Session`, same as any other caller-side cache.

## Channel-id allocation

The party that opens a channel allocates its id from its own
namespace, carried in the envelope alongside an initiator marker so
the two id spaces never collide — unifies "client allocates" (a
subscription) and "server allocates" (a brokered relay between two
peers) under one rule rather than two schemes.

## Scope call: WebSocket now, WebTransport designed-for

Lighthouse's own plan deliberately built WebSocket only and left
WebTransport as "design-for, build-later," reasoned from the practical
state of its own codebase at the time. `01-build-order.md`'s one-line
description of this base ("WT/WS backends") reads as wanting both from
day one, so this is worth being explicit about rather than silently
repeating Lighthouse's call without restating why: the `Backend`
interface is deliberately shaped so a second backend is additive, not
a rewrite, and a working WebTransport backend needs real QUIC/HTTP-3
plumbing (`quic-go`/`webtransport-go`) that's a substantial, separately
verifiable effort in its own right — the same "verify the real library
before building against it" discipline already applied to
`smallstep/certificates` in `05-pki-and-signing.md`. Building and
proving the WebSocket backend first, with the interface already
shaped to not need changing when a WebTransport sibling lands, is the
lower-risk order; it is not a claim that WebTransport is less wanted.

## What stays explicitly deferred

- **WebTransport backend.** `Backend`/`Capabilities` accommodate it;
  no `quic-go`/`webtransport-go` code is written in this pass.
- **`Signal`/datagram delivery class.** Taxonomy slot reserved; no
  consumer needs lossy delivery yet.
- **Protocol/endpoint versioning.** Same trigger as Lighthouse's own:
  the first client that can't be updated in lockstep with its peer.
  Not relevant yet with zero shipped consumers.
- **Concurrent/opt-in dispatch and per-channel backpressure.** A real
  router sitting above `Backend.Accept` isn't designed here — this
  doc fixes the wire shape and the Session/Channel/Backend contracts
  a router would be built on, not the router itself.
- **mTLS wiring and cert-as-credential meaning.** `PeerIdentity` is
  surfaced; resolving it into a principal is the later mTLS
  integration (Transit + Cert-store + Gatehouse-core), per
  `01-build-order.md`'s Layer 2 table — not decided here.
