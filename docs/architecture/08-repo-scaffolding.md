# Repo scaffolding

This is where [`02-package-boundaries.md`](02-package-boundaries.md)'s
import rules become an actual directory layout and `go.mod` set, before
any real code exists. Deciding it now means the first lines of code have
something other than discipline enforcing the boundaries already
designed — a base's `go.mod` simply has no `require` line for a base it
isn't supposed to depend on, which is a stronger, more visible signal
than an import-path convention alone.

## Modules, not just packages, for Layer 1 bases

Go packages already give you "a consumer that doesn't import this code
doesn't compile it in." Separate **modules** buy something packages
don't: independent versioning (a `certstore` bugfix shouldn't force a
version bump on everything else), and a consumer only pulling in the
*transitive dependencies* of the bases it actually imports — a service
that only wants raw `transit` never resolves whatever crypto or DB driver
`certstore` needs, because it's never even in that consumer's module
graph, not just unused in its binary.

This is the same pattern Lighthouse's own `pkg/` already uses (`gatehouse`,
`typedvalue`, `livestore`, `buildinfo`, `shelltype` as separate small
modules) — carried forward and made the primary organizing structure
instead of a secondary one, because Archipelago's bases are meant to be
independently importable by third-party apps far more often than
Lighthouse's internal subsystems were.

**The cost, named honestly:** multi-module repos have real local-dev
friction — either `replace` directives in every consumer's `go.mod`
during development, or a workspace file. Archipelago's bases are being
actively co-developed right now (unlike Lighthouse's `pkg/` modules,
which were mostly independent by the time they existed), so a root
`go.work` resolves this for local development specifically — it has no
effect on how a published consumer resolves these modules, only on this
repo's own build/test loop. This is a deliberate difference from
Lighthouse's own choice not to have one, not an oversight; the two
repos' modules are at different points in their lifecycle.

## The layout

This section originally described a layout decided *before any real
code existed* — `integrations/` as one package tree inside a shared
module, a `tools/archlint/` CI check to police its boundaries. Neither
survived contact with actually building ten-plus integrations; see
"Integrations turned out to want modules too" below for what changed
and why. What follows is the layout as it actually exists, flat at the
repo root, one `go.mod` per dependency unit — base or integration,
no distinction in kind, only in which `require` lines each one has:

