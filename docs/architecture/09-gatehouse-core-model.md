# Gatehouse-core model

## Where this sits

[`01-build-order.md`](01-build-order.md) and
[`02-package-boundaries.md`](02-package-boundaries.md) decided
Gatehouse-core's *external* shape (a Layer 1 base, DB only, no
dependency on any other base) and left its *internal* package
decomposition open until real code exists. Neither decided what a
Principal, Credential, Grant, Role, Group, Session, or Context actually
*is* — that gap is what this doc closes. Nothing here changes the
dependency graph; it fills in the base the graph was already drawn
around.

**A status table, not a false consensus.** The items below were arrived
at with very different amounts of actual discussion. Treat the
"confirmed" rows as decided and the "proposed" rows as a loose
first draft borrowed from Lighthouse's own model, offered for
correction, not as already settled:

| Concept | Status |
|---|---|
| Context (exact-match, type+id) | confirmed |
| Grant scope (global / context, not lighthouse/realm/context) | confirmed |
| No realm anywhere in this model | confirmed |
| No cross-tenant permissions, no first-class tenant concept | confirmed |
| Principal types, `app_client` dropped, `external` generalized | confirmed |
| Cross-app/shared-CA credential vocabulary stays open | confirmed (deferred on purpose) |
| Credential shape, method-vs-credential split | proposed |
| Session shape, session-vs-credential invariant | proposed |
| Role / Group / Template | proposed |
| Deny effect | proposed (staying design-only) |
| Authority level | proposed |
| Generations / freshness | proposed |

## Why no realm, concretely, not just as a slogan

Lighthouse needed a containment concept *inside* its authority model
because one Lighthouse instance served many apps. Archipelago doesn't
have that problem: each app (or suite, sharing one authority domain per
[`03-multi-instance-and-suites.md`](03-multi-instance-and-suites.md))
gets its own Gatehouse-core instance and its own database. "Which app's
data is this" is answered by which database you're connected to, not by
a column every row carries. Realm's job is absorbed by the architecture,
not replaced by a new concept.

This also settles a question that sounds unrelated but isn't: whether
Gatehouse-core needs an intra-app tenant/namespace concept, for an app
that itself serves multiple customer orgs. It doesn't, on purpose — that
was never what realms solved anyway (realms were inter-*app*, never
intra-app multi-tenancy), and giving it a first-class concept without a
real case driving it repeats the mistake sharding would have been (see
[`06-logging-and-observability.md`](06-logging-and-observability.md)'s
note on the same reasoning). An app that genuinely needs customer
isolation models it with a group per customer and customer-scoped
context IDs in its grants — ordinary primitives, no new concept.

## Principal

*Confirmed shape, adapted from Lighthouse §4.1/§5.*

Anything that can act. Types:

```text
user | agent | service_account | system | external | anonymous
```

`app_client` is dropped — it existed specifically for *other apps*
connecting to one shared Lighthouse, a relationship that doesn't exist
here. `external` is kept and generalized: it's what any identity this
app's own Gatehouse-core didn't mint becomes, once mapped to a local
principal with its own locally-decided grants — an SSO-asserted human
identity today, and later (deliberately deferred — see below) an
identity from a genuinely different app, authenticated some other way
entirely. One principal type covers both because the receiving app does
the same thing either way: it never grants authority to the assertion
itself, only to the local principal it maps the assertion onto.

```text
PrincipalRecord
- principal_id     uuid
- key               stable human-oriented key
- display_name
- type              user | agent | service_account | system | external | anonymous
- metadata          opaque, never authorized on
- created_at / ... 
```

## Credential — proposed

*Loosely adapted from Lighthouse §4.2, §8.2, §9–10. Not yet discussed in
depth; treat every kind name below as a placeholder.*

Lighthouse drew a real, worth-keeping distinction between **how a
principal first proves who it is** (an authentication *method*) and
**what's presented on the wire afterward to authenticate a request**
(the *credential*):

```text
methods:    password | totp | passkey | sso_assertion
credentials: session_token | mtls_certificate | system
```

This lines up with what's already decided elsewhere: local-first login
(password+TOTP, passkeys) via composable primitives
(`00-overview.md` §6) establishes identity; `completeSession()` is what
actually mints the credential (a session token) that gets presented
afterward. Cert-as-credential (the Layer 2 integration with certstore,
`02-package-boundaries.md`) is a second, independent way a request
authenticates — a certificate itself, not a token derived from one.

**Deliberately left open, per this conversation:** a future credential
kind for a principal whose proof came from a *different app entirely* —
possibly a certificate signed by some shared CA multiple apps trust,
possibly something else. The exact shape isn't decided. What matters now
is that the vocabulary isn't closed: a kind like `foreign_certificate`
slots in next to `mtls_certificate` later without touching Principal,
Grant, or Session shape at all, the same way Lighthouse reserved
`mtls_certificate`/`challenge_key` as vocabulary years before either had
a provider.

