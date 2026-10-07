# The SDK: a composition root, not a second API

## Where this sits

[`08-repo-scaffolding.md`](08-repo-scaffolding.md) names `sdk/` as the
composed, ergonomic entry point — "the module most apps would import" —
and says it depends on every base and integration while leaving each of
those independently importable. [`00-overview.md`](00-overview.md)'s
thesis is that every capability is "a mode of the same SDK." Neither says
what the SDK *contains*. This doc pins that down against what the
modules actually look like today, and is deliberately smaller than the
phrase "the SDK" suggests.

## What an app has to do today, without it

Every base and integration is built and tested, and each is usable on
its own. Assembling several of them into a real app means repeating the
same wiring by hand. Today the only storage implementation is
Postgres-backed, so that wiring looks like:

1. Open a `db.Pool`, collect `Migrations()` from each base's
   `storage/dbstore` (gatehouse-core, policy, certstore, alias, vitals —
   each keyed by its own module name, so they don't collide), and call
   `ProvisionSchemas` once.
2. Construct a `PostgresReader`/`PostgresWriter` pair per base.
3. Hand the right store to each integration's own narrowly declared
   interface (`peerauth.Store`, `certcred.GatehouseStore`,
   `sso.PrincipalStore`, `sessions.Store`, …).
4. On every startup, run the idempotent seeding steps:
   `aliasauth.RegisterPermissions`, `vitalsauth.RegisterPermissions`,
   `vitals/facade.SeedBuiltinDefinitions`,
   `vitalsdefaults.EnsurePolicyDefinition`.

Steps 1 and 2 are *the DB-backed way of obtaining stores*, not something
every node does. Steps 3 and 4 are storage-agnostic — they only ever
see `Reader`/`Writer`/`Store` interfaces. **That split is the key design
constraint on the SDK**, because a node with no direct database access
at all is an expected future shape, not an edge case
([`02-package-boundaries.md`](02-package-boundaries.md) names the
remote-intermediary and direct-read/remote-write `Store` implementations;
[`03-multi-instance-and-suites.md`](03-multi-instance-and-suites.md)
covers the asymmetric topology). Those implementations don't exist yet —
"not yet", not "never" — but the SDK must not make them impossible.

## A checked fact the design rests on

The "consumer declares its own thin interface" rule
([`04-facades-and-ergonomics.md`](04-facades-and-ergonomics.md)) was
chosen so one concrete store could satisfy many consumers. That's worth
confirming rather than assuming, because the whole SDK shape depends on
it. A compile-time probe (not committed — it would only duplicate what
the SDK's own build will assert) confirmed that gatehouse-core's single
`PostgresReader` satisfies `evaluation.Store`, `peerauth.Store`,
`certcred.GatehouseStore`, `sso.PrincipalStore`, and `facade.Reader`; its
`PostgresWriter` satisfies `sessions.Store` and `facade.Writer`; and
certstore's and alias's reader/writer pairs satisfy their own consumers
the same way. So the SDK holds **one reader/writer pair per base**, not
one adapter per integration — and, since each base's `facade.Reader`/
`facade.Writer` is an interface, nothing about that claim requires the
pair to be Postgres-backed.

## What `sdk` is

Two layers, split exactly along the line above:

```go
// Storage-agnostic core: given stores, wire and seed.
type Stores struct {
    Gatehouse struct{ Reader gatehouseFacade.Reader; Writer gatehouseFacade.Writer }
    Policy    struct{ Reader policyFacade.Reader;     Writer policyFacade.Writer }
    // ... Alias, Certs, Vitals — each optional, per Modes
}

func New(ctx context.Context, stores Stores, cfg Config) (*App, error)

// DB-backed provider: the one storage implementation that exists today.
func OpenDB(ctx context.Context, dbCfg db.Config, modes Modes) (Stores, *db.Pool, error)
```

`New` is the real composition root: it validates that the stores a mode
needs were supplied, runs the idempotent seeding steps (4 above), and
returns an `App` whose exported fields are those stores — typed as the
**interfaces**, never as `*PostgresReader`. `OpenDB` is a convenience
that does steps 1 and 2 and hands back `Stores`; an app that wants the
common single-node case calls `New(ctx, stores, cfg)` with its result. A
node with a different storage arrangement skips `OpenDB` and supplies
`Stores` from wherever its reads and writes actually go. Step 3 stays the
caller's call, made against the exposed fields.

**Read-only nodes.** Seeding is a *write*, and in the asymmetric topology
only the designated writer should perform it. So `Config` carries an
explicit `SkipSeeding` (or, equivalently, `New` treats a `Stores` with a
nil `Writer` as read-only and seeds nothing). A read-only node still gets
the full `App` for evaluation and reads; calling a write path on it fails
at the missing `Writer`, loudly, rather than being silently absent. The
SDK doesn't invent the policy for *which* node writes — that's the
caller's topology decision, the same way `03` leaves it.

```mermaid
flowchart TB
    subgraph PROVIDERS["Where Stores come from (caller's choice)"]
        DBP["OpenDB: db.Open → collect Migrations() →<br/>ProvisionSchemas → Postgres Reader/Writer pairs<br/>(exists today)"]
        REMOTE["Remote-intermediary or direct-read /<br/>remote-write stores<br/>(not yet built — same interfaces)"]
    end
    STORES["Stores — one Reader/Writer interface pair per base"]
    NEW["sdk.New(ctx, stores, cfg)"]
    CHECK{"Writer present<br/>and seeding not skipped?"}
    SEED["Idempotent seeding, gated by Modes:<br/>RegisterPermissions, SeedBuiltinDefinitions,<br/>EnsurePolicyDefinition"]
    NOSEED["Read-only node: skip seeding,<br/>writes fail at the missing Writer"]
    APP["*App — exposes the stores as interface-typed fields"]
    CALLER["Caller passes app.Gatehouse.Reader etc.<br/>to whichever integrations it uses"]

    DBP --> STORES
    REMOTE -.-> STORES
    STORES --> NEW --> CHECK
    CHECK -- "yes" --> SEED --> APP
    CHECK -- "no" --> NOSEED --> APP
    APP --> CALLER
```

## What `sdk` deliberately is *not*

- **Not a pass-through facade.** `AdvertiseEndpoints`, `peerauth.Require`,
  `vitalsauth.WriteReading` and the rest are already the ergonomic
  surface; `App` doesn't re-wrap them as `app.WriteReading(...)`.
  `registry/doc.go` already made this decision once, explicitly, for the
  same reason: a wrapper with no behavior of its own is indirection, and
  a second place for an argument list to drift. The SDK adds *wiring*,
  never a parallel API for something that already has one.
- **Not a lock-in.** An app that wants only `transit`, or only `alias`,
  imports that module and never touches `sdk` — `08` already says this.
  Nothing `App` holds is reachable only through it.

## Modes

[`00-overview.md`](00-overview.md) describes registry, SSO-provider, and
coordinator behavior as "the same SDK with a few more modes turned on."
Concretely, a mode gates (a) which `Stores` entries `New` requires, (b)
which seeding steps run, and — only for `OpenDB` — (c) which migrations
run. It's never a reason for the core to touch a database itself:

| Mode | What turning it on does |
|---|---|
| (always) | gatehouse-core stores, permission registry |
| Policy | policy stores; required by Vitals' default-group resolution |
| Alias | alias stores + `aliasauth.RegisterPermissions` |
| Vitals | vitals stores, `vitalsauth.RegisterPermissions`, `SeedBuiltinDefinitions`; with Policy also on, `EnsurePolicyDefinition` |
| Certs | certstore stores; required for mTLS identity and SSO |
| SSO provider | needs a ticket-signing `certstoreEvaluation.Signer` supplied in `Config` — the SDK never generates or stores the key itself, matching `12-sso-tickets-model.md`'s "never the mTLS key" rule |

Registry/leases need no mode of their own: `Instance` and `Lease` are
gatehouse-core records, always part of its stores.

## What stays in-process until a Router exists

Several things an app would reasonably want to call *remotely* are, today,
ordinary Go functions: `AdvertiseEndpoints`
([`15`](15-endpoint-advertisement-model.md)), `traceagg.Collector.Ingest`
([`16`](16-trace-log-aggregation-model.md)), and the receiving half of SSO
delivery. [`11-transit-model.md`](11-transit-model.md) lists a Router /
dispatch layer above `Backend.Accept` as deliberately undesigned, and
`Session` has no generic receive method — concrete backends do, which is
why tests call them directly.

This matters for the SDK's scope: **it can't give these a wire surface
without inventing the Router as a side effect**, which is a Transit-level
design in its own right (message-type → handler registration, per-handler
auth via `peerauth.Require`, how it composes with trace-context
propagation). So `sdk` v0 exposes them as the in-process functions they
are. A Router, when designed, is its own doc and module; the SDK then
gains one more field on `App` and a registration step, and
`AdvertiseEndpoints`/`Ingest` become one-line handlers on top — no new
logic, as `15` and `16` already say.

## Not yet built, and not this pass

Kept as confirmed-but-deferred rather than dropped, per the project's
own distinction between "not yet" and "never":

- **Login primitives** (`verifyPassword`, `verifyOTP`, `doPasskeyCeremony`,
  `completeSession`) from `00-overview.md` §6. The credential *kinds*
  exist in gatehouse-core's structure; no verification logic does. The
  SDK would host these once they exist; they are not SDK-shaped work
  until then.
- **OIDC relying-party mode and JWKS/`.well-known` discovery** (`00` §6).
  Same status.
- **A CLI** (`cmd/archipelago`, `08`). Depends on `sdk`; nothing to wrap
  yet.
- **Policy-gated mTLS auto-provisioning**
  (`01-build-order.md`'s Peer authorization row). Needs `Policy` wired
  into `peerauth`'s caller; once `sdk` exists it is the natural place
  for that wiring, but the design for it isn't done.

## Build order for the module

1. `sdk/` module scaffold + `Stores`/`Config`/`Modes`/`New`, with seeding
   gated by modes and by write access. No `db` import in this layer —
   enforce that with the module's own `go.mod` (the DB-backed provider
   lives in a separate package, or a separate module, so a node that
   never touches it never `require`s `db` or pgx).
2. `OpenDB` — the Postgres provider.
3. An integration test against real Postgres that builds an `App` twice
   in a row (proving startup seeding is idempotent end to end), runs one
   real cross-module flow through the exposed fields — register a
   permission, `EnsurePrincipal`, grant, then `vitalsauth.WriteReading` —
   and also builds a read-only `App` (nil `Writer`) over the same data to
   prove reads work and seeding is skipped. So the SDK's tests exercise
   composition and the no-direct-DB seam, not each base's own behavior
   again.
