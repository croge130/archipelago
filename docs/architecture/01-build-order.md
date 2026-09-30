# Build order

The goal of this doc: separate what's genuinely a base capability — buildable
and testable entirely on its own — from what's an *integration*, which only
exists once two or more bases already do. Most of the apparent "everything
depends on everything" feeling dissolves once this split is made explicit;
what's left is a real, small set of "this has to exist before that"
constraints, not a loop.

## The rule for telling the two apart

A base needs nothing from the others to be real and testable — it does not
import another base's package, does not require a network connection to
another process to exercise its own logic, and its tests run against a
local DB (or nothing at all) alone.

An integration is the thing that happens once you want two bases to *know
about each other* — e.g., "only let this message through if the peer's
transport-verified identity resolves to a principal with a grant for this
action." That specific sentence needs Transit *and* Gatehouse-core *and*
Policy to all already exist. It does not need to exist for any of the three
to exist on their own.

**A node in this graph is a dependency unit, not a claim about internal Go
package structure.** "Gatehouse-core doesn't depend on Transit" is a
statement about the external boundary — whatever Gatehouse-core turns out
to be made of, none of it needs Transit to exist. It is not a claim that
Gatehouse-core is, or should be, exactly one package. Gatehouse-core in
particular — principals, credentials, grants, roles, groups, policy
evaluation, sessions, multiple auth providers — is large enough that it
almost certainly decomposes into its own internal sub-graph of packages,
using this same base/integration reasoning one level down (e.g., maybe
principals and grants don't need each other to exist, while evaluation
is itself an "integration" of both). That decomposition is deliberately
not decided here — it's exactly the kind of structural call that should
wait for real code, per
[`02-package-boundaries.md`](02-package-boundaries.md)'s note on the same
point, not be speculated upfront.

## Layer 0 — bedrock

**DB schema and connection.** No dependencies. Everything else assumes this
exists, the same starting point Lighthouse's own `ProvisionSchemas` used.

## Layer 1 — independent bases

These five do not depend on each other. Each is buildable, testable, and
individually useful with nothing else in this document existing yet.

| Base | What it is | Needs |
|---|---|---|
| **Gatehouse-core**¹ | Principals, credentials, grants, local authority evaluation, password/token auth | DB only |
| **Policy** | Policy definitions/instances/contexts, resolution, generation-based caching | DB only |
| **Cert-store / PKI** | CSR handling, CA signing, enrollment records | DB only (+ eventually a `Signer` backend — vTPM/HSM/YubiKey, decided later, swappable) |
| **Transit (raw)** | WT/WS backends, delivery classes, byte-level peer identity extraction (`PeerIdentity()`) | Nothing — moving bytes between two processes doesn't need a DB, Gatehouse, or certs |
| **Alias** | Table + name → opaque target value, with its own lifecycle (active/released), never load-bearing | DB only |

Transit's `PeerIdentity()` extraction is pure crypto against whatever cert
is presented — it can be tested with a throwaway self-signed cert long
before the real cert store exists. It *produces* an identity; it never
*interprets* one. That's what keeps it a base rather than pulling
Gatehouse-core in as a dependency.

