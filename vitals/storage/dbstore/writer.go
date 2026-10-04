package dbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/vitals/evaluation"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// execer is satisfied by both *pgxpool.Pool and pgx.Tx — the small
// interface that lets upsertReading/insertHistory run either as
// standalone statements or as steps inside WriteReadingAtomic's own
// transaction, without duplicating their SQL.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

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
// a raw set, like Alias's own UpsertAlias. Callers that need the
// decide-then-write sequence (computing Revision and quality warnings
// against whatever's currently stored) serialized against concurrent
// writers to the *same* instance should use WriteReadingAtomic below
// instead of calling this directly with a value computed from a
// separate, unlocked read.
func (w *PostgresWriter) UpsertReading(ctx context.Context, r structure.Reading) error {
	if err := r.Validate(); err != nil {
		return err
	}
	return upsertReading(ctx, w.pool, r)
}

func upsertReading(ctx context.Context, exec execer, r structure.Reading) error {
	warnings, err := json.Marshal(r.QualityWarnings)
	if err != nil {
		return fmt.Errorf("dbstore: marshal quality_warnings: %w", err)
	}
	traceID, spanID, parentSpanID := spanContextToText(r.TraceContext)
	_, err = exec.Exec(ctx,
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
// evaluation.IsNotableTransition for what counts as notable. A raw
// insert; WriteReadingAtomic below is what decides whether to call
// this at all, inside the same transaction as its own UpsertReading.
func (w *PostgresWriter) InsertHistory(ctx context.Context, h structure.HistoryEntry) error {
	if h.HistoryID == uuid.Nil {
		return fmt.Errorf("dbstore: insert history: HistoryID is required")
	}
	if err := h.Reading.Validate(); err != nil {
		return err
	}
	return insertHistory(ctx, w.pool, h)
}

func insertHistory(ctx context.Context, exec execer, h structure.HistoryEntry) error {
	warnings, err := json.Marshal(h.QualityWarnings)
	if err != nil {
		return fmt.Errorf("dbstore: marshal quality_warnings: %w", err)
	}
	traceID, spanID, parentSpanID := spanContextToText(h.TraceContext)
	_, err = exec.Exec(ctx,
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

// WriteReadingAtomic is WriteReading's real fix: the previous-reading
// read, the Revision/quality-warning decision, the current-row write,
// and the conditional history insert all run inside one transaction,
// with the previous row (when one exists) locked via SELECT ... FOR
// UPDATE for the duration. Without this, two concurrent writers to the
// same InstanceID could each read the same previous revision, each
// decide "notable" against the same stale baseline, and silently
// overwrite one another with no error and no signal — worse than the
// Ensure*-style first-creation races elsewhere in this codebase, since
// it recurs on every write rather than only the first one. expectedStates
// is the caller's already-resolved structure.ExpectedStatesOrDefault
// result (facade.WriteReading computes it from the Instance/Definition,
// pure reads with no race to protect); this function only owns the
// part that genuinely needs serializing.
//
// A brand-new InstanceID's very first write is the one case this
// doesn't fully close — SELECT ... FOR UPDATE on a row that doesn't
// exist yet locks nothing, so two concurrent first writes can still
// both compute Revision 1. That's the same accepted Ensure*-shaped
// race as everywhere else in this codebase (narrow, first-moment-only,
// and here additionally harmless: Postgres's own ON CONFLICT handling
// in upsertReading still produces a single consistent row, just with
// whichever of the two writes committed last).
func (w *PostgresWriter) WriteReadingAtomic(ctx context.Context, reading structure.Reading, expectedStates []structure.State) (structure.Reading, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return structure.Reading{}, fmt.Errorf("dbstore: write reading: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `SELECT `+readingSelectFields+` FROM vitals_current_readings WHERE instance_id = $1 FOR UPDATE`, uuidToText(reading.InstanceID))
	previous, err := scanReading(row)
	hadPrevious := true
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return structure.Reading{}, fmt.Errorf("dbstore: write reading: %w", err)
		}
		hadPrevious = false
	}

	reading.QualityWarnings = evaluation.QualityWarnings(reading, expectedStates)
	reading.Revision = 1
	if hadPrevious {
		reading.Revision = previous.Revision + 1
	}
	if err := reading.Validate(); err != nil {
		return structure.Reading{}, fmt.Errorf("dbstore: write reading: %w", err)
	}

	if err := upsertReading(ctx, tx, reading); err != nil {
		return structure.Reading{}, err
	}
	if evaluation.IsNotableTransition(previous, reading, hadPrevious) {
		if err := insertHistory(ctx, tx, structure.HistoryEntry{HistoryID: uuid.New(), Reading: reading}); err != nil {
			return structure.Reading{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return structure.Reading{}, fmt.Errorf("dbstore: write reading: commit: %w", err)
	}
	return reading, nil
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

// groupMembershipLockKey is a fixed key for pg_advisory_xact_lock,
// shared by every UpsertGroupMember call regardless of which group is
// involved. See that function's own doc comment for why a single
// global serialization point, rather than a per-group one, is the
// correct scope for this lock.
const groupMembershipLockKey = "vitals_group_members"

// UpsertGroupMember sets one (group_id, member_key) row. When the
// member is itself a group, this checks — via a recursive query —
// that child_group_id cannot already reach group_id through existing
// child-group edges, the same shape Lighthouse's own
// validateVitalGroupMembershipCycle uses, ported because none of it is
// realm-shaped.
//
// The check and the insert run inside one transaction, serialized
// against every other concurrent UpsertGroupMember call (for any
// group, not just this one) by a session-wide advisory lock taken
// first. Without it, two concurrent calls adding complementary edges
// — group A including group B, group B including group A — could
// each run the cycle check against the pre-commit state of the other
// and both pass, writing an actual cycle into the data despite the
// check. A global lock (rather than one scoped to the two groups
// involved) is a deliberate simplification: group-membership changes
// are rare, administrative operations, not a hot path, so there's no
// real concurrency to give up by serializing all of them against each
// other — the same "simpler until a real case proves otherwise"
// choice SetPolicyInstance's own doc comment makes for its own
// exclusivity rule.
func (w *PostgresWriter) UpsertGroupMember(ctx context.Context, m structure.GroupMember) error {
	if err := m.Validate(); err != nil {
		return err
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("dbstore: upsert group member: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, groupMembershipLockKey); err != nil {
		return fmt.Errorf("dbstore: upsert group member: acquire lock: %w", err)
	}

	if m.MemberKind == structure.MemberKindVitalGroup {
		var hasCycle bool
		err := tx.QueryRow(ctx, `
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

	if _, err := tx.Exec(ctx,
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
	); err != nil {
		return fmt.Errorf("dbstore: upsert group member: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("dbstore: upsert group member: commit: %w", err)
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
