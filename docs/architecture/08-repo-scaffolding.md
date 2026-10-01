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

```text
archipelago/
  go.work                    # local dev workspace only; each module still
                              # builds/tests standalone, same as Lighthouse's
                              # per-module convention

  docs/
    architecture/             # this doc set

  logging/                   # Layer 0 — own module, zero Archipelago-internal
                              # deps. Severity levels, structured attributes,
                              # trace/span/Resource types, sink interface.

  db/                        # Layer 0 — own module, zero Archipelago-internal
                              # deps. Connection/pool + migration runner that
                              # every base's storage layer depends on.

  gatehouse-core/             # Layer 1 base — own module
    structure/                 # principals, credentials, grants: types only
    evaluation/                 # authority checks; depends on structure +
                                 # the Store interface, never a concrete impl
    storage/
      dbstore/                   # today's Store implementation; depends on db/

  policy/                     # Layer 1 base — own module, same shape
    structure/
    evaluation/
    storage/
      dbstore/

  certstore/                  # Layer 1 base — own module, same shape
    structure/
    evaluation/
    storage/
      dbstore/

  alias/                      # Layer 1 base — own module, same shape
    structure/
    evaluation/
    storage/
      dbstore/

  transit/                    # Layer 1 base — own module; no structure/
                               # evaluation/storage split (not obviously the
                               # same shape — see 02-package-boundaries.md)
    wt/                          # WebTransport backend
    ws/                          # WebSocket backend
    envelope/                    # generic propagation envelope, trace-context field

  integrations/                # Layer 2/3 — packages, not separate modules,
                                # to start (see below)
    mtls/
    peerauth/
    credcert/
    sessions/
    aliasauth/
    sso/
    multiinstance/
    status/
    endpoint/
    adminkey/
    traceagg/

  sdk/                         # own module — the composed, ergonomic surface
                                # most apps import; depends on every base
                                # module plus the integrations packages

  cmd/
    archipelago/                 # the CLI (key enrollment, alias admin, …);
                                  # own module, depends on sdk/

  tools/
    archlint/                    # go-arch-lint config / go-list-based CI
                                  # boundary check
```

Each Layer 1 base directory is its own `go.mod`, module path
`github.com/croge130/archipelago/<name>` — the standard multi-module
monorepo shape, same as Lighthouse's `pkg/*`.

## Integrations start as packages, not modules

Every Layer 2/3 integration (`mtls`, `peerauth`, `sso`, `aliasauth`, …) is
its own *package* under `integrations/`, imported by whatever needs it —
but not its own *module* yet. A module boundary is cheap to add later
(move a directory, add a `go.mod`) and expensive to guess correctly now;
promoting one to its own module only makes sense once something concrete
wants, say, `mtls` + `transit` + `certstore` without pulling in the rest
of `integrations/` or the `sdk` module's own dependency weight. Until
that's a real case rather than a hypothetical one, keeping them as
packages avoids a module-per-integration explosion for what are mostly
small glue packages.

## Why the SDK module doesn't defeat "import only what you need"

The `sdk` module depends on every base and bundles the integrations —
that's the point of it: it's the "I want the ergonomic, composed
experience" entry point, not the minimal-footprint one. An app that
genuinely wants only raw `transit`, or only `alias` with no authority
model at all, imports that base module directly and never touches `sdk`.
Both are the same underlying code, offered at two different levels of
composition — nothing about one being convenient requires the other to
stop being minimal.

## What's decided here vs. still open

Decided: the module boundary sits at the base level: the
structure/evaluation/storage sub-layout for the four bases it applies to;
integrations as packages rather than modules, for now. **Still open,
deliberately:** the exact internal decomposition of any base's
`evaluation/` or `storage/` package once real code exists — e.g. whether
`gatehouse-core/evaluation` itself splits into principal-evaluation and
grant-evaluation sub-packages is a call for when there's a real evaluator
to look at, not something to pre-guess from a directory tree.

## Enforcement

The `go list`/`go-arch-lint` check named in
[`02-package-boundaries.md`](02-package-boundaries.md) belongs in
`tools/archlint/`, run in CI against the module set above — asserting,
concretely, that no base module's `go.mod` ever gains a `require` line
for another base module, and that only `integrations/*` and `sdk` import
more than one base at a time.

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
