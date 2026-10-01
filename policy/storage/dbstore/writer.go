package dbstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/croge130/archipelago/policy/structure"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresWriter implements facade.Writer directly against Postgres.
// The Writer interface itself is declared in facade, its consumer,
// not here — same reasoning as every other base's PostgresWriter.
type PostgresWriter struct {
	pool *pgxpool.Pool
}

func NewPostgresWriter(pool *pgxpool.Pool) *PostgresWriter {
	return &PostgresWriter{pool: pool}
}

func (w *PostgresWriter) CreatePolicyDefinition(ctx context.Context, d structure.PolicyDefinition) error {
	if err := d.Validate(); err != nil {
		return err
	}
	valueTypeJSON, err := json.Marshal(d.ValueType)
	if err != nil {
		return fmt.Errorf("dbstore: marshal value_type: %w", err)
	}
	constraintsJSON, err := json.Marshal(d.Constraints)
	if err != nil {
		return fmt.Errorf("dbstore: marshal constraints: %w", err)
	}
	_, err = w.pool.Exec(ctx,
		`INSERT INTO policy_definitions
		   (policy_definition_id, policy_key, value_type, constraints, merge_mode, activation, default_binding, lifecycle, created_by, updated_by, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		d.PolicyDefinitionID, d.PolicyKey, valueTypeJSON, constraintsJSON, string(d.Merge),
		string(d.Activation), string(d.DefaultBinding), string(d.Lifecycle), d.CreatedBy, d.UpdatedBy, d.CreatedAt, d.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create policy definition: %w", err)
	}
	return nil
}

func (w *PostgresWriter) CreatePolicyContext(ctx context.Context, c structure.PolicyContext) error {
	if err := c.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO policy_contexts (policy_context_id, key, description, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		c.PolicyContextID, c.Key, c.Description, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create policy context: %w", err)
	}
	return nil
}

// AddPolicyContextMember is idempotent — adding the same (context,
// ref) pair twice is a no-op, not a conflict error, since membership
// has no state of its own to overwrite (same reasoning as
// gatehouse-core's own GroupMembership).
func (w *PostgresWriter) AddPolicyContextMember(ctx context.Context, m structure.PolicyContextMember) error {
	if err := m.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO policy_context_members (policy_context_id, ref_kind, ref_key, created_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (policy_context_id, ref_kind, ref_key) DO NOTHING`,
		m.PolicyContextID, m.Ref.Kind, m.Ref.Key, m.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: add policy context member: %w", err)
	}
	return nil
}

func (w *PostgresWriter) RemovePolicyContextMember(ctx context.Context, policyContextID uuid.UUID, ref structure.Ref) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`DELETE FROM policy_context_members WHERE policy_context_id = $1 AND ref_kind = $2 AND ref_key = $3`,
		policyContextID, ref.Kind, ref.Key,
	)
	if err != nil {
		return fmt.Errorf("dbstore: remove policy context member: %w", err)
	}
	return nil
}

func (w *PostgresWriter) CreatePolicyInstance(ctx context.Context, i structure.PolicyInstance) error {
	if err := i.Validate(); err != nil {
		return err
	}
	return w.upsertPolicyInstance(ctx, i, true)
}

func (w *PostgresWriter) UpdatePolicyInstance(ctx context.Context, i structure.PolicyInstance) error {
	if err := i.Validate(); err != nil {
		return err
	}
	return w.upsertPolicyInstance(ctx, i, false)
}

func (w *PostgresWriter) upsertPolicyInstance(ctx context.Context, i structure.PolicyInstance, insert bool) error {
	valueJSON, err := json.Marshal(i.Value)
	if err != nil {
		return fmt.Errorf("dbstore: marshal value: %w", err)
	}
	var refKind, refKey *string
	if i.Ref != nil {
		refKind, refKey = &i.Ref.Kind, &i.Ref.Key
	}

	if insert {
		_, err = w.pool.Exec(ctx,
			`INSERT INTO policy_instances
			   (policy_instance_id, policy_definition_id, target_kind, ref_kind, ref_key, policy_context_id,
			    value, binding_mode, lifecycle, metadata, created_by, updated_by, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			i.PolicyInstanceID, i.PolicyDefinitionID, string(i.TargetKind), refKind, refKey, i.PolicyContextID,
			valueJSON, string(i.Binding), string(i.Lifecycle), nullableJSON(i.Metadata), i.CreatedBy, i.UpdatedBy, i.CreatedAt, i.UpdatedAt,
		)
	} else {
		_, err = w.pool.Exec(ctx,
			`UPDATE policy_instances SET
			   value = $2, binding_mode = $3, lifecycle = $4, metadata = $5, updated_by = $6, updated_at = $7
			 WHERE policy_instance_id = $1`,
			i.PolicyInstanceID, valueJSON, string(i.Binding), string(i.Lifecycle), nullableJSON(i.Metadata), i.UpdatedBy, i.UpdatedAt,
		)
	}
	if err != nil {
		return fmt.Errorf("dbstore: upsert policy instance: %w", err)
	}
	return nil
}

func nullableJSON(data []byte) any {
	if len(data) == 0 {
		return nil
	}
	return data
}
