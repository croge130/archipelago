package dbstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/policy/structure"
	"github.com/croge130/archipelago/typeconstraints"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresReader implements evaluation.Store directly against
// Postgres, plus the extra read methods facade needs beyond it —
// same split as every other base's own PostgresReader.
type PostgresReader struct {
	pool *pgxpool.Pool
}

func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{pool: pool}
}

func (r *PostgresReader) GetPolicyDefinition(ctx context.Context, policyKey string) (structure.PolicyDefinition, bool, error) {
	var d structure.PolicyDefinition
	var valueTypeJSON, constraintsJSON []byte
	var merge, activation, defaultBinding, lifecycle string
	err := r.pool.QueryRow(ctx,
		`SELECT policy_definition_id, policy_key, value_type, constraints, merge_mode,
		        activation, default_binding, lifecycle, created_by, updated_by, created_at, updated_at
		 FROM policy_definitions WHERE policy_key = $1`,
		policyKey,
	).Scan(&d.PolicyDefinitionID, &d.PolicyKey, &valueTypeJSON, &constraintsJSON, &merge,
		&activation, &defaultBinding, &lifecycle, &d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.PolicyDefinition{}, false, nil
		}
		return structure.PolicyDefinition{}, false, fmt.Errorf("dbstore: get policy definition: %w", err)
	}
	if d.ValueType, err = unmarshalValueType(valueTypeJSON); err != nil {
		return structure.PolicyDefinition{}, false, err
	}
	if d.Constraints, err = unmarshalConstraints(constraintsJSON); err != nil {
		return structure.PolicyDefinition{}, false, err
	}
	d.Merge = typeconstraints.MergeMode(merge)
	d.Activation = structure.ActivationMode(activation)
	d.DefaultBinding = structure.BindingMode(defaultBinding)
	d.Lifecycle = structure.Lifecycle(lifecycle)
	return d, true, nil
}

func (r *PostgresReader) GlobalInstance(ctx context.Context, policyDefinitionID uuid.UUID) (structure.PolicyInstance, bool, error) {
	rows, err := r.queryInstances(ctx,
		`WHERE i.policy_definition_id = $1 AND i.target_kind = 'global' AND i.lifecycle = 'active'`,
		policyDefinitionID,
	)
	if err != nil {
		return structure.PolicyInstance{}, false, err
	}
	if len(rows) == 0 {
		return structure.PolicyInstance{}, false, nil
	}
	return rows[0], true, nil
}

func (r *PostgresReader) RefInstances(ctx context.Context, policyDefinitionID uuid.UUID, refs []structure.Ref) ([]structure.PolicyInstance, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	kinds, keys := refSlices(refs)
	return r.queryInstances(ctx,
		`WHERE i.policy_definition_id = $1 AND i.target_kind = 'ref' AND i.lifecycle = 'active'
		   AND (i.ref_kind, i.ref_key) IN (SELECT * FROM unnest($2::text[], $3::text[]))`,
		policyDefinitionID, kinds, keys,
	)
}

