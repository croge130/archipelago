package facade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/alias/structure"
)

// ErrConflict is returned by EnsureAlias when table+name already
// resolves to a different, active target. "Ensure" means idempotently
// create-if-absent, never silently repoint an existing alias — a
// caller that genuinely wants to repoint one does so by releasing it
// first, an explicit, visible two-step operation rather than a quiet
// side effect of calling Ensure again.
var ErrConflict = errors.New("facade: alias exists with a different active target")

// ErrNotFound is returned by ReleaseAlias when table+name has never
// existed at all.
var ErrNotFound = errors.New("facade: alias not found")

// EnsureAlias is idempotent by (table, name): calling it again with
// the same target is a no-op; calling it with a different target while
// an active alias already exists is ErrConflict, not a silent
// overwrite. A released alias's slot can be reused — Ensure reactivates
// it with the given target.
//
// Known limitation, same as Gatehouse-core's EnsurePrincipal: this is
// a read then a write, not an atomic upsert, so two concurrent
// first-time Ensure calls for the same (table, name) can race.
func EnsureAlias(ctx context.Context, reader Reader, writer Writer, table, name, target string) (structure.Alias, error) {
	existing, found, err := reader.GetAlias(ctx, table, name)
	if err != nil {
		return structure.Alias{}, fmt.Errorf("facade: ensure alias: %w", err)
	}

	// Truncated to microsecond precision to match what Postgres's
	// timestamptz actually stores (and strip the monotonic reading
	// time.Now() carries) — otherwise a value returned fresh on create
	// has more precision than the same value read back later ever
	// will, and the two stop comparing equal despite representing the
	// same persisted instant.
	now := time.Now().Truncate(time.Microsecond)
	if found && existing.Lifecycle == structure.LifecycleActive {
		if existing.Target == target {
			return existing, nil
		}
		return structure.Alias{}, ErrConflict
	}

	a := structure.Alias{
		Table:     table,
		Name:      name,
		Target:    target,
		Lifecycle: structure.LifecycleActive,
		UpdatedAt: now,
	}
	if found {
		a.CreatedAt = existing.CreatedAt // reactivating a released slot keeps its original creation time
	} else {
		a.CreatedAt = now
	}
	if err := writer.UpsertAlias(ctx, a); err != nil {
		return structure.Alias{}, fmt.Errorf("facade: ensure alias: %w", err)
	}
	return a, nil
}

// ReleaseAlias marks table+name as released. Idempotent: releasing an
// already-released alias is a no-op success, not an error — matching
// EnsureAlias's own idempotent shape. Releasing something that was
// never created at all is ErrNotFound, since there's nothing to
// release.
func ReleaseAlias(ctx context.Context, reader Reader, writer Writer, table, name string) error {
	existing, found, err := reader.GetAlias(ctx, table, name)
	if err != nil {
		return fmt.Errorf("facade: release alias: %w", err)
	}
	if !found {
		return ErrNotFound
	}
	if existing.Lifecycle == structure.LifecycleReleased {
		return nil
	}

	now := time.Now().Truncate(time.Microsecond) // see EnsureAlias's comment on why
	existing.Lifecycle = structure.LifecycleReleased
	existing.UpdatedAt = now
	existing.ReleasedAt = &now
	if err := writer.UpsertAlias(ctx, existing); err != nil {
		return fmt.Errorf("facade: release alias: %w", err)
	}
	return nil
}
