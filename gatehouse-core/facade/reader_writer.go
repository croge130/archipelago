package facade

import (
	"context"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
)

// Reader is everything the facade needs to read: evaluation.Store (so
// it can call Evaluate/Require itself) plus the two lookups Evaluate
// never needs but EnsurePrincipal and introspection do.
type Reader interface {
	evaluation.Store
	GetPrincipalByKey(ctx context.Context, key string) (structure.Principal, bool, error)
	GetGeneration(ctx context.Context) (structure.AuthorityGeneration, error)
	GetCredentialByMTLSFingerprint(ctx context.Context, fingerprint string) (structure.Credential, structure.MTLSCertCredDetail, bool, error)
	GetSession(ctx context.Context, id uuid.UUID) (structure.Session, bool, error)
}

// Writer is every mutation the facade drives. Moved here from
// storage/dbstore — see dbstore.PostgresWriter's doc comment for why
// the interface belongs with its consumer, not its implementation.
// Revoke/update/delete operations aren't included yet; this is enough
// to drive the facade's own ensure/register/grant operations, not the
// full CRUD surface.
type Writer interface {
	CreatePrincipal(ctx context.Context, p structure.Principal) error
	RegisterPermissionDefinition(ctx context.Context, def structure.PermissionDefinition) error
	CreateGrant(ctx context.Context, g structure.Grant) error
	CreateRole(ctx context.Context, r structure.Role) error
	AddRolePermission(ctx context.Context, p structure.RolePermission) error
	CreateGroup(ctx context.Context, g structure.Group) error
	AddGroupMember(ctx context.Context, m structure.GroupMembership) error
	CreateMTLSCredential(ctx context.Context, cred structure.Credential, detail structure.MTLSCertCredDetail) error
	CreateSession(ctx context.Context, s structure.Session) error
	RevokeSession(ctx context.Context, id uuid.UUID, revokedAt time.Time) error
}
