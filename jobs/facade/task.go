package facade

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/croge130/archipelago/jobs/evaluation"
	"github.com/croge130/archipelago/jobs/structure"
)

// ErrConflict means something already exists under the same key with a
// different shape. "Register" and "submit with an idempotency key" are
// both create-if-absent, never a silent redefinition.
var ErrConflict = errors.New("facade: jobs: already exists with a different shape")

// DefaultReservedNamespaces are task-key prefixes an application may
// not register without saying so deliberately. This is hygiene against
// accidental collision with built-in task kinds, not a security
// boundary — the same stance 09 takes for permission keys. An
// integration that knows a wider list (Gatehouse-core's, for instance)
// passes it in RegisterTaskOptions.
var DefaultReservedNamespaces = []string{"jobs", "archipelago"}

type RegisterTaskOptions struct {
	// ReservedNamespaces replaces DefaultReservedNamespaces when non-nil.
	ReservedNamespaces []string
	// AllowReservedNamespace opts out of the reserved-namespace check.
	AllowReservedNamespace bool
}

// RegisterTaskDefinition records a task kind. It is idempotent by
// TaskKey: an identical re-registration is a no-op, and a different
// definition under the same key is ErrConflict. The comparison is by
// canonical form (evaluation.SameTaskDefinition), so a freshly built
// definition with nil lists matches the same definition read back from
// storage with empty ones.
//
// Like every Register*/Ensure* in this codebase it is a read then a
// write, so two concurrent first-time registrations of one brand-new key
// can race; the loser's insert fails on the primary key rather than
// corrupting anything.
func RegisterTaskDefinition(ctx context.Context, reader Reader, writer Writer, def structure.TaskDefinition, opts RegisterTaskOptions) error {
	if err := def.Validate(); err != nil {
		return fmt.Errorf("facade: register task definition: %w", err)
	}
	if !opts.AllowReservedNamespace {
		reserved := opts.ReservedNamespaces
		if reserved == nil {
			reserved = DefaultReservedNamespaces
		}
		first, _, _ := strings.Cut(def.TaskKey, ".")
		for _, r := range reserved {
			if first == r {
				return fmt.Errorf("facade: register task definition: %q is in the reserved namespace %q", def.TaskKey, r)
			}
		}
	}
	existing, found, err := reader.GetTaskDefinition(ctx, def.TaskKey)
	if err != nil {
		return fmt.Errorf("facade: register task definition: %w", err)
	}
	if found {
		if evaluation.SameTaskDefinition(existing, def) {
			return nil
		}
		return ErrConflict
	}
	if err := writer.RegisterTaskDefinition(ctx, def); err != nil {
		return fmt.Errorf("facade: register task definition: %w", err)
	}
	return nil
}
