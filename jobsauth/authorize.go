package jobsauth

import (
	"context"
	"errors"
	"fmt"

	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	gatehouseStructure "github.com/croge130/archipelago/gatehouse-core/structure"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
)

// Permission keys this package owns, and the context type a queue is.
const (
	PermissionSubmit = "jobs.submit"
	PermissionRead   = "jobs.read"
	PermissionClaim  = "jobs.claim"
	PermissionCancel = "jobs.cancel"

	// PermissionExecute is a global permission, not a queue-scoped one: it
	// gates the remote-executor protocol (20, "Remote executors") so that
	// only peers meant to be executors are offered or served its routes. It
	// does not replace PermissionClaim, which still decides which queues an
	// executor may pull from.
	PermissionExecute = "jobs.execute"

	// ContextTypeQueue is the Gatehouse-core context type this package
	// scopes permissions to; a context's ID under it is a queue key.
	ContextTypeQueue = "jobs.queue"
)

var (
	// ErrAuthorityDenied marks a failure that retrying cannot fix: the
	// operation is outside the task kind's declared scope, or the
	// effective principal does not (or no longer) hold the permission.
	// Fail treats a cause that wraps it as terminal.
	ErrAuthorityDenied = errors.New("jobsauth: authority denied")

	// ErrOutOfScope is the part of ErrAuthorityDenied where the
	// operation falls outside what the task kind declared it may do.
	ErrOutOfScope = fmt.Errorf("%w: outside the task kind's declared scope", ErrAuthorityDenied)

	ErrPrincipalNotFound = errors.New("jobsauth: principal not found")
)

// Deps are the stores this package works over. GatehouseWriter and
// JobsWriter may be nil on a node that only reads; calling an operation
// that writes then fails clearly rather than panicking.
type Deps struct {
	GatehouseReader gatehouseFacade.Reader
	GatehouseWriter gatehouseFacade.Writer
	JobsReader      jobsFacade.Reader
	JobsWriter      jobsFacade.Writer
}

func (d Deps) needWriters(op string) error {
	if d.GatehouseWriter == nil || d.JobsWriter == nil {
		return fmt.Errorf("jobsauth: %s needs both a Gatehouse writer and a jobs writer", op)
	}
	return nil
}

// RegisterPermissions registers this package's five permission keys as
// standard-authority, wildcard-includable definitions — wildcard-
// includable so a single "jobs.*" grant on a queue covers all of them,
// the same default every other key in this design uses.
// gatehouseFacade.RegisterPermission is idempotent by key, so this is
// safe on every startup.
func RegisterPermissions(ctx context.Context, reader gatehouseFacade.Reader, writer gatehouseFacade.Writer) error {
	opts := gatehouseFacade.RegisterPermissionOptions{AllowReservedNamespace: true}
	for _, key := range []string{PermissionSubmit, PermissionRead, PermissionClaim, PermissionCancel, PermissionExecute} {
		def := gatehouseStructure.PermissionDefinition{
			PermissionKey:          key,
			RequiredAuthorityLevel: gatehouseStructure.AuthorityLevelStandard,
			WildcardIncludable:     true,
		}
		if err := gatehouseFacade.RegisterPermission(ctx, reader, writer, def, opts); err != nil {
			return fmt.Errorf("jobsauth: register permissions: %w", err)
		}
	}
	return nil
}