```text
archipelago/
  go.work                    # local dev workspace only; each module still
                              # builds/tests standalone, same as Lighthouse's
                              # per-module convention

  docs/
    architecture/             # this doc set

  logging/                   # Layer 0 — own module, zero Archipelago-internal
                              # deps (other than being imported by everything
                              # above it). Severity levels, structured
                              # attributes, trace/span/Resource types, an
                              # opt-in in-memory capture buffer for export.

  db/                        # Layer 0 — own module, zero Archipelago-internal
                              # deps. Connection/pool + migration runner that
                              # every base's storage layer depends on.

  typedvalue/                # Layer 0 — own module. Dimension/prefix/unit/
                              # Definition: typed-value metadata, no DB.
  typeconstraints/            # Layer 0 — own module, depends one-way on
                              # typedvalue only. Set/MergeMode/Merge.

  wire/                      # Layer 1 base — own module. The envelope
                              # itself (Message/Kind/DeliveryClass/channel
                              # ids, trace-context fields) — not nested
                              # inside transit; transit depends on it.
  transit/                   # Layer 1 base — own module, depends on wire.
                              # Session/Channel/Backend interfaces.
    inmem/                      # loopback backend — no sockets, for tests
                                 # and anything built on Session before a
                                 # real backend's socket concerns matter
    websocket/                   # the real network backend (coder/websocket)

  gatehouse-core/             # Layer 1 base — own module
    structure/                 # principals, credentials, grants, Instance/
                                 # Lease, Session: types only
    evaluation/                 # authority checks; depends on structure +
                                 # the Store interface, never a concrete impl
    storage/
      dbstore/                   # today's Store implementation; depends on db/
    facade/                     # Ensure*/Register*/Require* convenience
                                 # calls over evaluation + storage

  policy/                     # Layer 1 base — own module, same
                                 structure/evaluation/storage/facade shape
  certstore/                  # Layer 1 base — own module, same shape
  alias/                      # Layer 1 base — own module, same shape
  vitals/                     # Layer 1 base — own module, same shape
  jobs/                       # Layer 1 base — own module, same shape; the first slice
                                 # is built (20-jobs-model.md, "Status")

  # Layer 2/3 integrations — each its own module, same shape as a base's
  # (no module-vs-package distinction between the two; see below)
  mtls/                       # Transit + certstore
  peerauth/                   # Transit + gatehouse-core
  certcred/                   # gatehouse-core + certstore
  sessions/                   # gatehouse-core + Transit
  aliasauth/                  # alias + gatehouse-core
  sso/                        # gatehouse-core + certstore + peerauth + router (IssueHandler)
  registry/                   # gatehouse-core (Instance/Lease) + peerauth + router (RegisterHandler)
  vitalsauth/                 # vitals + gatehouse-core
  jobsauth/                   # jobs + gatehouse-core
  router/                     # transit + wire + logging; the dispatch layer (21)
  routerauth/                 # router + peerauth + gatehouse-core: route, permission and endpoint in one call
  governor/                   # none of ours; node-wide budgets, priority, pressure, bounded admission (19)
  discovery/                  # none of ours; mDNS announce/browse of a peer's port and protocol range (18)
  jobsexec/                   # router + logging: the executor side of remote jobs (20); no store, no gatehouse-core
  jobsdirector/               # jobsexec + jobsauth + routerauth: the director side
  routere2e/                  # tests only: the router and its consumers over real mTLS websockets
  vitalsdefaults/             # vitals + policy
  traceagg/                   # router + wire + logging (not registry
                                 # directly — see 16-trace-log-aggregation-
                                 # model.md for why)

  sdk/                         # built — storage-agnostic composition root
                                # (Stores/Modes/New/Seed); no dependency on
                                # the db module. See 17-sdk-model.md. Still
                                # the entry point most apps would import,
                                # per "Why the SDK module doesn't defeat
                                # 'import only what you need'" below.
  sdkdb/                       # built — OpenDB, the DB-backed provider of
                                # sdk.Stores; separate from sdk so sdk
                                # never requires db

  cmd/
    archipelago/                 # not yet built — the CLI; would depend on sdk/
```

Each dependency unit — base or integration — is its own `go.mod`,
module path `github.com/croge130/archipelago/<name>`. There's no
longer a structural distinction in *kind* between a base and an
integration's module; the only real difference is which other modules
appear in its own `require` list, exactly matching `01-build-order.md`'s
own framing of "a dependency unit, not a claim about internal structure."

## Integrations turned out to want modules too

The original plan (directly above, before this rewrite) kept every
integration as a *package* under one `integrations/` directory inside
a shared module, specifically to dodge "a module-per-integration
explosion" — reasoning that a module boundary is cheap to add later and
expensive to guess correctly now, so packages should be the default
until something concrete needed otherwise. Ten integrations later, that
reasoning didn't hold up against what was actually built:

- **The cost side of the trade never materialized.** Every integration
  module this project added was the same copy-paste-and-adjust
  `go.mod` (a handful of `require`/`replace` lines), and `go.work`
  erases the local-dev friction entirely — exactly the mechanism this
  doc's own "Modules, not just packages" section above already
  describes for bases. "Expensive to guess correctly now" was a real
  worry before any module had actually been added; it wasn't one in
  practice across ten real additions.
- **The benefit side was underweighted.** A package-in-one-module
  arrangement needs an external tool to stop `sso` from quietly
  importing `aliasauth` — this doc's own "Enforcement" section named
  `tools/archlint/` for exactly that job. That tool was never built.
  Under the actual module-per-integration layout, the same guarantee
  comes free from the compiler: an integration's `go.mod` simply has
  no `require` line for a module it isn't supposed to touch. The
  enforcement mechanism the original plan deferred to tooling, the
  actual layout gets for nothing.
