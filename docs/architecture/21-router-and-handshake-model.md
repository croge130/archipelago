# The Router and the handshake

**Status: steps 1 to 5 of the build order are built** — `wire`'s protocol
vocabulary, `transit`'s receive interface and typed channels, the
`router` base itself, `routerauth`, and the existing consumers (`traceagg`,
`registry`, `sso`) on routes, and the remote-executor routes of `20` — see
"Status" near the end. Not built: the discovery module (`18`). It resolves the gap that
[`11-transit-model.md`](11-transit-model.md) deliberately left ("a real
router sitting above `Backend.Accept` isn't designed here") and that
`15`, `16`, `18`, `19` and `20` each name as the thing they are waiting
on. It is grounded in the transport code as it stands, which turns out to
be missing more than a dispatch loop.

## What is waiting on this

1. `AdvertiseEndpoints` ([`15`](15-endpoint-advertisement-model.md)) is an
   in-process function with no way for a peer to call it.
2. `traceagg.Collector.Ingest` ([`16`](16-trace-log-aggregation-model.md))
   takes a message "however it arrived", and nothing receives one.
3. The executor's receiving side and the director's pushed delivery
   ([`19`](19-node-roles-and-resource-governance-model.md),
   [`20`](20-jobs-model.md)).
4. The remote-intermediary store implementation
   ([`02`](02-package-boundaries.md)), which is how a node with no database
   reaches a node that has one.
5. The version `hello` that [`18`](18-peer-discovery-and-capabilities-model.md)
   needs once multicast advertises a protocol version.
6. `registry.RegisterFromSession` and SSO delivery, which today are called
   by tests that reach into a concrete backend.

## What the transport actually lacks

Read from the code, not assumed. Each of these is a prerequisite, not a
detail.

1. **`transit.Session` has no receive side.** It has `Reply`, `Push` and
   `OpenChannel` — all outbound. `Next` and `AcceptChannel` exist on the
   concrete `inmem.Session` and `websocket.Conn` and are what tests call
   directly. A Router needs an interface for them.
2. **There is no client-side call.** `Reply` answers an exchange; nothing
   sends a request and waits for the response with the matching `ID`. A
   response arriving on a connection falls into the same inbound queue as
   everything else, so something has to demultiplex responses from
   requests. That something is the Router's other half.
3. **A channel carries no purpose.** `OpenChannel` sends a `stream_open`
   frame with only `Kind` and `Channel` — no `Type`, no payload. A Router
   cannot dispatch an accepted channel to the right handler without knowing
   what the channel is for.
4. **Stream control frames fail `Validate`.** `Message.Validate` requires a
   `Type`, and those frames have none. It works today only because encoding
   and decoding do not validate. Giving `stream_open` a `Type` (point 3)
   fixes the open frame; `stream_data`/`stream_end` should carry the
   channel's type too, or the Router should not validate them as messages.
5. **There is no handshake and no version.** `11` defers versioning until
   the first peer that cannot update in lockstep; advertising a version in
   multicast arguably is that peer.
6. **A full channel inbox stalls the whole connection.** In the websocket
   backend the single read loop delivers a channel's data into a bounded
   inbox (32) and, when it is full, waits. While it waits, nothing else on
   that connection is read — including a `cancel` frame for the very handler
   that is not draining the channel. The same applies to the 64-message queue
   of non-channel messages: the Router's loop must therefore drain it
   promptly and hand work off, never run it inline, and the channel case is
   a real head-of-line hazard that per-channel flow control (below) is
   meant to remove. Until then a slow channel consumer can wedge its
   session.
7. **Nothing defines what happens to an unknown type, a failed handler, a
   cancelled request, or a peer that sends faster than a handler runs.**

## Shape

```mermaid
flowchart TB
    NET["Backend.Accept → Session<br/>(inmem or websocket; one Backend per domain, per 18)"]
    PEER["router.Peer — one per session<br/>the receive loop: Next + AcceptChannel"]
    HELLO{"hello done?"}
    KIND{"message kind"}
    PENDING["Pending-call table<br/>response / error / ack → the waiting Call"]
    ROUTE["Route lookup by Type<br/>(RouteKey{Type, Version})"]
    AUTH["Route's authorizer<br/>(peerauth.Require, via routerauth)"]
    RUN["Handler runs under a derived context:<br/>child span, cancelled by a cancel frame<br/>or by the session ending"]
    REPLY["Handler's result → response, or error → coded error frame"]
    CH["Typed channel → channel handler by Type"]

    NET --> PEER --> HELLO
    HELLO -- "no: only transit.hello is accepted" --> KIND
    HELLO -- "yes" --> KIND
    KIND -- "response / error / ack" --> PENDING
    KIND -- "request / event" --> ROUTE
    KIND -- "stream_open" --> CH
    ROUTE -- "unknown" --> REPLY
    ROUTE -- "found" --> AUTH
    AUTH -- "denied" --> REPLY
    AUTH -- "ok" --> RUN --> REPLY
```

Three pieces, each in the layer its dependencies put it in:

1. **`transit` gains two small additions.** A `Receiver` interface
   (`Next`, `AcceptChannel`) that both backends already satisfy, and a
   `Type` (plus optional parameters) on `ChannelOpts`, carried in
   `stream_open` and exposed on the accepted `Channel`. No behavior of
   existing code changes.
2. **`wire` gains the protocol's own vocabulary:** the protocol version
   constants and their changelog (the contract clients need, as the
   Lighthouse plan already put it), the `hello` payload types, and the
   coded error payload and its codes.
3. **`router`, a new base** depending on `transit`, `wire` and `logging`
   and nothing else. It owns the registry of routes, the per-session
   `Peer`, response correlation, and the handshake. It knows nothing of
   principals, permissions, databases or domains.

## Routes

```go
type Handler func(ctx context.Context, req Request) (json.RawMessage, error)

type Route struct {
    Type        string
    Version     int           // 0 and unused for now; see "Versions"
    Handler     Handler
    Concurrency int           // 1 = sequential (the default); n = up to n at once
    Authorize   Authorizer    // optional; see routerauth
}

type Request struct {
    Message wire.Message
    Session transit.Session
    Peer    *Peer             // to call back, or push, on the same session
}
```

1. **A request returns a result or an error.** A returned result becomes
   a `response` with the request's `ID`; a returned error becomes a coded
   `error` frame with the same `ID`. An **event** has no `ID` and no
   reply: its handler's error is logged, not sent.
2. **The route is the ordering domain for non-channel messages.** By
   default a route handles one message at a time per session, in arrival
   order, which is the safe choice and matches Lighthouse's rule to land
   sequential dispatch first. A route opts into concurrency per route, not
   globally. A slow handler therefore blocks later messages to *the same
   route*, never other routes.
3. **A channel is the ordering domain for streamed messages,** unchanged
   from `11`: same channel in order, different channels independent. Never
   assume order across channels (`00` §7).
4. **The read loop never runs a handler inline.** It decodes, looks up,
   and hands off, so one stuck handler cannot stop a session from reading
   a `cancel` or a heartbeat.
5. **A per-session in-flight limit** bounds how many handlers run at once;
   beyond it a request is answered `busy`. This is the inbound analogue of
   the consent rule in `19`: a peer cannot make a node do unbounded work by
   sending faster than it can answer. How it ties to the node-wide
   governor is open.
6. **Cancellation.** A `cancel` frame naming a request `ID` cancels that
   handler's context; the session ending cancels all of them. Cooperative,
   as everywhere in this design: a handler that ignores its context is not
   killed.
7. **Unknown routes.** A request to an unknown `Type` is answered
   `unknown_route`. An unknown **event** is ignored (logged), because a
   newer peer may send events an older one does not know — the same
   forward-compatibility stance `18` takes for unknown core keys.

### Errors

A small closed set, on the wire as `{code, message}` in an `error` frame.
The message is for logs and humans and never carries internals.

| Code | Meaning |
|---|---|
| `unknown_route` | No route for that `Type` |
| `hello_required` | The session has not completed the handshake |
| `version_unsupported` | No protocol version in common |
| `unauthorized` | The route's authorizer refused; says nothing of *why* |
| `invalid` | The payload failed the handler's validation |
| `busy` | The in-flight limit is reached; retry later |
| `cancelled` | The request was cancelled |
| `internal` | The handler failed; details are logged on the handling side only |

`unauthorized` deliberately does not distinguish "no such grant" from "no
such thing", so a peer cannot probe what exists.

## Calling a peer

```go
func (p *Peer) Call(ctx context.Context, typ string, payload json.RawMessage) (json.RawMessage, error)
func (p *Peer) Push(ctx context.Context, typ string, payload json.RawMessage) error
```

1. `Call` sends a `request` with a fresh `ID`, registers it in the
   pending-call table, and waits for the matching `response` or `error`,
   the context, or the session ending. A coded error becomes a
   `*RemoteError` carrying the code, so a caller can branch on it.
2. **Symmetry.** Both ends of a session run a `Peer` with their own routes
   and can call each other. That is what the executor/director exchange
   needs — the director calls the executor, and the executor later pushes
   a result back.
3. A caller's context deadline is a **duration relative to receipt on the
   far side**, never an absolute time, per `19`'s decision on clock skew.

## Events are not acknowledged; what must not be lost is a request

**Decision (settles the former open question about `ack`): the Router sends
no acknowledgement for events, and none is added.** Nothing new was needed,
because a route's handler serves a `request` and an `event` alike, and a
request already gets everything an acknowledgement would carry: the handler
ran and succeeded, or a coded error says why it did not.

1. **`Push` is fire-and-forget by definition.** "Reliable" for the event
   delivery class means the *transport* does not lose it. It does not mean the
   receiver accepted it. A receiver over its in-flight limit drops the event
   and counts it (`Peer.DroppedEvents`); an unauthorized event is refused in
   the log only; a handler error on an event is logged only.
2. **Anything that must not be lost is sent with `Call`.** It gets `busy`
   when the receiver is full, `unauthorized` when refused, and a result when
   it ran. Same route, same handler, the sender picks. Job results
   (`jobs.result`) and anything else whose loss would matter use `Call`.
3. **`busy` is the only code worth retrying** (`RemoteError.Retryable`,
   `router.IsRetryable`): it says nothing about the request, only about the
   receiver's load. Every other code is a verdict on the request or the
   caller, a cancellation, or an internal failure whose retry safety only the
   operation can judge. The router does not retry on its own: a blind retry of
   a non-idempotent request is how work runs twice. That is why jobs fence
   completion on the attempt number (`20`): a retried `jobs.result` that
   arrives after the first landed is rejected as stale, not applied twice.
4. **Why not an acknowledged event class.** It would be a request that
   returns nothing, which `Call` already is, plus a second code path in the
   dispatcher and a third delivery state for senders to reason about. The
   cost it would remove is a sender having to write `Call` instead of `Push`
   for the cases that matter, and that choice is the honest place for the
   decision to live.
5. **Consequence for consumers.** Diagnostics (`traceagg.entries`) stay
   events: losing a batch under overload is acceptable and counted. A
   consumer that cannot tolerate loss uses `Call` and retries on `busy`.

## Typed channels

`ChannelOpts` gains `Type` and optional `Params`; `stream_open` carries
them; the accepting side's `Channel` exposes `Type()`. The Router
dispatches an accepted channel to a channel handler registered under that
type, with the same authorizer and in-flight accounting as a route. A
channel handler owns the channel until it returns or the channel closes.
This is what `19` and `20` mean by "a channel only for task kinds that
declare they stream."

## The handshake

The first exchange on a session is a `transit.hello` request from the
dialer, answered by the acceptor.

```mermaid
sequenceDiagram
    participant D as Dialer
    participant A as Acceptor (this domain's Router)

    D->>A: transport connects (mTLS identity already established)
    D->>A: request transit.hello {min, max protocol}
    A-->>D: response {chosen, max}  (or error version_unsupported)
    Note over D,A: only now are other routes reachable
    D->>A: request endpoints.list
    A->>A: authorizer: this peer's principal
    A-->>D: response — only what this peer may see
    D->>A: request jobs.run {...}
    A-->>D: response ack / rejected(busy | not offered)
```

1. **Before `hello`, only `transit.hello` is accepted.** Anything else is
   `hello_required`. After it, the negotiated version is fixed for the
   session.
2. **`hello` carries a protocol version range and nothing else of
   substance.** It reveals no more than the multicast announcement
   already does (`18`): that this speaks Archipelago and which versions.
   It is the pre-authorization step; nothing about the domain, the
   principal, the roles or the endpoints is exchanged in it. Those are
   learned after, through `endpoints.list` and the node-role claims, each
   filtered by the visibility policy `15` defines.
3. **Domain.** There is no domain field. Per `18`, each domain is served
   by its own listener presenting its own identity, so the handshake that
   reaches a given Router is already in that domain's context, and the
   connection's success or failure under mTLS is the domain check.
4. **Versions.** `wire` owns the version constants and a changelog.
   Following the Lighthouse plan's recorded model, routes are keyed by
   `RouteKey{Type, Version}` with `Version` left at 0 and unused, so adding
   versioned handlers later populates a field that already exists instead of
   rekeying the table. Protocol *editions* (the server-owned manifest
   mapping an edition to handler versions) stay deferred until a real
   version skew exists; what `hello` settles now is only that a mismatch
   is detected at the start and reported with a code, not discovered as
   garbled behavior later.
5. **An optional instance ID** may accompany `hello` as a logging hint
   only. It is an unverified claim, like every peer-asserted claim in `18`.

## Authorization and the endpoint registry

The Router has no notion of permissions. A route carries an optional
`Authorizer func(ctx, Session, Message) error`, run after `hello` and
before the handler; a refusal becomes `unauthorized`.

**The integration `routerauth` (router + Gatehouse-core) closes the loop
`04` opened.** Its registration function takes a route *and* the
permission it requires and does three things in one call: registers the
route, builds the authorizer from `peerauth.Require`, and registers the
matching `EndpointDefinition` ([`15`](15-endpoint-advertisement-model.md)).
Because one call registers all three, "which endpoints exist" and "what
each needs" cannot drift apart — which is the exact failure `04` records
for Lighthouse's `adminTokenMessageTypes`, now prevented structurally
instead of by discipline. `routerauth` also provides the `endpoints.list`
route itself, as a one-line handler over `AdvertiseEndpoints`, with the
visibility policy from `15`.

A node that wants raw routing with its own authorization uses `router`
alone; neither base is made to import the other's concerns.

Task kinds are **not** routed this way. Per `20`, a task kind is not an
endpoint; jobs reach an executor through a few fixed core routes
(`jobs.run`, `jobs.result`), not one route per kind.

## Domains

One `Router` per domain, per `App`. Handlers are closures over that
`App`'s stores, so a handler can reach nothing from another domain, and
the Router needs no domain concept at all. A node in two domains runs two
Routers, each over its own `Backend` (`18`'s listener-per-domain).

## Observability

For every handled message the Router derives a **child span** from the
message's trace context (`wire.Message.TraceContext`) and puts it in the
handler's context, so the handler's own logging joins the sender's trace
without the handler doing anything — the "receiving handler calls
`ContextWithSpan`" step `16` left to each caller becomes automatic.
Within a domain only: no implicit cross-domain propagation (`18`). Each
message logs the route, the peer's certificate fingerprint, the outcome
code and the duration, as structured attributes.

## What this changes in what exists

1. `traceagg.Collector.Ingest` becomes the handler of an event route; the
   tests that reach into a concrete backend for the message go through a
   `Peer`. *(Done.)*
2. `AdvertiseEndpoints` becomes callable by peers via `endpoints.list`.
   *(Done.)*
3. `registry.RegisterFromSession` and the SSO delivery become routes whose
   authorizer resolves the peer through `peerauth`. *(Done; SSO as
   self-issuance only, see Status.)*
4. The remote-executor half of `20` has somewhere to live. *(Done, as a
   pull through a director rather than a push: `jobs.pull`, `jobs.heartbeat`,
   `jobs.complete`, `jobs.fail`, `jobs.abandon`, `jobs.authorize`.)*

## Status

**Built:**

1. **`wire`** — `MinProtocol`/`MaxProtocol` with a changelog, `Hello` and
   `HelloReply`, `NegotiateVersion`, the closed set of error codes,
   `ErrorReply` and `DecodeError`. `Message.Validate` now accepts a channel's
   `stream_data`/`stream_end` frames identified by their `Channel` alone, and
   requires a `Channel` on every stream frame; every other message still
   needs a `Type`.
2. **`transit`** — a `Receiver` interface and a `Conn` that is both it and
   `Session`; `ChannelOpts.Type` and `Params`, carried in `stream_open`, and
   `Type()`/`Params()` on both ends of a channel. Both backends updated and
   asserted to satisfy `Conn`.
3. **`router`** — `Router` (routes and channel routes, registered once;
   duplicates, the `transit.` namespace and versioned routes are refused),
   `Peer` (`Accept` for a session this node accepted, `Connect` for one it
   dialed, `Call`, `Push`, `OpenChannel`), the handshake with its timeout,
   sequential-by-default routes with opt-in concurrency, the per-session
   in-flight limit, cooperative cancellation, panic recovery, coded errors,
   authorizer hooks, child spans, and typed-channel dispatch.
4. **`routere2e`** — a test-only module running the router over the real
   websocket backend, so `router` itself depends on no backend.
5. **`routerauth`** — a `Registrar` over one router and one domain's
   Gatehouse-core stores. `Handle` (and `HandleChannel`) registers the route,
   builds its authorizer from `peerauth.Require`, and writes the
   `EndpointDefinition` in one call, with the route key as the endpoint key.
   `HandleEndpointsList(filterByGrant)` registers `endpoints.list` over
   `facade.AdvertiseEndpoints`, and `ListEndpoints(ctx, peer)` is the client
   half. `router` gained `Route.Validate`/`ChannelRoute.Validate`, and
   `endpoints` joined Gatehouse-core's reserved namespaces.
6. **The existing consumers on routes.** `traceagg.Collector.Handler` and a
   `traceagg.Pusher` (which `*router.Peer` satisfies; `PushEntries` now takes
   it), `registry.RegisterHandler` and `registry.Register`, and
   `sso.IssueHandler` and `sso.RequestTicket`. Each consumer depends on
   `router` for the handler type and on nothing for authorization: a
   deployment passes the handler to `routerauth.Handle` with the permission
   it chooses.

**Tested:** the consumers end to end over real mTLS, a real websocket and real Postgres, with every route registered through `routerauth`: an authorised peer registers an instance bound to its own principal, has its pushed entries collected, and is issued a ticket a relying party verifies; a peer holding no grants is refused on every route and is told about the public endpoints only (and its pushed entries are not collected). Making the trace route public makes both tests fail. `routerauth` against real Postgres: an endpoint enforces exactly
the permission it advertises; an unregistered permission, a reserved
namespace, an invalid route or a duplicate leaves no half-registration; a
grant made after the session opened is honoured on the next call; each of an
admin, an unprivileged peer, an unknown certificate and a peer with no
identity sees only what it may call in `endpoints.list`; a typed channel is
authorized like a route. Removing the authorizer, or ignoring the visibility
filter, makes tests fail. `router` over the in-memory backend and, once, over a real websocket —
the handshake, a call, an unknown route, a typed channel with its params, a
cancel frame crossing the network, and a lost connection failing calls
instead of hanging them. Removing the handshake gate, or the panic recovery,
makes tests fail.

**Findings from building it, worth recording:**

1. **`Channel.Done()` fires on a local close only,** by design (channels are
   half-duplex). A peer that closes a channel is noticed through `Recv`
   returning `ErrChannelClosed`. The router closes a channel with no route
   or before the handshake, and the opener sees exactly that.
2. **A channel's close reason is not conveyed to the peer.** The router
   passes a reason (`unknown_route`, `busy`, …) to `Close`, but neither
   backend sends it, so the opener learns *that* the channel closed, not
   why. Cheap to add to the `stream_end` frame; not done.
3. **`Session` has no "send a request" method.** `Reply` answers and `Push`
   sends, and both just write a frame, so the router uses `Push` for
   everything a node initiates. The interface names a delivery class, not a
   direction, and this is where that shows.
4. **An event over the in-flight limit is dropped and counted** — it has no
   reply path to say `busy` on. `Peer.DroppedEvents` exposes the count.
   This is the honest consequence of a bounded limit with no
   application-level event acknowledgement, a trade accepted in "Events are
   not acknowledged": what must not be lost is sent with `Call`.

**Findings from moving the consumers:**

1. **An unauthorized event is silent.** A pushed event has no reply to refuse
   on, so the end-to-end test can only assert the absence of an effect (the
   collector stays empty) and the refusal is visible in the log alone. This
   is the second consequence of events having no application-level
   acknowledgement, after the in-flight drop above. It is accepted: a sender
   that needs to know uses `Call` (see "Events are not acknowledged").
2. **`sso.ticket.issue` issues for the caller only.** The existing design
   left the request side open; a self-issuing route is the one shape with no
   delegation question in it. A Viewer-style login (a user authenticated by
   something other than their certificate) is not served and is not
   designed. See `12`.
3. **Core permission names and reserved namespaces for these routes are not
   decided.** The consumers export route keys (`traceagg.entries`,
   `registry.register`, `sso.ticket.issue`) and leave the permission to the
   deployment. None of `traceagg`, `registry` or `sso` is in Gatehouse-core's
   reserved namespaces, so an app could today register its own key there.
4. **The first end-to-end test of any of these over a real backend** (real
   mTLS websocket, real Postgres, certificates bound to principals) is
   `routere2e`'s `consumers_test.go`. Making `PushEntries` take a `Pusher`
   instead of a raw session was the one breaking change, and the only
   callers were its own tests.

**Findings from `routerauth`:**

1. **Registration order is validate, then define, then route.** The router has
   no unregister, so the order is chosen so every failure that can be
   predicted happens before anything is written; the one failure left for the
   last step is a duplicate route, and then the definition is the one the
   other registration already holds (or the call fails with `ErrConflict`).
2. **A store failure during authorization is `unauthorized` on the wire.**
   The authorizer returns an error and the router refuses; the caller cannot
   tell a database outage from a denial. That is fail-closed and right for a
   security check, and the real reason is in the log, but a caller retrying
   an outage sees a permanent-looking refusal.
3. **`endpoints.list` under filtering shows an unknown peer the public
   endpoints only.** The nil principal holds nothing, so the same evaluator
   that gates calls gates visibility. With filtering off the list is
   everything, and a call still fails; seeing is not being allowed.
4. **Routes and channel routes share one endpoint-key space** because the
   registry has one. Registering both under one key is permitted only if
   their definitions match, and is best avoided.

## Not designed here

1. **Binding the in-flight limit to the node-wide governor** (`19`), and
   priority for inbound work.
2. **Event acknowledgement: settled** (see "Events are not acknowledged").
   The `ack` kind stays in the wire vocabulary, unused by the router.
3. **Protocol editions** beyond the cheap `RouteKey.Version` anticipation.
4. **A second backend's effect.** The Router should be backend-agnostic by
   construction (it sees `Receiver` and `Session`), but the websocket
   backend is the only network one, so this is untested against, say,
   WebTransport's native streams.
5. **Per-channel backpressure and credits** — the Lighthouse plan's stage 4.
   The current backends bound a channel's inbox, and the websocket backend
   stalls its whole read loop when one is full (see above); richer flow
   control that removes that is not designed, and until it exists a slow
   channel consumer can wedge its session.
6. **Backwards compatibility of the `stream_open` change** for any peer that
   is not this code. There is none today.
7. **Reconnection and session resumption.** A dropped session cancels its
   handlers and fails its pending calls; the layer above decides whether to
   redial.

## Build order

1. **`wire`:** protocol version constants, `hello` payload types, the coded
   error payload and codes.
2. **`transit`:** the `Receiver` interface; typed channels
   (`ChannelOpts.Type`, `Channel.Type()`); both backends updated, with a
   compile-time assertion that each satisfies `Receiver`.
3. **`router`:** `Router`, `Peer`, `Call`/`Push`, the handshake, dispatch,
   concurrency and the in-flight limit, cancellation, coded errors, typed
   channels. Tested over `inmem` first, then once over the websocket
   backend.
4. **`routerauth`** *(built)*: one-call registration (route + authorizer +
   endpoint definition) and the `endpoints.list` route.
5. **Move the existing consumers** *(built)* (`traceagg`, `registry`, SSO
   delivery) onto routes, which is also their first end-to-end test over a
   real backend.
6. Only then: the zero-dependency discovery module (`18`), and the
   executor/director routes (`20`).
