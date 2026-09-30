# Archipelago

Archipelago is a federated identity, authority, and coordination toolkit.
It grew out of lessons learned building Lighthouse: a centralized Gatehouse
that served many apps from one shared instance worked, but cross-boundary
identity got expensive fast (see the cross-realm containment policy that
Lighthouse had to bolt on once it actually happened), provisioning access to
a separate always-running service was a constant source of friction, and a
"coordinator" that owns everything doesn't compose well once you want more
than one topology.

Archipelago's answer: there is no distinguished coordinator binary. Every
capability — identity, authority, SSO, registry, discovery — is a mode of
the same SDK. An app that wants to be fully self-sufficient just uses the
SDK against its own database. An app that wants to act as a coordinator, an
SSO provider, or a registry for others is running the exact same SDK with a
few more modes turned on. Nothing is special-cased into a separate codebase.

## Start here

- [`docs/architecture/00-overview.md`](docs/architecture/00-overview.md) —
  the core thesis and the decisions that follow from it.
- [`docs/architecture/01-build-order.md`](docs/architecture/01-build-order.md) —
  what can be built and tested independently, and what genuinely requires
  something else first.
- [`docs/architecture/02-package-boundaries.md`](docs/architecture/02-package-boundaries.md) —
  how the above maps onto actual Go package structure.

Diagrams are Mermaid, embedded directly in the docs — GitHub renders them
natively, and they diff like text because they *are* text. See
`docs/architecture/` for where new ones should live as the design grows.
