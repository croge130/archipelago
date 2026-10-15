package noderolesauth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/noderoles"
	"github.com/croge130/archipelago/scheduler"
	"github.com/google/uuid"
)

// Authority source vocabulary used by node roles (09). The names are the
// ones the design doc fixes; there is no shared Go type for them yet.
const (
	SourceScheduledTask = "scheduled_task"
	SourceServiceAction = "service_action"
)

// Authority is who a run executed as and why: the three facts of 09 that must
// never be flattened into one actor field.
type Authority struct {
	// ActorPrincipalID performed the work; RequestedByPrincipalID's authority
	// requested it. For a node acting under its own standing authority they
	// are the same principal.
	ActorPrincipalID       uuid.UUID
	RequestedByPrincipalID uuid.UUID
	// Source is SourceScheduledTask or SourceServiceAction.
	Source string
	// Ref is the artifact that justifies it: "<node role>/<duty>".
	Ref string
}

type ctxKey struct{}

// AuthorityFrom is the authority of the run whose context this is.
func AuthorityFrom(ctx context.Context) (Authority, bool) {
	a, ok := ctx.Value(ctxKey{}).(Authority)
	return a, ok
}

// MissingPermissionsError says which of the permissions a node role needs the
// principal lacks. It matches evaluation.ErrDenied.
type MissingPermissionsError struct {
	PrincipalID uuid.UUID
	Missing     []string
}

func (e *MissingPermissionsError) Error() string {
	return fmt.Sprintf("principal %s lacks %s", e.PrincipalID, strings.Join(e.Missing, ", "))
}

func (e *MissingPermissionsError) Is(target error) bool { return target == evaluation.ErrDenied }

// Authorizer checks that principalID holds every permission, globally and at
// standard authority, and reports all that it lacks. A permission nobody ever
// registered counts as lacking (the evaluator denies an unknown key), so a
// node role cannot be authorized for something that does not exist. A store
// failure is returned as such, not as a denial, so the log can tell an outage
// from a refusal; the runtime fails closed on either.
func Authorizer(store evaluation.Store, principalID uuid.UUID) func(ctx context.Context, permissions []string) error {
	return func(ctx context.Context, permissions []string) error {
		var missing []string
		for _, key := range permissions {
			err := evaluation.RequirePermission(ctx, store, principalID, key)
			switch {
			case err == nil:
			case errors.Is(err, evaluation.ErrDenied):
				missing = append(missing, key)
			default:
				return fmt.Errorf("noderolesauth: checking %s for %s: %w", key, principalID, err)
			}
		}
		if len(missing) > 0 {
			return &MissingPermissionsError{PrincipalID: principalID, Missing: missing}
		}
		return nil
	}
}

// Annotator attaches the Authority to each run's context.
func Annotator(principalID uuid.UUID) func(ctx context.Context, role, duty string, f scheduler.Fire) context.Context {
	return func(ctx context.Context, role, duty string, f scheduler.Fire) context.Context {
		src := SourceServiceAction
		if !f.Slot.IsZero() {
			src = SourceScheduledTask
		}
		return context.WithValue(ctx, ctxKey{}, Authority{
			ActorPrincipalID: principalID, RequestedByPrincipalID: principalID, Source: src, Ref: role + "/" + duty,
		})
	}
}

// Bind returns the adoption with Authorize and Annotate set for the node's
// principal, leaving everything else (the budget, the gate, schedule
// overrides) as the caller set it.
func Bind(a noderoles.Adoption, store evaluation.Store, principalID uuid.UUID) noderoles.Adoption {
	a.Authorize = Authorizer(store, principalID)
	a.Annotate = Annotator(principalID)
	return a
}

// RegisterPermissions registers every permission the definitions name, as
// standard-authority keys, idempotently. A core node role under a reserved
// namespace sets opts.AllowReservedNamespace; an app-defined one does not,
// and the reserved-namespace guard then applies as it does to any key.
func RegisterPermissions(ctx context.Context, reader facade.Reader, writer facade.Writer, opts facade.RegisterPermissionOptions, defs ...noderoles.Definition) error {
	seen := map[string]bool{}
	for _, d := range defs {
		for _, key := range d.Permissions {
			if seen[key] {
				continue
			}
			seen[key] = true
			if err := facade.RegisterPermission(ctx, reader, writer, structure.PermissionDefinition{
				PermissionKey: key, RequiredAuthorityLevel: structure.AuthorityLevelStandard,
			}, opts); err != nil {
				return fmt.Errorf("noderolesauth: registering %s for node role %s: %w", key, d.Key, err)
			}
		}
	}
	return nil
}
