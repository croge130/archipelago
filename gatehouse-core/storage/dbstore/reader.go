package dbstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresReader implements evaluation.Store directly against
// Postgres. Kept as its own type, separate from PostgresWriter below,
// specifically so a future asymmetric-DB-access topology (per
// docs/architecture/03-multi-instance-and-suites.md) can keep this
// reader doing direct reads while swapping in a different Writer that
// routes writes through a designated writer node instead — reads and
// writes were never going to need to change together, so they were
// never given one type to change together in.
type PostgresReader struct {
	pool *pgxpool.Pool
}

func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{pool: pool}
}

// GetPrincipalByKey looks up a principal by its stable, human-oriented
// key. Not part of evaluation.Store — Evaluate works from a
// PrincipalID, never a key — but needed by the facade's EnsurePrincipal
// to make "ensure" genuinely idempotent rather than erroring on a
// second call for the same key.
func (r *PostgresReader) GetPrincipalByKey(ctx context.Context, key string) (structure.Principal, bool, error) {
	var p structure.Principal
	var principalIDText string
	var typ string
	var ownerText *string
	var metadata []byte
	err := r.pool.QueryRow(ctx,
		`SELECT principal_id, key, display_name, type, owner_principal_id, metadata, created_at, updated_at
		 FROM gatehouse_principals WHERE key = $1`,
		key,
	).Scan(&principalIDText, &p.Key, &p.DisplayName, &typ, &ownerText, &metadata, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Principal{}, false, nil
		}
		return structure.Principal{}, false, fmt.Errorf("dbstore: get principal by key: %w", err)
	}
	if p.PrincipalID, err = parseUUID(principalIDText); err != nil {
		return structure.Principal{}, false, fmt.Errorf("dbstore: parse principal_id: %w", err)
	}
	if p.OwnerPrincipalID, err = parseNullableUUID(ownerText); err != nil {
		return structure.Principal{}, false, fmt.Errorf("dbstore: parse owner_principal_id: %w", err)
	}
	p.Type = structure.PrincipalType(typ)
	p.Metadata = metadata
	return p, true, nil
}

// GetPrincipal looks up a principal by its PrincipalID — the lookup
// Evaluate itself never needs (it always already has a PrincipalID in
// hand) but a caller vouching for one, like SSO ticket issuance, does:
// confirming the subject it's about to sign for actually exists before
// signing anything.
func (r *PostgresReader) GetPrincipal(ctx context.Context, principalID uuid.UUID) (structure.Principal, bool, error) {
	var p structure.Principal
	var principalIDText string
	var typ string
	var ownerText *string
	var metadata []byte
	err := r.pool.QueryRow(ctx,
		`SELECT principal_id, key, display_name, type, owner_principal_id, metadata, created_at, updated_at
		 FROM gatehouse_principals WHERE principal_id = $1`,
		principalID.String(),
	).Scan(&principalIDText, &p.Key, &p.DisplayName, &typ, &ownerText, &metadata, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Principal{}, false, nil
		}
		return structure.Principal{}, false, fmt.Errorf("dbstore: get principal: %w", err)
	}
	if p.PrincipalID, err = parseUUID(principalIDText); err != nil {
		return structure.Principal{}, false, fmt.Errorf("dbstore: parse principal_id: %w", err)
	}
	if p.OwnerPrincipalID, err = parseNullableUUID(ownerText); err != nil {
		return structure.Principal{}, false, fmt.Errorf("dbstore: parse owner_principal_id: %w", err)
	}
	p.Type = structure.PrincipalType(typ)
	p.Metadata = metadata
	return p, true, nil
}

