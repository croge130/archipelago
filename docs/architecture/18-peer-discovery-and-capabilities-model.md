# Peer discovery, domains, and what a peer offers

**Status: design draft. Nothing here is built, and nothing should be built
from it until the open questions at the end are settled.** It was written
to pin the vocabulary and the boundaries before any implementation, in
the order the conversation that produced it reached them. Where it
revises an earlier statement, it says so.

## Where this sits

[`03-multi-instance-and-suites.md`](03-multi-instance-and-suites.md)
lists peer discovery as "registry-backed by default, optional SWIM-style
gossip fallback." [`13-registry-and-leases-model.md`](13-registry-and-leases-model.md)
built the registry half, but `registry.RegisterFromSession` only runs over
an already-authenticated Transit session, so it can't help a node find its
*first* peer. [`15-endpoint-advertisement-model.md`](15-endpoint-advertisement-model.md)
built "what does this peer expose, and what does each thing need" as an
in-process query. [`11-transit-model.md`](11-transit-model.md) defers
protocol versioning and a Router. This doc connects those pieces and adds
the one missing tier: how a node with no configured peer location finds
one.

## Vocabulary: "capabilities" is already taken

`transit.Capabilities` (`NativeStreams`, `Datagrams`) describes what a
*backend* can realize, and `11` is explicit that it is for logging and
realization choices, never for handler branching. It is local, never
negotiated, and unrelated to what follows. This doc uses **capability**
only in the sense below, and refers to the existing type as *realization
capabilities* when both appear together.

| Layer | Answers | Status |
|---|---|---|
| Realization capabilities | What can this transport do? | Built, local-only |
| Protocol version | Can we talk at all? | Not built (`11` defers it) |
| Roles, labels, capabilities | What does this peer do, and how do I select it? | This doc |

## The archipelago domain

An **archipelago domain** is one deployment: the set of nodes that trust
the same root(s) and share authority. A node may operate in more than
one, and **its roles, labels and capabilities are then independent per
domain** — the same process can be an SSO provider in one and a plain
worker in another, with different endpoints, different grants and
different signing keys.

### One App per domain

[`17-sdk-model.md`](17-sdk-model.md)'s `App` already has the right shape
for this: it is built from one set of stores, one set of modes and one
`Config` (including its own `SSOSigner`). A node straddling two domains
holds **two `App`s**, each over its own stores. Nothing about
roles/labels/capabilities needs a domain column; the domain *is* which
`App` you are talking to.

This is deliberate, and it is the project's own earlier lesson applied
again. Lighthouse's alias table had to retrofit `realm_id NOT NULL` after
the fact ([`01-build-order.md`](01-build-order.md)), and `00-overview.md`
§1 records how expensive cross-boundary identity got. Isolating domains by
construction — separate stores, separate identity — means a bug in one
domain's endpoint registry cannot leak into another's, because they never
shared a table.

*Not yet:* two domains deliberately sharing one database. That would need
a per-domain filter on the registries (the same shape as `registry`'s
`Group` scoping) and is not designed here. Nothing needs it today.

### Consequences of per-domain scoping

- **Endpoint registry** (`15`): per `App`, because it lives in that
  domain's Gatehouse store. A multi-domain node advertises a different
  set in each, with no extra mechanism.
- **Trace aggregation** (`16`): one `Collector` per domain. Stitching
  entries across domains would leak one domain's activity into another;
  it must not happen implicitly.
- **Trace-context propagation across a domain boundary** is an open
  question (below). The default must be *not* to propagate.
- **SSO signer, seeding, modes:** already per-`App` in `17`.

## Roles, labels, capabilities — and core vs. app-defined

Three distinct concepts:

| Concept | Meaning | How a claim is checked |
|---|---|---|
| **Capability** | Something the peer can do: an endpoint it serves, a protocol feature. | Call it; failure is the answer. |
| **Role** | A responsibility it holds. *Exclusive* roles (leader) are exactly the leases `13` already built; *non-exclusive* roles (SSO provider) are declared and imply a capability. | Exclusive: check the lease holder. Non-exclusive: check the implied capability. |
| **Label** | Free-form selector metadata (environment, zone). Never authorized on — the same rule `Principal.Metadata` and `Instance.Metadata` already follow. | Not verified. |

**Core vs. app-defined reuses the reserved-namespace mechanism.**
`gatehouse-core/facade`'s `defaultReservedNamespaces` already lists
`archipelago` plus one entry per base. A *core* role, label or
capability is a key under one of those namespaces; an *app-defined* one is
anything else. `RegisterPermission` and `RegisterEndpoint` already share
this map and its explicit override; no second naming mechanism is
introduced.

**The prefix means different things locally and on the wire.** `09`
describes the reservation as hygiene, "not a security boundary", because
an app already owns its own database. That reasoning ends at the network
edge: a remote peer claiming an `archipelago.*` key proves nothing, since
anyone can type it. So **every peer-asserted claim, core or app-defined, is
an unverified hint until checked** (the right-hand column above), and what a
verified claim is *worth* is decided only by the receiving node's own
Gatehouse — the same rule `00` §4 states for the coordinator.

**Forward compatibility.** The core set is closed, documented, and
versioned with the protocol. An unknown key in a core namespace from a
newer peer is ignored, not an error; an unknown app-defined key is just
data.

A node *adopting* a role, and the resource bounds that keep adopted
roles from overwhelming it, are designed in
[`19-roles-and-resource-governance-model.md`](19-roles-and-resource-governance-model.md).

*Open:* labels have no type today — only `Instance.Group` and opaque
`Metadata`. Selection like "peers with `env=prod`" needs a queryable shape,
so a small namespaced key/value type is likely warranted; not designed.

## Three discovery tiers

```mermaid
flowchart TB
    START["Node needs a peer in domain D"]
    CFG{"Explicit location<br/>configured for D?"}
    REG{"Registry/coordinator<br/>reachable and knows one?<br/>(00 §3, 13)"}
    MC["Multicast: find any archipelago peer on the<br/>local link (this doc) — a hint, not a fact"]
    CONNECT["Connect with D's identity;<br/>mTLS handshake is the real check"]
    NOPEER["No peer found"]

    START --> CFG
    CFG -- "yes" --> CONNECT
    CFG -- "no" --> REG
    REG -- "yes" --> CONNECT
    REG -- "no" --> MC
    MC -- "candidate found" --> CONNECT
    MC -- "none" --> NOPEER
```

Multicast is the **bootstrap** tier only. `03`'s SWIM gossip is a
different layer — ongoing membership after bootstrap — and is not
replaced or required by this.

## The multicast announcement: protocol version, and nothing else

The announcement says one thing: *"an Archipelago peer is here, and it
speaks protocol version N."* The location comes from the discovery
record itself; everything else is learned over the connection.

**Deliberately not in it:**

- **The domain.** Revises an earlier suggestion in this design
  conversation. The mTLS handshake already answers "is this peer in my
  domain" with a verified result — a peer from another archipelago fails
  the handshake because its cert doesn't chain to a root this node
  trusts — so announcing the domain would only add an unverifiable hint.
  A node in several domains has no clean single value to announce, and a
  passive LAN observer should not learn which deployments a node belongs
  to. The cost: a node on a LAN shared by several archipelagos will try,
  and fail, some connections to unrelated peers. Each is one cheap
  handshake, and negatives can be cached. An opt-in hint (truncated hashes
  of trusted roots) is possible later for deployments where that noise
  matters; it is not part of the first version.
- **Roles, labels, capabilities** — core or app-defined. App-defined ones
  would leak to the LAN and have no bounded size; core ones are
  per-domain, so there is no single set to announce. Both arrive
  post-connection, filtered by the same `filterByGrant` visibility policy
  `AdvertiseEndpoints` already uses.

