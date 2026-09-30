# Package boundaries

This is the Go-package-level expression of
[`01-build-order.md`](01-build-order.md)'s dependency graph. The two
documents should stay consistent — if a build-order dependency changes,
this doc's import rules change with it.

**A caveat that applies to everything below:** "gatehouse-core", "policy",
"certstore", and "transit" are names for dependency units — groupings by
the property of not needing the other groupings — not promises that each
one is literally a single Go package. `gatehouse-core` in particular
(principals, credentials, grants, evaluation, sessions, multiple auth
providers) is large enough that it's very likely several packages
internally, related to each other by the same base/integration reasoning
one level down. That internal decomposition isn't decided here — it's a
call to make once real code exists, not something to lock in from a
diagram. What *is* fixed at this level: whatever `gatehouse-core` turns
out to be made of, none of it imports anything belonging to `policy`,
`certstore`, or `transit`, and vice versa.

## One internal boundary that *is* decided: structure / evaluation / storage

Almost everything about `gatehouse-core`'s internal package count is
open. This one isn't, because it's not a stylistic call — it's the
precondition for a feature already committed to in
[`03-multi-instance-and-suites.md`](03-multi-instance-and-suites.md):
DB access eventually needs to be swappable for "talks to a remote
intermediary service" (indirect access) or split asymmetrically (reads
direct, writes through a designated service), without evaluation logic
or calling code changing at all. That's only possible if three concerns
are never collapsed into each other from the start:

- **Structure** — the plain data types (`Principal`, `Credential`,
  `Grant`, `Role`, `Group`, request/result shapes). No logic, no storage
  calls, nothing but definitions. Everything else depends on this; it
  depends on nothing.
- **Evaluation** — the logic that answers "does this principal have this
  grant." Depends on Structure for its types, and on a `Store`-shaped
  *interface* for reading/writing them — never on a concrete storage
  implementation directly.
- **Storage** — a concrete implementation of that `Store` interface.
  Depends on Structure. A local-DB implementation is the first one; a
  remote-intermediary-backed implementation, or one that's direct-for-reads
  and remote-for-writes, are later implementations of the *same*
  interface, not a reason to touch Evaluation.

The dependency points the opposite way from how it might read naturally:
Storage depends on Structure and implements the interface; Evaluation
depends on the interface, never on Storage. That inversion is what lets
Storage be swapped later without Evaluation or any calling code noticing.

Exactly how many packages this becomes, and what they're named, is still
open — this section fixes the *shape* of the boundary, not the package
layout around it.

```mermaid
flowchart LR
    STRUCT["Structure<br/>(types only)"]
    EVAL["Evaluation<br/>(logic, depends on Store interface)"]
    IFACE(("Store interface"))
    DBIMPL["Local DB<br/>(Store implementation, today)"]
    REMOTEIMPL["Remote intermediary<br/>(Store implementation, later)"]

    STRUCT --> EVAL
    EVAL -.depends on.-> IFACE
    STRUCT --> DBIMPL
    STRUCT --> REMOTEIMPL
    DBIMPL -.implements.-> IFACE
    REMOTEIMPL -.implements.-> IFACE
```

## Why package boundaries specifically, not just discipline

Go enforces visibility at the package boundary, not at any looser
convention: two things in different packages can only talk through
exported identifiers. That's a compiler-backed guarantee, not a style
preference — you cannot accidentally reach into another package's
internals the way you easily could if two pieces of logic shared one
package with everything mutually visible.

This is a deliberate departure from Lighthouse's own convention, not a
carry-over. Lighthouse's `authority_*.go` family was split by *file*
within *one* package on purpose — those subsystems were all part of one
tightly-coupled evaluator, never meant to be used piecemeal. Archipelago's
bases are meant to be genuinely separable, independently testable, and in
some cases independently importable by an app that wants only part of the
system. That's exactly the shape where real package boundaries — not just
file boundaries — pay for themselves.

## The rule

