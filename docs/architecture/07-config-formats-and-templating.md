# Config formats and templating

## One principle: match the format to the content's shape

Archipelago uses three different formats on purpose, not by accident, and
not for lack of picking a favorite. The early instinct to keep everything
on one format "for toolchain consistency" was a mistake worth naming
explicitly: consistency across files that don't actually share a shape is
a superficial win that costs a real one — each format below was chosen
because the content it holds is shaped differently, not because one
format is generally better than the others.

| Format | Used for | Why this one |
|---|---|---|
| **XML** | Documents and dashboards | Real hierarchy + mixed content (structure interleaved with prose) that a line-oriented format handles awkwardly; explicit open/close tags mean nesting depth is never ambiguous from whitespace alone |
| **HCL** | Principals, roles, groups, permissions/grants — "ensure this exists" templates | A reference graph, not a document — what matters is validated cross-references between entries, and HCL's explicit braces give the same nesting-is-never-ambiguous property as XML without XML's verbosity for this shape |
| **TOML** | Flat operational settings — endpoints, ports, CRL/cert config, SDK-init options | Comments (unlike JSON) and no implicit type coercion (unlike YAML's classic string/bool/date ambiguity) matter more here than expressiveness, because there's no hierarchy to speak of |

## XML: documents and dashboards

This carries forward a concept from the Lighthouse design corpus largely
unchanged, even though ownership of the underlying subsystem has changed
— the documentation system was always meant to be genuinely interactive
(queried, cross-referenced, eventually editable) rather than static
markdown, and XML's explicit structure is what a DocBook/DITA-style
interactive system is built on. **This doc is about the format decision
only.** The interactive rendering/querying/editing system itself is a
separate, later subsystem and isn't designed here.

The security-relevant property worth restating: explicit open/close tags
mean a parser (or a person) can never be tricked about where a nested
element begins or ends the way significant-whitespace formats can be —
relevant here because dashboards and docs are exactly the kind of content
that mixes structure with free-form prose, where that ambiguity would
otherwise be easy to introduce by accident.

## HCL: existence templates, not a second way to write code

This is the templating idea from early in the design: give an app's SDK a
declarative file describing which principals, groups, roles, and
grants should exist, instead of requiring that bootstrapping to be
written as imperative code. An HCL template is a **caller of the same
facade API** described in
[`04-facades-and-ergonomics.md`](04-facades-and-ergonomics.md) — parsed,
then applied as the same `Ensure*`-style idempotent calls an app could
make in code directly. It is never a parallel system with its own
authority logic; the real evaluator doesn't know or care whether a given
`EnsurePrincipal` call came from a template file or from a line of Go.

Why HCL specifically, over XML or YAML, for this one shape:

- **Validated cross-references matter more here than in docs.** A role
  referencing a group that doesn't exist in the same file is a real error
  to catch before anything is applied, not a cosmetic one — this is
  fundamentally a reference graph (principal → role → group → grant),
  the same shape HCL was built for (Terraform's resource graph).
- **Go-native tooling already exists** (`hashicorp/hcl`) with no extra
  ecosystem to bring in, consistent with TOML's Go-idiomatic rationale
  below.
- **Idempotent application, not a scripting language.** Applying the same
  template twice should be a no-op the second time — this is "ensure",
  not "run" — the same semantics Lighthouse's own alias `ensure`
  operations already used, generalized to a whole file instead of one CLI
  call at a time.

An app is never required to use templates — they're an alternative entry
point into the same facade a hand-written bootstrap script already has,
useful specifically for the common case of "these principals/roles/groups
should exist" being more legible as data than as a sequence of function
calls. Code-based and template-based provisioning can coexist in the same
app; both end up calling the same evaluator underneath.

## TOML: flat operational settings

Endpoints, ports, CRL/cert configuration, SDK-init options — content
with no real hierarchy and no cross-references to validate, where the
two things that matter are comments (so a config file can explain itself,
unlike JSON) and the absence of YAML's implicit type-coercion footguns
(an unquoted `no` or a bare date-shaped string silently becoming the
wrong type). TOML is also already the Go ecosystem's own default for this
shape of content, consistent with HCL's Go-native rationale above.

## Node roles and jobs: planned extensions, none built

[`19-roles-and-resource-governance-model.md`](19-roles-and-resource-governance-model.md)
adds state that fits these formats, split by the same rule this doc already
states — match the format to the content's shape:

1. **HCL, "ensure this exists".** The store-resident side of node roles
   and jobs is the same kind of reference graph as principals, roles,
   groups and grants: the grants a node role's principal needs, the Policy
   values that configure a role (budget, schedule), and recurring job
   definitions, each of which refers to a task kind and a permission.
   A cross-reference error — a recurring job naming a task kind nobody
   registered — is exactly the sort of mistake HCL validation catches
   before anything is applied. These would apply through the same
   idempotent `Ensure*` path, not a new one.
2. **TOML, flat operational settings.** What a *node* adopts is not
   written to any store; it is read at startup, is node-local, and must
   work on a node with no database at all. That is SDK-init configuration
   by this doc's own definition: the node-wide resource ceiling, the
   mapping from budget tier to concrete numbers, and each domain's list of
   adopted node roles with their tier and priority. The structs these decode
   into are the source of truth, so code-defined configuration stays valid.
3. **A terminology warning.** The "roles" in the HCL section above are
   Gatehouse permission bundles. A *node role* in `19` is a different
   thing — a set of duties a node performs. Where both appear, say which.

## Where this sits relative to the rest of the design

None of this introduces a new base or a new dependency in
[`01-build-order.md`](01-build-order.md) — an HCL template consumer is
part of Gatehouse-core's facade layer (calling the same `Store`-backed
evaluator everything else calls), not a new package that other bases
need to know about. The only genuinely new piece of tooling implied here
is an HCL parser call and a small "diff current state against the
template, apply what's missing" reconciliation step inside that facade —
not a system of its own.
