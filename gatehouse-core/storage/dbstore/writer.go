package dbstore

import (
	"context"
	"fmt"
	"time"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresWriter implements facade.Writer directly against Postgres.
// The Writer interface itself is deliberately NOT declared here —
// it's declared in facade, the package that actually consumes it,
// mirroring exactly how evaluation declares Store rather than dbstore
// declaring it. Declaring Writer in this package would mean any future
// alternative implementation (one that routes through a designated
// writer node for an asymmetric topology) has to import dbstore just
// to reference the interface type, pulling in pgx and everything else
// Postgres-specific along with it — precisely the coupling the
// structure/evaluation/storage split exists to avoid.
type PostgresWriter struct {
	pool *pgxpool.Pool
}

func NewPostgresWriter(pool *pgxpool.Pool) *PostgresWriter {
	return &PostgresWriter{pool: pool}
}

func (w *PostgresWriter) CreatePrincipal(ctx context.Context, p structure.Principal) error {
	if err := p.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO gatehouse_principals (principal_id, key, display_name, type, owner_principal_id, metadata, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuidToText(p.PrincipalID), p.Key, p.DisplayName, string(p.Type),
		nullableUUIDToText(p.OwnerPrincipalID), nullableJSON(p.Metadata), p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create principal: %w", err)
	}
	return nil
}

// CreateMTLSCredential inserts both the common Credential row and its
// mtls_certificate detail row atomically — no secret storage at all,
// per the model doc: cred_fingerprint is a reference into certstore's
// own records, never a copy of the certificate or key. Does not bump
// principal_grant_generation: a credential is authentication
// material, orthogonal to what a principal is authorized for, which
// only grants/roles/groups change.
func (w *PostgresWriter) CreateMTLSCredential(ctx context.Context, cred structure.Credential, detail structure.MTLSCertCredDetail) error {
	if err := cred.Validate(); err != nil {
		return err
	}
	if err := detail.Validate(); err != nil {
		return err
	}
	if cred.CredentialID != detail.CredentialID {
		return fmt.Errorf("dbstore: create mtls credential: Credential.CredentialID and MTLSCertCredDetail.CredentialID must match")
	}
	if cred.Kind != structure.CredentialKindMTLSCertificate {
		return fmt.Errorf("dbstore: create mtls credential: Credential.Kind must be %q, got %q", structure.CredentialKindMTLSCertificate, cred.Kind)
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbstore: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`INSERT INTO gatehouse_credentials (credential_id, principal_id, kind, status, created_at, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		uuidToText(cred.CredentialID), uuidToText(cred.PrincipalID), string(cred.Kind), string(cred.Status), cred.CreatedAt, cred.RevokedAt,
	); err != nil {
		return fmt.Errorf("dbstore: create mtls credential: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO gatehouse_mtls_certificate_credentials (credential_id, cert_fingerprint)
		 VALUES ($1, $2)`,
		uuidToText(detail.CredentialID), detail.CertFingerprint,
	); err != nil {
		return fmt.Errorf("dbstore: create mtls credential: %w", err)
	}
	return tx.Commit(ctx)
}

// CreateSession inserts a new session. Never bumps
// principal_grant_generation — authenticating is orthogonal to what a
// principal is authorized for, which only grants/roles/groups change.
func (w *PostgresWriter) CreateSession(ctx context.Context, s structure.Session) error {
	if err := s.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO gatehouse_sessions (
			session_id, principal_id, credential_id, kind, authority_level, authentication_method,
			asserted_by_principal_id, metadata, created_at, expires_at, last_seen, revoked_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		uuidToText(s.SessionID), uuidToText(s.PrincipalID), nullableUUIDToText(s.CredentialID),
		string(s.Kind), string(s.AuthorityLevel), string(s.AuthenticationMethod),
		nullableUUIDToText(s.AssertedByPrincipalID), nullableJSON(s.Metadata),
		s.CreatedAt, s.ExpiresAt, s.LastSeen, s.RevokedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create session: %w", err)
	}
	return nil
}

// RevokeSession marks a session revoked. Per the model doc's standing
// invariant, a revoked session is never honored by anything cached
// regardless of what any snapshot still claims — this is the write
// that makes that true going forward from the moment it commits.
func (w *PostgresWriter) RevokeSession(ctx context.Context, id uuid.UUID, revokedAt time.Time) error {
	_, err := w.pool.Exec(ctx,
		`UPDATE gatehouse_sessions SET revoked_at = $2 WHERE session_id = $1 AND revoked_at IS NULL`,
		uuidToText(id), revokedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: revoke session: %w", err)
	}
	return nil
}

func (w *PostgresWriter) RegisterPermissionDefinition(ctx context.Context, def structure.PermissionDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbstore: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`INSERT INTO gatehouse_permission_definitions (permission_key, required_authority_level, wildcard_includable)
		 VALUES ($1, $2, $3)`,
		def.PermissionKey, string(def.RequiredAuthorityLevel), def.WildcardIncludable,
	); err != nil {
		return fmt.Errorf("dbstore: register permission definition: %w", err)
	}
	if err := bumpPermissionSchemaGeneration(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *PostgresWriter) CreateGrant(ctx context.Context, g structure.Grant) error {
	if err := g.Validate(); err != nil {
		return err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbstore: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`INSERT INTO gatehouse_grants (
			grant_id, subject_type, subject_id, target_type, permission_key, role_id,
			scope, context_type, context_id, effect, status, origin, metadata,
			created_by, created_at, updated_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		uuidToText(g.GrantID), string(g.SubjectType), uuidToText(g.SubjectID), string(g.TargetType),
		g.PermissionKey, nullableUUIDToText(g.RoleID),
		string(g.Scope), g.ContextType, g.ContextID,
		string(g.Effect), string(g.Status), string(g.Origin), nullableJSON(g.Metadata),
		nullableUUIDToText(g.CreatedBy), g.CreatedAt, g.UpdatedAt,
	); err != nil {
		return fmt.Errorf("dbstore: create grant: %w", err)
	}
	// Direct grant change — see the model doc's one-invariant
	// principal_grant_generation rule.
	if err := bumpPrincipalGrantGeneration(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *PostgresWriter) CreateRole(ctx context.Context, r structure.Role) error {
	if err := r.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO gatehouse_roles (role_id, key, description, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		uuidToText(r.RoleID), r.Key, r.Description, r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create role: %w", err)
	}
	return nil
}

func (w *PostgresWriter) AddRolePermission(ctx context.Context, p structure.RolePermission) error {
	if err := p.Validate(); err != nil {
		return err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbstore: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// id is a storage-only surrogate key; structure.RolePermission has
	// no ID field of its own (see migrations/0012's comment).
	if _, err := tx.Exec(ctx,
		`INSERT INTO gatehouse_role_permissions (id, role_id, permission_key, child_role_id, effect)
		 VALUES ($1, $2, $3, $4, $5)`,
		uuidToText(uuid.New()), uuidToText(p.RoleID), p.PermissionKey, nullableUUIDToText(p.ChildRoleID), string(p.Effect),
	); err != nil {
		return fmt.Errorf("dbstore: add role permission: %w", err)
	}
	// A role change (permission or inheritance edge) — covered by the
	// same principal_grant_generation invariant as a direct grant.
	if err := bumpPrincipalGrantGeneration(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *PostgresWriter) CreateGroup(ctx context.Context, g structure.Group) error {
	if err := g.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO gatehouse_groups (group_id, key, description, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		uuidToText(g.GroupID), g.Key, g.Description, g.CreatedAt, g.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create group: %w", err)
	}
	return nil
}

func (w *PostgresWriter) AddGroupMember(ctx context.Context, m structure.GroupMembership) error {
	if err := m.Validate(); err != nil {
		return err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbstore: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`INSERT INTO gatehouse_group_memberships (group_id, principal_id, created_at)
		 VALUES ($1, $2, $3)`,
		uuidToText(m.GroupID), uuidToText(m.PrincipalID), m.CreatedAt,
	); err != nil {
		return fmt.Errorf("dbstore: add group member: %w", err)
	}
	// A group membership change — covered by the same
	// principal_grant_generation invariant as a direct grant.
	if err := bumpPrincipalGrantGeneration(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func bumpPrincipalGrantGeneration(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO gatehouse_authority_generation (id, principal_grant_generation, updated_at)
		VALUES (true, 1, now())
		ON CONFLICT (id) DO UPDATE SET
			principal_grant_generation = gatehouse_authority_generation.principal_grant_generation + 1,
			updated_at = now()`)
	if err != nil {
		return fmt.Errorf("dbstore: bump principal grant generation: %w", err)
	}
	return nil
}

func bumpPermissionSchemaGeneration(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO gatehouse_authority_generation (id, permission_schema_generation, updated_at)
		VALUES (true, 1, now())
		ON CONFLICT (id) DO UPDATE SET
			permission_schema_generation = gatehouse_authority_generation.permission_schema_generation + 1,
			updated_at = now()`)
	if err != nil {
		return fmt.Errorf("dbstore: bump permission schema generation: %w", err)
	}
	return nil
}