// GetCredentialByMTLSFingerprint resolves a verified mTLS peer's
// certificate fingerprint straight to the Credential and its detail
// row — peer authorization's primary lookup, per
// 01-build-order.md's Layer 2 table. Not part of evaluation.Store:
// resolving a fingerprint to a principal is the peerauth integration's
// job, Evaluate itself works from a PrincipalID.
func (r *PostgresReader) GetCredentialByMTLSFingerprint(ctx context.Context, fingerprint string) (structure.Credential, structure.MTLSCertCredDetail, bool, error) {
	var cred structure.Credential
	var detail structure.MTLSCertCredDetail
	var credentialIDText, principalIDText, kind, status string
	err := r.pool.QueryRow(ctx,
		`SELECT c.credential_id, c.principal_id, c.kind, c.status, c.created_at, c.revoked_at, m.cert_fingerprint
		 FROM gatehouse_mtls_certificate_credentials m
		 JOIN gatehouse_credentials c ON c.credential_id = m.credential_id
		 WHERE m.cert_fingerprint = $1`,
		fingerprint,
	).Scan(&credentialIDText, &principalIDText, &kind, &status, &cred.CreatedAt, &cred.RevokedAt, &detail.CertFingerprint)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Credential{}, structure.MTLSCertCredDetail{}, false, nil
		}
		return structure.Credential{}, structure.MTLSCertCredDetail{}, false, fmt.Errorf("dbstore: get credential by mtls fingerprint: %w", err)
	}
	if cred.CredentialID, err = parseUUID(credentialIDText); err != nil {
		return structure.Credential{}, structure.MTLSCertCredDetail{}, false, fmt.Errorf("dbstore: parse credential_id: %w", err)
	}
	if cred.PrincipalID, err = parseUUID(principalIDText); err != nil {
		return structure.Credential{}, structure.MTLSCertCredDetail{}, false, fmt.Errorf("dbstore: parse principal_id: %w", err)
	}
	cred.Kind = structure.CredentialKind(kind)
	cred.Status = structure.CredentialStatus(status)
	detail.CredentialID = cred.CredentialID
	return cred, detail, true, nil
}

// GetSession looks up a session by ID. Not part of evaluation.Store —
// Evaluate works from a PrincipalID and an AuthorityLevel the caller
// already has in hand, never from a session lookup of its own.
func (r *PostgresReader) GetSession(ctx context.Context, id uuid.UUID) (structure.Session, bool, error) {
	var s structure.Session
	var sessionIDText, principalIDText, kind, authorityLevel, authMethod string
	var credentialIDText, assertedByText *string
	var metadata []byte
	err := r.pool.QueryRow(ctx,
		`SELECT session_id, principal_id, credential_id, kind, authority_level, authentication_method,
		        asserted_by_principal_id, metadata, created_at, expires_at, last_seen, revoked_at
		 FROM gatehouse_sessions WHERE session_id = $1`,
		uuidToText(id),
	).Scan(&sessionIDText, &principalIDText, &credentialIDText, &kind, &authorityLevel, &authMethod,
		&assertedByText, &metadata, &s.CreatedAt, &s.ExpiresAt, &s.LastSeen, &s.RevokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Session{}, false, nil
		}
		return structure.Session{}, false, fmt.Errorf("dbstore: get session: %w", err)
	}
	if s.SessionID, err = parseUUID(sessionIDText); err != nil {
		return structure.Session{}, false, fmt.Errorf("dbstore: parse session_id: %w", err)
	}
	if s.PrincipalID, err = parseUUID(principalIDText); err != nil {
		return structure.Session{}, false, fmt.Errorf("dbstore: parse principal_id: %w", err)
	}
	if s.CredentialID, err = parseNullableUUID(credentialIDText); err != nil {
		return structure.Session{}, false, fmt.Errorf("dbstore: parse credential_id: %w", err)
	}
	if s.AssertedByPrincipalID, err = parseNullableUUID(assertedByText); err != nil {
		return structure.Session{}, false, fmt.Errorf("dbstore: parse asserted_by_principal_id: %w", err)
	}
	s.Kind = structure.SessionKind(kind)
	s.AuthorityLevel = structure.AuthorityLevel(authorityLevel)
	s.AuthenticationMethod = structure.AuthenticationMethod(authMethod)
	s.Metadata = metadata
	return s, true, nil
}

func (r *PostgresReader) GetPermissionDefinition(ctx context.Context, key string) (structure.PermissionDefinition, bool, error) {
	var def structure.PermissionDefinition
	err := r.pool.QueryRow(ctx,
		`SELECT permission_key, required_authority_level, wildcard_includable
		 FROM gatehouse_permission_definitions WHERE permission_key = $1`,
		key,
	).Scan(&def.PermissionKey, &def.RequiredAuthorityLevel, &def.WildcardIncludable)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.PermissionDefinition{}, false, nil
		}
		return structure.PermissionDefinition{}, false, fmt.Errorf("dbstore: get permission definition: %w", err)
	}
	return def, true, nil
}

