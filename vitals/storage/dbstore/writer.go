package dbstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresWriter implements facade.Writer directly against Postgres.
// The Writer interface itself is declared in facade, its consumer —
// same rule every other module's own PostgresWriter follows.
type PostgresWriter struct {
	pool *pgxpool.Pool
}

func NewPostgresWriter(pool *pgxpool.Pool) *PostgresWriter {
	return &PostgresWriter{pool: pool}
}

func ttlSeconds(d *structure.Definition) any {
	if d.DefaultTTL == nil {
		return nil
	}
	return int64(d.DefaultTTL.Seconds())
}

// CreateDefinition inserts a new definition — a raw insert; the
// facade's EnsureDefinition decides key+version+hash idempotency
// before calling this, the same split every other Create* writer
// method in this design uses.
func (w *PostgresWriter) CreateDefinition(ctx context.Context, d structure.Definition) error {
	if err := d.Validate(); err != nil {
		return err
	}
	var valueMetadata []byte
	if d.ValueMetadata != nil {
		var err error
		if valueMetadata, err = json.Marshal(d.ValueMetadata); err != nil {
			return fmt.Errorf("dbstore: marshal value_metadata: %w", err)
		}
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO vitals_definitions (
			definition_id, definition_key, definition_version, schema_hash, title, description,
			value_metadata, app_value_metadata, allowed_states, default_expected_states, default_importance,
			default_ttl_seconds, default_display_hints, app_metadata, created_at, updated_at, archived_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		uuidToText(d.DefinitionID), d.DefinitionKey, d.DefinitionVersion, d.SchemaHash, d.Title, d.Description,
		valueMetadata, nullableJSON(d.AppValueMetadata), textFromStates(d.AllowedStates), textFromStates(d.DefaultExpectedStates),
		string(d.DefaultImportance), ttlSeconds(&d), nullableJSON(d.DefaultDisplayHints), nullableJSON(d.AppMetadata),
		d.CreatedAt, d.UpdatedAt, d.ArchivedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create definition: %w", err)
	}
	return nil
}

// CreateInstance inserts a new instance — a raw insert; the facade's
// EnsureInstance decides (scope_type, scope_id, instance_key)
// idempotency before calling this.
func (w *PostgresWriter) CreateInstance(ctx context.Context, i structure.Instance) error {
	if err := i.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO vitals_instances (
			instance_id, scope_type, scope_id, instance_key, definition_id, subject_type, subject_key,
			display_name, description, category_path, expected_states, importance, reference_ranges, display_hints,
			resource_uri, external_uri, detail_route, app_metadata, created_at, updated_at, archived_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		uuidToText(i.InstanceID), i.ScopeType, i.ScopeID, i.InstanceKey, uuidToText(i.DefinitionID), i.SubjectType, i.SubjectKey,
		i.DisplayName, i.Description, orEmptyStrings(i.CategoryPath), textFromStates(i.ExpectedStates), string(i.Importance),
		nullableJSON(i.ReferenceRanges), nullableJSON(i.DisplayHints), i.ResourceURI, i.ExternalURI, i.DetailRoute,
		nullableJSON(i.AppMetadata), i.CreatedAt, i.UpdatedAt, i.ArchivedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create instance: %w", err)
	}
	return nil
}

// spanContextToText extracts the three hex-text columns from sc, all
// nil if sc itself is nil — the common case of a reading with no
// trace context at all.
func spanContextToText(sc *logging.SpanContext) (traceID, spanID, parentSpanID *string) {
	if sc == nil {
		return nil, nil, nil
	}
	t, s := sc.TraceID.String(), sc.SpanID.String()
	traceID, spanID = &t, &s
	if !sc.ParentSpanID.IsZero() {
		p := sc.ParentSpanID.String()
		parentSpanID = &p
	}
	return traceID, spanID, parentSpanID
}

// UpsertReading sets the full current-reading row for r.InstanceID —
// a raw set, like Alias's own UpsertAlias; the facade's WriteReading
// decides the desired Revision and whether a history row is also
// needed before calling this.
func (w *PostgresWriter) UpsertReading(ctx context.Context, r structure.Reading) error {
	if err := r.Validate(); err != nil {
		return err
	}
	warnings, err := json.Marshal(r.QualityWarnings)
	if err != nil {
		return fmt.Errorf("dbstore: marshal quality_warnings: %w", err)
	}
	traceID, spanID, parentSpanID := spanContextToText(r.TraceContext)
	_, err = w.pool.Exec(ctx,
		`INSERT INTO vitals_current_readings (
			instance_id, state, impact, impact_score, value, summary, reason_code, details_json,
			observed_at, updated_at, expires_at, source_instance_id, actor_principal_id,
			trace_id, span_id, parent_span_id, quality_warnings, revision
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		 ON CONFLICT (instance_id) DO UPDATE SET
			state = EXCLUDED.state, impact = EXCLUDED.impact, impact_score = EXCLUDED.impact_score,
			value = EXCLUDED.value, summary = EXCLUDED.summary, reason_code = EXCLUDED.reason_code,
			details_json = EXCLUDED.details_json, observed_at = EXCLUDED.observed_at,
			updated_at = EXCLUDED.updated_at, expires_at = EXCLUDED.expires_at,
			source_instance_id = EXCLUDED.source_instance_id, actor_principal_id = EXCLUDED.actor_principal_id,
			trace_id = EXCLUDED.trace_id, span_id = EXCLUDED.span_id, parent_span_id = EXCLUDED.parent_span_id,
			quality_warnings = EXCLUDED.quality_warnings, revision = EXCLUDED.revision`,
		uuidToText(r.InstanceID), string(r.State), string(r.Impact), r.ImpactScore, nullableJSONOrNil(r.Value),
		r.Summary, r.ReasonCode, nullableJSON(r.DetailsJSON), r.ObservedAt, r.UpdatedAt, r.ExpiresAt,
		nullableUUIDToText(r.SourceInstanceID), nullableUUIDToText(r.ActorPrincipalID),
		traceID, spanID, parentSpanID, warnings, r.Revision,
	)
	if err != nil {
		return fmt.Errorf("dbstore: upsert reading: %w", err)
	}
	return nil
}

// nullableJSONOrNil is nullableJSON's counterpart for a genuinely
// nullable jsonb column (vitals_current_readings.value) — unlike
// app-metadata-shaped columns, "no value was ever reported" and "an
// empty object was reported" are different, real states here, so this
// stays NULL rather than defaulting to '{}'.
func nullableJSONOrNil(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// InsertHistory records a notable transition — see
// evaluation.IsNotableTransition for what counts as notable. The
// facade decides that; this is a raw insert.
func (w *PostgresWriter) InsertHistory(ctx context.Context, h structure.HistoryEntry) error {
	if h.HistoryID == uuid.Nil {
		return fmt.Errorf("dbstore: insert history: HistoryID is required")
	}
	if err := h.Reading.Validate(); err != nil {
		return err
	}
	warnings, err := json.Marshal(h.QualityWarnings)
	if err != nil {
		return fmt.Errorf("dbstore: marshal quality_warnings: %w", err)
	}
	traceID, spanID, parentSpanID := spanContextToText(h.TraceContext)
	_, err = w.pool.Exec(ctx,
		`INSERT INTO vitals_reading_history (
			history_id, instance_id, state, impact, impact_score, value, summary, reason_code, details_json,
			observed_at, updated_at, expires_at, source_instance_id, actor_principal_id,
			trace_id, span_id, parent_span_id, quality_warnings, revision
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		uuidToText(h.HistoryID), uuidToText(h.InstanceID), string(h.State), string(h.Impact), h.ImpactScore,
		nullableJSONOrNil(h.Value), h.Summary, h.ReasonCode, nullableJSON(h.DetailsJSON), h.ObservedAt, h.UpdatedAt,
		h.ExpiresAt, nullableUUIDToText(h.SourceInstanceID), nullableUUIDToText(h.ActorPrincipalID),
		traceID, spanID, parentSpanID, warnings, h.Revision,
	)
	if err != nil {
		return fmt.Errorf("dbstore: insert history: %w", err)
	}
	return nil
}

// CreateGroup inserts a new group — a raw insert; the facade's
// EnsureGroup decides (scope_type, scope_id, group_key) idempotency.
func (w *PostgresWriter) CreateGroup(ctx context.Context, g structure.Group) error {
	if err := g.Validate(); err != nil {
		return err
	}
	_, err := w.pool.Exec(ctx,
		`INSERT INTO vitals_groups (
			group_id, scope_type, scope_id, group_key, title, description, sort_order,
			display_hints, app_metadata, created_at, updated_at, archived_at
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		uuidToText(g.GroupID), g.ScopeType, g.ScopeID, g.GroupKey, g.Title, g.Description, g.SortOrder,
		nullableJSON(g.DisplayHints), nullableJSON(g.AppMetadata), g.CreatedAt, g.UpdatedAt, g.ArchivedAt,
	)
	if err != nil {
		return fmt.Errorf("dbstore: create group: %w", err)
	}
	return nil
}

// ErrGroupMembershipCycle is returned by UpsertGroupMember when adding
// a child-group edge would let the containing group reach itself
// through some chain of child groups.
var ErrGroupMembershipCycle = fmt.Errorf("dbstore: vital group membership would create a cycle")

// UpsertGroupMember sets one (group_id, member_key) row. When the
// member is itself a group, this checks — inside the same call, via a
// recursive query — that child_group_id cannot already reach group_id
// through existing child-group edges, the same shape Lighthouse's own
// validateVitalGroupMembershipCycle uses, ported because none of it is
// realm-shaped.
func (w *PostgresWriter) UpsertGroupMember(ctx context.Context, m structure.GroupMember) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if m.MemberKind == structure.MemberKindVitalGroup {
		var hasCycle bool
		err := w.pool.QueryRow(ctx, `
			WITH RECURSIVE descendants(group_id) AS (
				SELECT $2::uuid
				UNION
				SELECT gm.child_group_id
				FROM vitals_group_members gm
				JOIN descendants d ON d.group_id = gm.group_id
				WHERE gm.member_kind = 'vital_group' AND gm.child_group_id IS NOT NULL
			)
			SELECT EXISTS (SELECT 1 FROM descendants WHERE group_id = $1::uuid)`,
			uuidToText(m.GroupID), uuidToText(*m.ChildGroupID),
		).Scan(&hasCycle)
		if err != nil {
			return fmt.Errorf("dbstore: check group membership cycle: %w", err)
		}
		if hasCycle {
			return ErrGroupMembershipCycle
		}
	}

	_, err := w.pool.Exec(ctx,
		`INSERT INTO vitals_group_members (
			group_id, member_key, member_kind, vital_instance_id, child_group_id,
			label, sort_order, required, display_hints, app_metadata
		 ) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (group_id, member_key) DO UPDATE SET
			member_kind = EXCLUDED.member_kind, vital_instance_id = EXCLUDED.vital_instance_id,
			child_group_id = EXCLUDED.child_group_id, label = EXCLUDED.label, sort_order = EXCLUDED.sort_order,
			required = EXCLUDED.required, display_hints = EXCLUDED.display_hints, app_metadata = EXCLUDED.app_metadata`,
		uuidToText(m.GroupID), m.MemberKey, string(m.MemberKind), nullableUUIDToText(m.VitalInstanceID),
		nullableUUIDToText(m.ChildGroupID), m.Label, m.SortOrder, m.Required,
		nullableJSON(m.DisplayHints), nullableJSON(m.AppMetadata),
	)
	if err != nil {
		return fmt.Errorf("dbstore: upsert group member: %w", err)
	}
	return nil
}

// DeleteGroupMember removes one (group_id, member_key) row —
// unconditionally successful whether or not it existed, same
// idempotent-release shape as ReleaseAlias/RevokeSession.
func (w *PostgresWriter) DeleteGroupMember(ctx context.Context, groupID uuid.UUID, memberKey string) error {
	_, err := w.pool.Exec(ctx,
		`DELETE FROM vitals_group_members WHERE group_id = $1 AND member_key = $2`,
		uuidToText(groupID), memberKey,
	)
	if err != nil {
		return fmt.Errorf("dbstore: delete group member: %w", err)
	}
	return nil
}