## Session — proposed

*Loosely adapted from Lighthouse §8. Not yet discussed in depth.*

The invariant worth keeping unchanged, because it has nothing to do with
realms: **a session record alone never authenticates a request; only
explicit credential material does.** A session is who/what is currently
authenticated or asserted; a credential is how a given request proves it
belongs to that session. Collapsing the two is exactly the escalation
this split exists to prevent.

```text
SessionRecord
- session_id
- principal_id
- credential_id?        present when a transport credential exists
- session_kind          ui | cli | agent | service | automation | federated
- authority_level       standard | elevated | recovery_access
- authentication_method
- asserted_by_principal_id?   set when this session's proof came from
                              somewhere other than local credentials
                              (SSO, and later, a foreign credential)
- metadata
- created_at / expires_at? / last_seen / revoked_at?
```

`app_user`/`app_service` (Lighthouse's kinds for "a session belonging to
another app's user, asserted in") don't translate by name, but the job
they did — flagging a session whose proof isn't first-party — still
matters here, so `federated` is proposed as the one kind covering both
today's SSO case and tomorrow's cross-app case.

## Context

*Confirmed, carried forward close to unchanged from Lighthouse §17.*

Exact-match only, deliberately — no hierarchy, no policy conditions, the
same restraint Lighthouse kept even after real production use:

```text
ContextType    type_key (namespaced, e.g. myapp.readinglist), description, lifecycle
Context        context_type + context_id, lifecycle
```

No owner/visibility dimension — Lighthouse's `owner: lighthouse | app |
realm` and multi-level visibility existed because of nested containment
that no longer exists. A Context here just identifies one specific
resource instance a grant can be scoped to (a specific reading list, not
"reading lists" as a class).

## Grant

*Confirmed shape.*

```text
Grant
- subject_type       principal | group
- subject_id
- target_type        permission | role
- permission_key     (xor role_id)
- scope              global | context
- context_type / context_id    (required iff scope = context)
- effect             allow
- status             active | revoked
- origin             builtin | manual | provisioned
- metadata           opaque, never authorized on
- created_by / timestamps
```

`scope` is two-way (`global`/`context`), not Lighthouse's three-way
(`lighthouse`/`realm`/`context`) — the direct, mechanical consequence of
one level of containment instead of two. `origin: provisioned` is what
Lighthouse called `app_managed`: a template or automation created this
grant, not a human clicking a button — still a meaningful distinction
here, just renamed since "app" no longer disambiguates anything (there's
only ever one).

## Role, Group, Template — proposed

*Loosely adapted from Lighthouse §18.2–18.4. Nothing about these three
is realm-dependent, so the carry-forward is closer to verbatim than
anything else in this doc — flagged as "proposed" only because it hasn't
been explicitly discussed, not because there's a known reason to change
it.*

```text
Role      = live bundle of permissions, with inheritance (cycle-checked,
            bounded expansion)
Group     = live bundle of principals; membership changes bump the
            principal-grant generation
Template  = an explicitly-applied creation recipe; never live authority —
            changing a template never silently changes what it already
            created
```

## Authority level — proposed

*Loosely adapted from Lighthouse §4.10, unchanged in substance.*

```text
standard | elevated | recovery_access
```

Nothing about this is realm- or app-specific; it's the active posture of
a session/request, independent of containment.

## Deny — proposed, staying design-only

Lighthouse never built deny evaluation — only `allow` exists as an
effect — because deny's specificity rules (subject, permission,
context, authority — which deny wins when more than one matches) need to
be deterministic and *explainable* before it's safe to ship, and that
explanation machinery was never built. That reasoning has nothing to do
with realms either; it carries forward unchanged. `effect` stays a
single-value field (`allow`) in the Grant shape above, not because deny
is rejected, but because it's not earned yet.

## Generations and freshness — proposed

*Loosely adapted from Lighthouse §4.13, and already implicitly assumed
by [`02-package-boundaries.md`](02-package-boundaries.md)'s note that
Policy's Evaluation layer does "resolution + generation-based caching."*

Monotonic counters (grant generation, policy generation) recorded at
evaluation time and compared for staleness, distinguishing *which*
generation went stale and by how much — avoids requiring a live query on
every permission check while staying honest about when a cached decision
might be wrong.

## What stays explicitly deferred

- **Cross-app / shared-CA credentials.** Vocabulary left open (see
  Credential above); mechanism not designed.
- **Context hierarchy and policy conditions on contexts.** Exact-match
  only for now, same as Lighthouse's own Context v0.
- **Deny evaluation.** Design-only until specificity/explainability
  rules exist.
- **Intra-app multi-tenancy.** No first-class concept; an app that needs
  it uses groups and context-scoped grants.