func (r *PostgresReader) ActiveGrantsForSubject(ctx context.Context, subjectType structure.GrantSubjectType, subjectID uuid.UUID) ([]structure.Grant, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT grant_id, subject_type, subject_id, target_type, permission_key, role_id,
		       scope, context_type, context_id, effect, status, origin, metadata,
		       created_by, created_at, updated_at
		FROM gatehouse_grants
		WHERE subject_type = $1 AND subject_id = $2 AND status = $3`,
		string(subjectType), uuidToText(subjectID), string(structure.GrantStatusActive),
	)
	if err != nil {
		return nil, fmt.Errorf("dbstore: active grants for subject: %w", err)
	}
	defer rows.Close()

	var grants []structure.Grant
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, rows.Err()
}

func (r *PostgresReader) GroupIDsForPrincipal(ctx context.Context, principalID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT group_id FROM gatehouse_group_memberships WHERE principal_id = $1`,
		uuidToText(principalID),
	)
	if err != nil {
		return nil, fmt.Errorf("dbstore: group ids for principal: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("dbstore: scan group id: %w", err)
		}
		id, err := parseUUID(s)
		if err != nil {
			return nil, fmt.Errorf("dbstore: parse group id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *PostgresReader) RolePermissions(ctx context.Context, roleID uuid.UUID) ([]structure.RolePermission, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT role_id, permission_key, child_role_id, effect
		 FROM gatehouse_role_permissions WHERE role_id = $1`,
		uuidToText(roleID),
	)
	if err != nil {
		return nil, fmt.Errorf("dbstore: role permissions: %w", err)
	}
	defer rows.Close()

	var perms []structure.RolePermission
	for rows.Next() {
		var roleIDText string
		var permissionKey, childRoleIDText *string
		var effect string
		if err := rows.Scan(&roleIDText, &permissionKey, &childRoleIDText, &effect); err != nil {
			return nil, fmt.Errorf("dbstore: scan role permission: %w", err)
		}
		rid, err := parseUUID(roleIDText)
		if err != nil {
			return nil, fmt.Errorf("dbstore: parse role_id: %w", err)
		}
		childRoleID, err := parseNullableUUID(childRoleIDText)
		if err != nil {
			return nil, fmt.Errorf("dbstore: parse child_role_id: %w", err)
		}
		perms = append(perms, structure.RolePermission{
			RoleID:        rid,
			PermissionKey: permissionKey,
			ChildRoleID:   childRoleID,
			Effect:        structure.GrantEffect(effect),
		})
	}
	return perms, rows.Err()
}

// GetGeneration reads the current generation counters. Not part of
// evaluation.Store — Evaluate doesn't need it today — but a real read,
// kept on the Reader rather than the Writer, for tests and future
// freshness/snapshot work to build on. A missing row (nothing has ever
// bumped either counter) reads as generation zero rather than an error.
func (r *PostgresReader) GetGeneration(ctx context.Context) (structure.AuthorityGeneration, error) {
	var gen structure.AuthorityGeneration
	err := r.pool.QueryRow(ctx,
		`SELECT permission_schema_generation, principal_grant_generation, updated_at
		 FROM gatehouse_authority_generation WHERE id = true`,
	).Scan(&gen.PermissionSchemaGeneration, &gen.PrincipalGrantGeneration, &gen.GeneratedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.AuthorityGeneration{}, nil
		}
		return structure.AuthorityGeneration{}, fmt.Errorf("dbstore: get generation: %w", err)
	}
	return gen, nil
}

func scanGrant(rows pgx.Rows) (structure.Grant, error) {
	var g structure.Grant
	var grantIDText, subjectIDText string
	var subjectType, targetType, scope, effect, status, origin string
	var permissionKey, roleIDText, contextType, contextID, createdByText *string
	var metadata []byte

	if err := rows.Scan(
		&grantIDText, &subjectType, &subjectIDText, &targetType, &permissionKey, &roleIDText,
		&scope, &contextType, &contextID, &effect, &status, &origin, &metadata,
		&createdByText, &g.CreatedAt, &g.UpdatedAt,
	); err != nil {
		return g, fmt.Errorf("dbstore: scan grant: %w", err)
	}

	var err error
	if g.GrantID, err = parseUUID(grantIDText); err != nil {
		return g, fmt.Errorf("dbstore: parse grant_id: %w", err)
	}
	if g.SubjectID, err = parseUUID(subjectIDText); err != nil {
		return g, fmt.Errorf("dbstore: parse subject_id: %w", err)
	}
	if g.RoleID, err = parseNullableUUID(roleIDText); err != nil {
		return g, fmt.Errorf("dbstore: parse role_id: %w", err)
	}
	if g.CreatedBy, err = parseNullableUUID(createdByText); err != nil {
		return g, fmt.Errorf("dbstore: parse created_by: %w", err)
	}

	g.SubjectType = structure.GrantSubjectType(subjectType)
	g.TargetType = structure.GrantTargetType(targetType)
	g.PermissionKey = permissionKey
	g.Scope = structure.GrantScope(scope)
	g.ContextType = contextType
	g.ContextID = contextID
	g.Effect = structure.GrantEffect(effect)
	g.Status = structure.GrantStatus(status)
	g.Origin = structure.GrantOrigin(origin)
	g.Metadata = metadata
	return g, nil
}