**Layer 1 bases never import each other, at the level of the base as a
whole.** No package belonging to `gatehouse-core` imports anything
belonging to `policy`, `certstore`, or `transit`, and the same holds in
every other direction between the four. This says nothing about how many
packages make up `gatehouse-core` internally, or how those internal
packages relate to each other — only that the boundary around the whole
base holds. This is checked, not just intended — see "Enforcing it"
below.

**Every Layer 2/3 integration is its own package that imports the bases it
needs.** It is never folded into one of the bases it combines. Concretely:
mTLS is not "transit, with gatehouse baked in" — it's a separate package
that imports both, and a system that wants raw peer identity with its own
authorization logic never has to pull in gatehouse-core's dependency graph
just because a convenience wrapper happened to live inside transit.

**Multiple integration packages can coexist for the same pair of bases.**
There is no single canonical "the" way to combine transit and
gatehouse-core. A convenience integration with opinionated defaults can
exist alongside a leaner one for systems that want different
authorization semantics on top of the same peer identity — neither is
privileged, and importing transit never implies importing either.

## What Go's compiler does and doesn't give you for free

It prevents **cycles** (`certstore` importing `transit` while `transit`
imports `certstore` simply won't build) and it prevents **reaching into
unexported internals** across a package boundary. It does **not** prevent
an unnecessary one-directional import — `gatehouse-core` *could* import
`transit` without creating a cycle, and the compiler would have nothing to
say about it even though it violates the rule above. That part is design
discipline, and discipline alone doesn't hold at scale.

### Enforcing it

- **`internal/` packages** restrict who's even allowed to import
  something, when the goal is "no one outside this module" rather than
  "no one, period."
- **A `go list`-based CI check (or a tool like `go-arch-lint`)** can assert
  a rule at the base level — "nothing under the `gatehouse/...` tree may
  import anything under `transit/...`" — rather than needing to name
  exact package paths, which is what makes this survive `gatehouse-core`
  turning out to be several packages rather than one. Worth setting up
  once the Layer 1 boundaries are real, not deferred indefinitely.

## Facades live inside their base, unless they need something extra

A simple, ergonomic API (`RequirePermission(ctx, "some.permission")`) that
just calls the real evaluator with sane defaults belongs inside whichever
package within `gatehouse-core` owns evaluation, as ordinary convenience
functions — no new package, no new dependency, regardless of how many
packages `gatehouse-core` ends up being internally. It only needs a
genuinely separate package if it pulls in something gatehouse-core itself
doesn't need (e.g., a convenience that also touches transit). Splitting
for its own sake, when
there's no real additional dependency to keep optional, just scatters one
coherent idea across files someone has to mentally reassemble — the same
"match the tool to the real shape of the problem" discipline as everywhere
else in this project, applied to package count instead of a technology
choice.

See [`04-facades-and-ergonomics.md`](04-facades-and-ergonomics.md) for the
facade-assumptions rules (parameter defaults vs. policy configuration,
and why they behave differently once more than one facade is in play).

## Diagram

```mermaid
flowchart TB
    subgraph GHCore["gatehouse-core (illustrative — not decided)"]
        direction LR
        GHP["principals"]
        GHC["credentials"]
        GHGR["grants"]
        GHE["evaluation"]
    end
    GHP -.-> GHE
    GHGR -.-> GHE

    subgraph Base["Layer 1 — no base imports another base"]
        direction LR
        GHCore
        POL["policy"]
        CS["certstore"]
        TR["transit"]
    end

    subgraph Integrations["Layer 2/3 — each imports only what it needs"]
        direction LR
        MTLS["mtls<br/>(transit + certstore)"]
        PEERAUTH["peerauth<br/>(transit + gatehouse-core + policy)"]
        SSO["sso<br/>(gatehouse-core + certstore + transit)"]
    end

    TR --> MTLS
    CS --> MTLS
    TR --> PEERAUTH
    GHCore --> PEERAUTH
    POL -.-> PEERAUTH
    GHCore --> SSO
    CS --> SSO
    TR --> SSO

    Note["No arrow ever points the other way:<br/>a base never imports an integration, and no base<br/>imports another base — regardless of how many<br/>packages a base turns out to be made of internally."]
```
