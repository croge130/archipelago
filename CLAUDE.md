# Working in this repo

## Style of replies

1. In chat replies, use numbered points. Do not use bulleted lists.
2. Prose paragraphs and tables are fine where they fit better than a list.
3. This is about how replies are written. It does not restrict the format of
   the architecture docs under `docs/architecture/`, which have their own
   conventions.

## Terminology

1. "Role" on its own means Gatehouse's permission bundle (`gatehouse_roles`).
2. The concept of a set of duties a node adopts is always a "node role",
   in docs, type names, module names (`noderoles`) and config keys. Never
   write bare "role" for it.
3. Within a node role, a "duty" is one unit of automatic work; a "task" is
   one execution of it, and a "job" is a task made durable by the job system.

## Design rules

1. Depending on `typedvalue` or `typeconstraints` is fine and is never a
   reason to hand-roll something. They are the intended type system wherever
   values are complex or dynamic (Policy-held settings, definitions that are
   data). For a permanently fixed type, plain Go types are fine too.
2. Keeping a dependency out is a legitimate goal for systems that should stay
   optional or usable on their own: a node with no database access should not
   have to carry Gatehouse-core to run jobs, and discovery needs nothing of
   ours. Weigh a dependency by whether the system should be usable without it,
   not by a blanket preference either way.