func (r *PostgresReader) PolicyContextIDsForRefs(ctx context.Context, refs []structure.Ref) ([]uuid.UUID, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	kinds, keys := refSlices(refs)
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT policy_context_id FROM policy_context_members
		 WHERE (ref_kind, ref_key) IN (SELECT * FROM unnest($1::text[], $2::text[]))`,
		kinds, keys,
	)
	if err != nil {
		return nil, fmt.Errorf("dbstore: policy context ids for refs: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("dbstore: policy context ids for refs: scan: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *PostgresReader) PolicyContextInstances(ctx context.Context, policyDefinitionID uuid.UUID, policyContextIDs []uuid.UUID) ([]structure.PolicyInstance, error) {
	if len(policyContextIDs) == 0 {
		return nil, nil
	}
	return r.queryInstances(ctx,
		`WHERE i.policy_definition_id = $1 AND i.target_kind = 'policy_context' AND i.lifecycle = 'active'
		   AND i.policy_context_id = ANY($2::uuid[])`,
		policyDefinitionID, policyContextIDs,
	)
}

// ActiveNonGlobalInstances returns every active ref- or
// policy_context-targeted instance for a definition — used by facade
// to enforce the conservative, definition-wide exclusivity rule for
// non-commutative merge modes (see facade's own doc comment for why
// that rule is deliberately simpler than a true membership-overlap
// check).
func (r *PostgresReader) ActiveNonGlobalInstances(ctx context.Context, policyDefinitionID uuid.UUID) ([]structure.PolicyInstance, error) {
	return r.queryInstances(ctx,
		`WHERE i.policy_definition_id = $1 AND i.target_kind != 'global' AND i.lifecycle = 'active'`,
		policyDefinitionID,
	)
}

func (r *PostgresReader) GetPolicyInstance(ctx context.Context, id uuid.UUID) (structure.PolicyInstance, bool, error) {
	rows, err := r.queryInstances(ctx, `WHERE i.policy_instance_id = $1`, id)
	if err != nil {
		return structure.PolicyInstance{}, false, err
	}
	if len(rows) == 0 {
		return structure.PolicyInstance{}, false, nil
	}
	return rows[0], true, nil
}

func (r *PostgresReader) GetPolicyContext(ctx context.Context, id uuid.UUID) (structure.PolicyContext, bool, error) {
	return r.scanPolicyContext(ctx, `WHERE policy_context_id = $1`, id)
}

func (r *PostgresReader) GetPolicyContextByKey(ctx context.Context, key string) (structure.PolicyContext, bool, error) {
	return r.scanPolicyContext(ctx, `WHERE key = $1`, key)
}

func (r *PostgresReader) ListPolicyContextMembers(ctx context.Context, policyContextID uuid.UUID) ([]structure.Ref, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ref_kind, ref_key FROM policy_context_members WHERE policy_context_id = $1`,
		policyContextID,
	)
	if err != nil {
		return nil, fmt.Errorf("dbstore: list policy context members: %w", err)
	}
	defer rows.Close()

	var out []structure.Ref
	for rows.Next() {
		var ref structure.Ref
		if err := rows.Scan(&ref.Kind, &ref.Key); err != nil {
			return nil, fmt.Errorf("dbstore: list policy context members: scan: %w", err)
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (r *PostgresReader) scanPolicyContext(ctx context.Context, where string, args ...any) (structure.PolicyContext, bool, error) {
	var c structure.PolicyContext
	err := r.pool.QueryRow(ctx,
		`SELECT policy_context_id, key, description, created_at, updated_at FROM policy_contexts `+where,
		args...,
	).Scan(&c.PolicyContextID, &c.Key, &c.Description, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.PolicyContext{}, false, nil
		}
		return structure.PolicyContext{}, false, fmt.Errorf("dbstore: get policy context: %w", err)
	}
	return c, true, nil
}

// queryInstances runs a SELECT over policy_instances joined against
// policy_definitions (for the value_type needed to re-normalize the
// stored value's Go type), with whereClause appended as-is. whereClause
// is always a compile-time-constant string built by this file's own
// methods, never request-derived text, so this stays within the
// parameterized-everything rule despite the string concatenation.
func (r *PostgresReader) queryInstances(ctx context.Context, whereClause string, args ...any) ([]structure.PolicyInstance, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT i.policy_instance_id, i.policy_definition_id, i.target_kind, i.ref_kind, i.ref_key,
		        i.policy_context_id, i.value, i.binding_mode, i.lifecycle, i.metadata,
		        i.created_by, i.updated_by, i.created_at, i.updated_at, d.value_type
		 FROM policy_instances i
		 JOIN policy_definitions d ON d.policy_definition_id = i.policy_definition_id
		 `+whereClause,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("dbstore: query policy instances: %w", err)
	}
	defer rows.Close()

	var out []structure.PolicyInstance
	for rows.Next() {
		var inst structure.PolicyInstance
		var targetKind, binding, lifecycle string
		var refKind, refKey *string
		var valueJSON, valueTypeJSON, metadataJSON []byte
		if err := rows.Scan(&inst.PolicyInstanceID, &inst.PolicyDefinitionID, &targetKind, &refKind, &refKey,
			&inst.PolicyContextID, &valueJSON, &binding, &lifecycle, &metadataJSON,
			&inst.CreatedBy, &inst.UpdatedBy, &inst.CreatedAt, &inst.UpdatedAt, &valueTypeJSON); err != nil {
			return nil, fmt.Errorf("dbstore: query policy instances: scan: %w", err)
		}
		inst.TargetKind = structure.TargetKind(targetKind)
		inst.Binding = structure.BindingMode(binding)
		inst.Lifecycle = structure.Lifecycle(lifecycle)
		inst.Metadata = metadataJSON
		if refKind != nil && refKey != nil {
			inst.Ref = &structure.Ref{Kind: *refKind, Key: *refKey}
		}

		valueType, err := unmarshalValueType(valueTypeJSON)
		if err != nil {
			return nil, err
		}
		if inst.Value, err = normalizeStoredValue(valueType, valueJSON); err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

func refSlices(refs []structure.Ref) (kinds, keys []string) {
	kinds = make([]string, len(refs))
	keys = make([]string, len(refs))
	for i, r := range refs {
		kinds[i] = r.Kind
		keys[i] = r.Key
	}
	return kinds, keys
}