**The announcement is a hint and is never trusted.** Anyone on the link
can forge one. Its only effect is to make a node *try a connection*; trust
is established by mTLS and the endpoint query afterwards.

**Independence.** Discovery must not depend on gatehouse-core or any
database, so a node with no direct database access (`17`) can announce and
discover. It would be a zero-dependency module, like `alias`.

**Likely carrier — to verify before any code.** DNS-SD over mDNS
(RFC 6762/6763), service `_archipelago._tcp`, with the version in TXT and
the port in SRV, rather than a custom packet. Things to check against the
real libraries first, per this project's usual discipline: mDNS is
link-local only and commonly blocked in cloud VPCs and Kubernetes pod
networks (acceptable for an optional fallback); TXT records are meant to
stay small; and the practical differences between `hashicorp/mdns` and
`grandcat/zeroconf`.

## The connection process, per domain

A multi-domain node must present the right identity to a caller without
the caller naming the domain in the clear.

**Revises an earlier suggestion.** Selecting a certificate by TLS server
name (`tls.Config.GetCertificate` sees `ClientHelloInfo.ServerName`) would
work mechanically, but in TLS 1.3 the server name is sent unencrypted
(absent Encrypted Client Hello), so a client naming a domain-derived
identifier would announce its domain to every observer on the link at
connect time — recreating, for real connections, the leak the announcement
was designed to avoid.

**Preferred: one listener per domain.** Each `App` owns its own
`transit.Backend` listener on its own port, presenting only that domain's
certificate. The multicast layer then lists several instances — each just
"Archipelago peer, version N, port P" with no domain — and the caller
simply tries them with the identity of the domain it is looking for.
There is no mux and no server-name convention to get wrong, and it
matches "one `App` per domain". The cost is more ports.

```mermaid
sequenceDiagram
    participant C as Caller (domain D)
    participant M as Multicast (link-local)
    participant L as Listener for D's App
    participant R as Router for D's App

    C->>M: query: any archipelago peers?
    M-->>C: instance list (version, port) — hints only
    C->>L: mTLS handshake using D's identity
    L-->>C: server cert for D (fails here if not in D)
    C->>R: hello (protocol version) — first message on the session
    R-->>C: hello (agreed version)
    C->>R: list endpoints (AdvertiseEndpoints, filtered by grant)
    R-->>C: only what C's principal may see
```

Everything after the handshake is that domain's `App` alone. Note the
`hello` and the Router are **not built**: `11` lists the Router as
deliberately undesigned and defers versioning until the first peer that
cannot update in lockstep. Advertising a version in multicast is
effectively that trigger, so the hello/versioning design and the Router
design should be done together.

## Not yet / open questions

1. **Router and `hello`.** Designed together, per above. Also what a
   handler can see: the `App` for its domain, and nothing from another.
2. **How a caller picks a domain's identity** when it has several `App`s
   and finds a peer through domain-less multicast — try each in turn, or
   remember which instance names belong to which domain once learned.
3. **Labels as a type** (above).
4. **Cross-domain trace propagation.** Default no; whether any explicit
   opt-in exists is undecided.
5. **Listener-per-domain vs. a single shared listener with some other
   domain selector.** The preferred option above has a real cost in
   ports; a shared listener needs a selector that does not leak the
   domain in the clear.
6. **Domain identity's concrete form** — "the root(s) a node trusts" is
   the working definition; how it is represented in config (and how a node
   enumerates its domains) is not designed. The announcement does not need
   it, which is why this can wait.
7. **Two domains sharing one database** (see "Not yet" above).
8. **The SWIM-style gossip fallback** `03` names: relation to this tier
   once membership beyond bootstrap is wanted.

## Suggested order

1. Settle this doc (the open questions above).
2. Router + `hello`/versioning design, domain-aware.
3. Zero-dependency discovery module (announce/query only).
4. The label type, if selection by label turns out to be needed.
