# Logging, tracing, and observability

## Bedrock, not a later addition

Lighthouse's own logging (Logbook) predated the realm concept, which meant
realm-scoping had to be retrofitted into a structured-tag system that
wasn't designed with it in mind — the same shape of mistake as the
alias `realm_id NOT NULL` problem in
[`01-build-order.md`](01-build-order.md), just on the observability side
instead of the data side. Archipelago's fix isn't sequencing (build
logging after identity exists) — it's **genericity**: the field/attribute
set stays open-ended from day one, so whatever identity dimensions get
added later (an authority domain, a suite, anything not yet decided) are
just new attribute keys on existing log calls, never a migration.

That genericity is also why logging belongs in Layer 0, alongside the DB
schema, not as a Layer 1 base that waits its turn. It has fewer
dependencies than anything else in the system — not even the DB — and
every other base wants to use it for its own observability the moment
it's being built and tested. Delaying it just means everything logs to
`fmt.Println` temporarily and migrates later, which is its own version of
the same mistake: a real dependency that exists from the first line of
code, left undeclared.

## Two different things, easy to conflate

- **Operational logging** — severity-leveled, structured, ambient. Every
  base and integration calls into it directly, the way code calls into a
  standard library logger. No DB required to exist; a local file or
  stdout/journald is a perfectly good sink on its own.
- **Audit logging** — a record of *authority decisions specifically*
  (grants checked, sessions created, credentials verified). This is not a
  separate base. It's Gatehouse-core's evaluation logic calling the same
  operational-logging plumbing, with its own structured attributes
  (principal ID, grant checked, outcome) and, because authority decisions
  are the one category of log line that's genuinely worth querying later
  rather than just tailing, a DB-backed sink in addition to whatever
  general sink is configured. The distinction is about what's being
  recorded and how durably, not about a different logging system.

**Status, honestly:** the `logging` module itself is built (levels,
`Resource`, trace/span IDs, span links, the capture buffer), but
adoption is not. Of the bases and integrations, only `vitals` (a
`SpanContext` field on `Reading`) and `wire`/`traceagg` import it today;
`gatehouse-core`'s evaluator doesn't log at all yet, and the audit
sink described above doesn't exist. Both are confirmed, deferred design
— the audit record's field shape is in
[`09-gatehouse-core-model.md`](09-gatehouse-core-model.md)'s "Audit
shape" section, itself "not yet pinned to exact field names" — not
abandoned ideas.

## Standard terms, not invented ones

Lighthouse's Logbook used its own vocabulary for connecting related
events (`narrative`, `parent ID`, `related ID`). Archipelago adopts
OpenTelemetry's tracing vocabulary instead — it's the real standard, and
using it means Archipelago's logs are legible to, and pluggable into,
tooling that already exists (Jaeger, Grafana Tempo, Zipkin) rather than
requiring a bespoke viewer to be built before any of this is useful.

| Lighthouse term | Standard term |
|---|---|
| narrative | **trace** |
| span | span (already standard, unchanged) |
| parent ID | **parent span ID** |
| related ID | **span link** (the OTel term for a causal relationship that isn't strictly parent/child — batch fan-out, queue handoffs) |
| tag | **attribute** |

Trace and span IDs use the **W3C Trace Context** wire format
(`traceparent`/`tracestate`-shaped: 128-bit trace ID, 64-bit span ID, both
fixed-width hex) rather than a free-form string. The rigidity is the
point: it's what makes a trace emitted by Archipelago mergeable with
traces from anything else already speaking the same standard, for free.

## Origin: the Resource, not just the trace

A trace/span says *which logical operation* a log line belongs to. A
separate, standard concept — OTel calls it a **Resource** — says *what
produced it*, attached once per process rather than per event:

| Resource attribute | Meaning | Archipelago's existing name |
|---|---|---|
| `service.name` | logical service/app identity | already "app" |
| `service.instance.id` | one running copy of that service | already "instance" — matches the grouping concept in [`03-multi-instance-and-suites.md`](03-multi-instance-and-suites.md) |
| `process.pid` | the OS process backing the instance right now | new — distinguishes "same instance, but this is run #3 after a restart" |
| `host.name` | the machine the process ran on | new — infra-level correlation across co-located instances. Built as `host.name` only, filled from `os.Hostname()`; a stable `host.id` (e.g. `/etc/machine-id`) is left for a caller to set rather than guessed at |

**Shard is deliberately not on this list.** Sharding is a data-partitioning
concept (which subset of data this instance is responsible for), not an
identity-of-origin concept — an instance may or may not be a shard,
independent of whether it has a `service.instance.id`. Archipelago isn't
planning around sharding as a core SDK feature (it's the same deferred
multi-DB-reconciliation problem from `03-multi-instance-and-suites.md`,
just motivated by intra-tenant scale instead of cross-tenant
independence, and the `Store` interface already leaves room for a sharded
implementation later without deciding anything now). If a shard concept
is ever needed, it's one more optional Resource attribute alongside
instance and host — not a replacement for either, and not something to
design for before there's a real case driving it.

## Transit carries correlation for free

`wire.Message` (the envelope Transit actually ships — `type`, `payload`,
`id`, `kind`, `channel`, per [`11-transit-model.md`](11-transit-model.md))
carries three more optional fields: `TraceID`/`SpanID`/`ParentSpanID`
(hex, W3C Trace Context-shaped), additive exactly like `id`/`channel`
already are. Every message that crosses a process boundary through
Transit — peer traffic, status reports, SSO delivery, anything — *can*
carry correlation data this way, with nothing extra to remember to
instrument, as long as the caller sets it; propagation isn't automatic
or forced, the same "additive, opt-in" property `id`/`channel` have.

This is what makes the thing a coordinator/Viewer will eventually want —
stitching together events that happened across several instances into
one causal story — possible without a second, separate correlation
mechanism. It's one half of [`16-trace-log-aggregation-model.md`](16-trace-log-aggregation-model.md)'s
Layer 3 feature: that pass found this field hadn't actually been built
yet (this section once claimed it had), added it to `wire.Message`
plus a small opt-in capture buffer here in `logging`, and built the
actual cross-instance collector — genuinely new plumbing, not a
composition of pre-existing pieces the way Status/health aggregation
turned out to be.

## Severity

Lighthouse's severity scale — `trace`, `debug`, `verbose`, `info`,
`notice`, `warning`, `error`, `critical`, `fatal` — carries forward
unchanged. It doesn't map one-to-one onto OTel's severity numbers, but it
doesn't need to: OTel's severity model is an open 1-24 range with
*suggested* names, not a closed enum, so keeping names that split `debug`
from `verbose` (a distinction Lighthouse found genuinely useful — see the
project-level logging guidance) is compatible, not a deviation.

## Where this sits in the build order

Logging is Layer 0 bedrock, alongside the DB schema, used directly
(never through an integration package) by everything above it — see
[`01-build-order.md`](01-build-order.md). Log/trace aggregation into a
collective, stitched view is a Layer 3 feature, built once Transit's
envelope and the registry/grouping concept both exist, the same
dependency shape as Status/health aggregation.