- **Uniformity has its own value.** Every base is already its own
  module; giving every integration the identical treatment means the
  whole dependency graph in `01-build-order.md` is one shape throughout
  — "a dependency unit" — rather than bases being modules and
  integrations being packages-inside-something-else, two different
  answers to the same question depending which layer you're looking at.

This is a correction to match what was actually built and found
superior, not a plan still being evaluated — the ten existing
integration modules aren't getting consolidated back into a shared
tree. A future integration follows the same pattern: its own directory,
its own `go.mod`, `replace` directives for whichever bases (or other
integrations — `registry` depends on `peerauth`) it actually needs.

## Why the SDK module doesn't defeat "import only what you need"

The `sdk` module depends on the bases and integrations it composes —
that's the point of it: it's the "I want the ergonomic, composed
experience" entry point, not the minimal-footprint one. (Today that's
gatehouse-core, policy, alias, certstore, vitals and the three
integrations that seed them; transit-facing integrations aren't wired in
until a Router exists — see `17-sdk-model.md`.) An
app that genuinely wants only raw `transit`, or only `alias` with no
authority model at all, imports that base module directly and never
touches `sdk`. Both are the same underlying code, offered at two
different levels of composition — nothing about one being convenient
requires the other to stop being minimal. Nothing in the module-per-
integration correction above changes this design; `sdk` is now built, and
every integration module above remains independently importable exactly
the way this section always intended.

## What's decided here vs. still open

Decided: the module boundary sits at the dependency-unit level — every
base and every integration gets its own module, no distinction in
kind between them; the structure/evaluation/storage/facade sub-layout
for the four bases it applies to (`gatehouse-core`, `policy`,
`certstore`, `alias`, and now `vitals`). **Still open, deliberately:**
the exact internal decomposition of any base's `evaluation/` or
`storage/` package (e.g. whether `gatehouse-core/evaluation` itself
splits into principal-evaluation and grant-evaluation sub-packages is a
call for when that split is actually needed, not something to pre-guess
from a directory tree); and the `cmd/archipelago` CLI layer, named
above as intended but not yet built (`sdk` itself is built).

## Enforcement

The base/integration boundary enforcement `02-package-boundaries.md`'s
"Enforcing it" section and this doc's own original "Enforcement"
section both named a `go-arch-lint`-style CI check for is no longer a
gap needing a tool: the module graph itself is the enforcement — no
base or integration module's `go.mod` can gain a `require` line for
something it isn't supposed to depend on without that line being
visible, deliberate, and reviewable in a diff. A lint tool, if one is
ever built, would target a narrower, still-open concern this doesn't
cover: discipline *within* one module's own internal packages (e.g.
keeping `gatehouse-core/structure` from reaching into
`gatehouse-core/storage/dbstore`'s internals) — a real but smaller
question than the one `tools/archlint/` was originally scoped for.

## DB-backed tests across a module's packages: run serially

`go test ./...` runs each package's test binary as its own process, and
different packages' binaries run *concurrently* with each other by
default — fine when nothing shares state, not fine when multiple
packages' integration tests point at the same real test database.
Gatehouse-core's `evaluation`, `storage/dbstore`, and `facade` packages
all exercise the same `ARCHIPELAGO_TEST_DATABASE_URL` instance, and
running them in parallel produces exactly the failure this sentence is
here to prevent being re-diagnosed: two packages' tests racing to
`TRUNCATE` and re-insert the same row, surfacing as a spurious unique-
constraint violation that looks like a logic bug and isn't one. Lighthouse's
own `AGENTS.md` names the identical root cause for its own DB-backed test
packages (there, a schema-provisioning deadlock; here, a data race —
same shared-database-under-parallel-test-binaries cause). Run
`go test -p 1 ./...` whenever a module has more than one package with
DB-backed tests, the same fix for the same reason.