**Alias is a core base this time, not a later addition, on purpose.** In
Lighthouse it arrived after Gatehouse's core already existed with
`realm_id NOT NULL` baked into the alias tables, and it took a whole
migration phase to relax that once a genuinely realm-less table turned
out to be real (Lighthouse's own built-in Manual document set). Building
it as a Layer-1 base from day one, with no realm — or anything else —
assumed mandatory, means that specific mistake can't recur; there's
nothing to migrate away from because nothing was ever assumed. It fits
the base shape naturally: resolving a name to a target is pure structure
and evaluation over storage, and doesn't need Gatehouse to exist any more
than Gatehouse-core needs Alias — *who's allowed to create or resolve* a
given alias entry is a Layer 2 integration with Gatehouse-core, same as
everything else that needs authorization layered on top of a base that
doesn't require it to function.

¹ Treat "Gatehouse-core" as a name for a dependency unit, not a promise
that it's one package. It's the most likely of the four to turn out to be
several packages internally (principals, credentials, grants, evaluation,
sessions as their own sub-graph) — see the note above and
[`02-package-boundaries.md`](02-package-boundaries.md).

## Layer 2 — pairwise integrations

Each of these needs exactly two Layer-1 bases, combined by a package that
depends on both — never by one base importing the other directly.

| Integration | Needs | What it adds |
|---|---|---|
| **mTLS** | Transit + Cert-store | Real cert-backed `PeerIdentity()`, not a throwaway test cert |
| **Peer authorization** | Transit + Gatehouse-core (+ Policy) | "Does this verified peer's identity resolve to a principal with this grant" |
| **Cert-as-credential** | Gatehouse-core + Cert-store | Only if the credential model treats a certificate as a credential type — a real coupling to decide on purpose, not an accident |
| **Sessions** | Gatehouse-core (+ Transit, for connection-bound sessions specifically) | AuthoritySession tied to a principal, optionally to a live connection |
| **Alias authorization** | Alias + Gatehouse-core | *Who's allowed* to create, resolve, or release a given alias entry — Alias itself resolves a name with no opinion on this; this integration is what a caller reaches for the moment it needs one |

## Layer 3 — compound features

These need Layer 2 integrations, not just Layer 1 bases directly.

| Feature | Needs | Notes |
|---|---|---|
| **SSO ticket issuance/verification** | Gatehouse-core + Cert-store (dedicated signing key, *not* the mTLS key) + Transit for delivery | Audience-bound, identity-only, short-lived |
| **Multi-instance coordination** (peer discovery, broadcast, locks) | Transit + Gatehouse-core (registry/grouping concept) | See `03-multi-instance-and-suites.md` |
| **Status/health aggregation** | Policy (aggregation-policy pointer) + Transit (propagation envelope) + the same registry/grouping concept | Group definition holds a policy pointer; each computed rollup snapshots the resolved value |
| **Endpoint self-advertisement** | Gatehouse-core (permission metadata declared at registration) + the registry | The thing that replaces a hand-maintained allowlist with a real source of truth |
| **Admin/destructive-action key enrollment** | Cert-store (CSR+CA signing) + an out-of-band confirmation path (CLI) | Deliberately *not* gated by the same signed-request mechanism as app-level dangerous actions — machine access to the backend already implies broader trust than that mechanism would add |

## Layer 4 — deployment roles

Pure configuration/composition of everything above. No new code, no new
package. This is why "multiple coordinators" and "any instance can be
designated coordinator" were cheap: there was never a separate thing to
build for them.

- **Coordinator mode** — registry + optional SSO-provider mode, turned on
  in an otherwise ordinary SDK instance.
- **Ingress gateway** — external infra (Caddy/Traefik), controlled via
  their own APIs. Not built by us at all.
- **Viewer** — a generic client consuming the registry, SSO, and whatever
  authority a given app grants it. Web-based, no special coordinator
  access of its own.

## The dependency graph

```mermaid
flowchart TB
    DB[("Layer 0: DB schema")]

    subgraph L1["Layer 1 — independent bases"]
        direction LR
        GH["Gatehouse-core"]
        POL["Policy"]
        PKI["Cert-store / PKI"]
        TR["Transit (raw)"]
        ALIAS["Alias"]
    end

    DB --> GH
    DB --> POL
    DB --> PKI
    DB --> ALIAS

    subgraph L2["Layer 2 — pairwise integrations"]
        direction LR
        MTLS["mTLS"]
        PEERAUTH["Peer authorization"]
        CREDCERT["Cert-as-credential"]
        SESS["Sessions"]
        ALIASAUTH["Alias authorization"]
    end

    TR --> MTLS
    PKI --> MTLS
    TR --> PEERAUTH
    GH --> PEERAUTH
    POL -.-> PEERAUTH
    GH --> CREDCERT
    PKI --> CREDCERT
    GH --> SESS
    TR -.-> SESS
    ALIAS --> ALIASAUTH
    GH --> ALIASAUTH

    subgraph L3["Layer 3 — compound features"]
        direction LR
        SSO["SSO tickets"]
        MULTI["Multi-instance coordination"]
        STATUS["Status aggregation"]
        ENDPOINT["Endpoint advertisement"]
        ADMINKEY["Admin key enrollment"]
    end

    GH --> SSO
    PKI --> SSO
    TR --> SSO
    MTLS --> MULTI
    PEERAUTH --> MULTI
    POL --> STATUS
    TR --> STATUS
    MULTI -.shared grouping.-> STATUS
    GH --> ENDPOINT
    PKI --> ADMINKEY

    subgraph L4["Layer 4 — deployment roles (config only)"]
        direction LR
        COORD["Coordinator mode"]
        GATEWAY["Ingress gateway (external)"]
        VIEWER["Viewer"]
    end

    ENDPOINT --> COORD
    SSO --> COORD
    MULTI -.-> COORD
    COORD --> VIEWER
    SSO --> VIEWER
```

## What this buys, concretely

Nothing above requires the "everything eventually touches everything"
feeling to be resolved before starting. Build order:

1. **Layer 0 + Layer 1, in any order or in parallel.** Each is a standalone
   Go package (or module), tested against a local DB (or nothing, for raw
   Transit) with zero cross-imports. This is the actual place to start.
2. **Layer 2, once the two bases it needs exist.** Still small, still
   testable without the rest of the system running.
3. **Layer 3**, pulling Layer 2 pieces together into the features that are
   actually user-visible.
4. **Layer 4** last, and it costs nothing new — it's a configuration
   decision on top of software that already exists.

Anything that feels like a loop when reasoning about the *final, fully
integrated* system almost always isn't one in the *build* graph, once
bases are built against stubs/fakes for what they'll eventually integrate
with rather than against each other's final, integrated form.
