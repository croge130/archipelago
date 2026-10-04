package dbstore

import (
	"context"
	"errors"
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

// UpsertInstance sets the full row for InstanceID — a raw set, like
// alias's own UpsertAlias; the registry integration's RegisterFromSession
// and Heartbeat decide the desired row state before calling this.
func (w *PostgresWriter) UpsertInstance(ctx context.Context, i structure.Instance) error {
	if err := i.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO gatehouse_instances (instance_id, principal_id, instance_group, metadata, registered_at, last_heartbeat_at)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (instance_id) DO UPDATE SET
			last_heartbeat_at = EXCLUDED.last_heartbeat_at,
			metadata = EXCLUDED.metadata`,
		uuidToText(i.InstanceID), uuidToText(i.PrincipalID), i.Group, nullableJSON(i.Metadata),
		i.RegisteredAt, i.LastHeartbeatAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: upsert instance: %w", err)
	}
	return nil
}

// DeleteInstance removes an instance's row outright — the graceful-
// shutdown deregistration path; a crashed instance simply ages out of
// ListInstancesByGroup's own staleness cutoff instead of needing this.
func (w *PostgresWriter) DeleteInstance(ctx context.Context, instanceID uuid.UUID) error {
	_, err := w.pool.Exec(ctx, `DELETE FROM gatehouse_instances WHERE instance_id = $1`, uuidToText(instanceID))
	if err != nil {
		return fmt.Errorf("dbstore: delete instance: %w", err)
	}
	return nil
}

// AcquireOrRenewLease is the one atomic CAS this package relies on for
// correctness, per 13-registry-and-leases-model.md: the WHERE clause on
// the DO UPDATE only lets the conflicting row change if it's expired or
// already held by holderInstanceID, so Postgres itself — not a
// read-then-write race in Go — decides whether the lease was actually
// claimed. ok is false, with no error, when the lease is held by
// someone else and isn't expired yet.
func (w *PostgresWriter) AcquireOrRenewLease(ctx context.Context, group, name string, holderInstanceID uuid.UUID, acquiredAt, expiresAt time.Time) (structure.Lease, bool, error) {
	var l structure.Lease
	var holderText string
	err := w.pool.QueryRow(ctx,
		`INSERT INTO gatehouse_leases (lease_group, lease_name, holder_instance_id, acquired_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (lease_group, lease_name) DO UPDATE SET
			holder_instance_id = EXCLUDED.holder_instance_id,
			acquired_at = EXCLUDED.acquired_at,
			expires_at = EXCLUDED.expires_at
		 WHERE gatehouse_leases.expires_at < EXCLUDED.acquired_at
		    OR gatehouse_leases.holder_instance_id = EXCLUDED.holder_instance_id
		 RETURNING lease_group, lease_name, holder_instance_id, acquired_at, expires_at`,
		group, name, uuidToText(holderInstanceID), acquiredAt, expiresAt,
	).Scan(&l.Group, &l.Name, &holderText, &l.AcquiredAt, &l.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Lease{}, false, nil
		}
		return structure.Lease{}, false, fmt.Errorf("dbstore: acquire or renew lease: %w", err)
	}
	if l.HolderInstanceID, err = parseUUID(holderText); err != nil {
		return structure.Lease{}, false, fmt.Errorf("dbstore: parse holder_instance_id: %w", err)
	}
	return l, true, nil
}

// ReleaseLease deletes the lease on (group, name) only if
// holderInstanceID is the one currently holding it — unconditionally
// successful either way, since releasing something you don't hold
// changes nothing, per 13-registry-and-leases-model.md.
func (w *PostgresWriter) ReleaseLease(ctx context.Context, group, name string, holderInstanceID uuid.UUID) error {
	_, err := w.pool.Exec(ctx,
		`DELETE FROM gatehouse_leases WHERE lease_group = $1 AND lease_name = $2 AND holder_instance_id = $3`,
		group, name, uuidToText(holderInstanceID),
	)
	if err != nil {
		return fmt.Errorf("dbstore: release lease: %w", err)
	}
	return nil
}

// RegisterEndpointDefinition inserts a new endpoint definition — a raw
// insert; facade.RegisterEndpoint decides key idempotency and the
// RequiredPermissionKey cross-check before calling this, the same
// split RegisterPermissionDefinition's own caller uses. No generation
// bump: unlike a permission or grant, registering an endpoint doesn't
// change any evaluation outcome, only what gets listed.
func (w *PostgresWriter) RegisterEndpointDefinition(ctx context.Context, def structure.EndpointDefinition) error {
	if err := def.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO gatehouse_endpoint_definitions (endpoint_key, description, required_permission_key, metadata)
		 VALUES ($1, $2, $3, $4)`,
		def.EndpointKey, def.Description, def.RequiredPermissionKey, nullableJSON(def.Metadata),
	)
	if err != nil {
		return fmt.Errorf("dbstore: register endpoint definition: %w", err)
	}
	return nil
}
