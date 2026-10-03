package dbstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/typedvalue"
	"github.com/croge130/archipelago/vitals/structure"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresReader implements every read Vitals needs directly against
// Postgres. Kept separate from PostgresWriter — see gatehouse-core's
// own dbstore doc comment for why reads and writes get their own
// types, unchanged reasoning here.
type PostgresReader struct {
	pool *pgxpool.Pool
}

func NewPostgresReader(pool *pgxpool.Pool) *PostgresReader {
	return &PostgresReader{pool: pool}
}

// rowScanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows
// (Query, row by row via Next) — the small scanner interface this
// package's scanX helpers share, same pattern gatehouse-core's own
// reader.go uses.
type rowScanner interface {
	Scan(dest ...any) error
}

func statesFromText(raw []string) []structure.State {
	if len(raw) == 0 {
		return nil
	}
	states := make([]structure.State, len(raw))
	for i, s := range raw {
		states[i] = structure.State(s)
	}
	return states
}

func textFromStates(states []structure.State) []string {
	raw := make([]string, len(states))
	for i, s := range states {
		raw[i] = string(s)
	}
	return raw
}

// orEmptyStrings defaults a nil slice to an empty one — pgx encodes a
// nil Go slice as SQL NULL for a text[] parameter, which a NOT NULL
// array column (category_path) rejects outright. A Go zero-value
// Instance.CategoryPath is nil, not an empty slice, so this is the
// actual fix rather than something to avoid triggering.
func orEmptyStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

const definitionSelectFields = `definition_id, definition_key, definition_version, schema_hash, title, description,
	value_metadata, app_value_metadata, allowed_states, default_expected_states, default_importance,
	default_ttl_seconds, default_display_hints, app_metadata, created_at, updated_at, archived_at`

func scanDefinition(row rowScanner) (structure.Definition, error) {
	var d structure.Definition
	var idText string
	var valueMetadata, appValueMetadata, defaultDisplayHints, appMetadata []byte
	var allowedStates, defaultExpectedStates []string
	var defaultImportance string
	var defaultTTLSeconds *int64

	if err := row.Scan(
		&idText, &d.DefinitionKey, &d.DefinitionVersion, &d.SchemaHash, &d.Title, &d.Description,
		&valueMetadata, &appValueMetadata, &allowedStates, &defaultExpectedStates, &defaultImportance,
		&defaultTTLSeconds, &defaultDisplayHints, &appMetadata, &d.CreatedAt, &d.UpdatedAt, &d.ArchivedAt,
	); err != nil {
		return d, fmt.Errorf("dbstore: scan definition: %w", err)
	}

	var err error
	if d.DefinitionID, err = parseUUID(idText); err != nil {
		return d, fmt.Errorf("dbstore: parse definition_id: %w", err)
	}
	if len(valueMetadata) > 0 {
		var vm typedvalue.Definition
		if err := json.Unmarshal(valueMetadata, &vm); err != nil {
			return d, fmt.Errorf("dbstore: unmarshal value_metadata: %w", err)
		}
		d.ValueMetadata = &vm
	}
	d.AppValueMetadata = appValueMetadata
	d.AllowedStates = statesFromText(allowedStates)
	d.DefaultExpectedStates = statesFromText(defaultExpectedStates)
	d.DefaultImportance = structure.Importance(defaultImportance)
	if defaultTTLSeconds != nil {
		ttl := time.Duration(*defaultTTLSeconds) * time.Second
		d.DefaultTTL = &ttl
	}
	d.DefaultDisplayHints = defaultDisplayHints
	d.AppMetadata = appMetadata
	return d, nil
}

func (r *PostgresReader) GetDefinition(ctx context.Context, id uuid.UUID) (structure.Definition, bool, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+definitionSelectFields+` FROM vitals_definitions WHERE definition_id = $1`, uuidToText(id))
	d, err := scanDefinition(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Definition{}, false, nil
		}
		return structure.Definition{}, false, err
	}
	return d, true, nil
}

func (r *PostgresReader) GetDefinitionByKeyVersion(ctx context.Context, key string, version int) (structure.Definition, bool, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+definitionSelectFields+` FROM vitals_definitions WHERE definition_key = $1 AND definition_version = $2`,
		key, version,
	)
	d, err := scanDefinition(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Definition{}, false, nil
		}
		return structure.Definition{}, false, err
	}
	return d, true, nil
}

const instanceSelectFields = `instance_id, scope_type, scope_id, instance_key, definition_id, subject_type, subject_key,
	display_name, description, category_path, expected_states, importance, reference_ranges, display_hints,
	resource_uri, external_uri, detail_route, app_metadata, created_at, updated_at, archived_at`

func scanInstance(row rowScanner) (structure.Instance, error) {
	var i structure.Instance
	var idText, definitionIDText string
	var categoryPath, expectedStates []string
	var importance string
	var referenceRanges, displayHints, appMetadata []byte

	if err := row.Scan(
		&idText, &i.ScopeType, &i.ScopeID, &i.InstanceKey, &definitionIDText, &i.SubjectType, &i.SubjectKey,
		&i.DisplayName, &i.Description, &categoryPath, &expectedStates, &importance, &referenceRanges, &displayHints,
		&i.ResourceURI, &i.ExternalURI, &i.DetailRoute, &appMetadata, &i.CreatedAt, &i.UpdatedAt, &i.ArchivedAt,
	); err != nil {
		return i, fmt.Errorf("dbstore: scan instance: %w", err)
	}

	var err error
	if i.InstanceID, err = parseUUID(idText); err != nil {
		return i, fmt.Errorf("dbstore: parse instance_id: %w", err)
	}
	if i.DefinitionID, err = parseUUID(definitionIDText); err != nil {
		return i, fmt.Errorf("dbstore: parse definition_id: %w", err)
	}
	i.CategoryPath = categoryPath
	i.ExpectedStates = statesFromText(expectedStates)
	i.Importance = structure.Importance(importance)
	i.ReferenceRanges = referenceRanges
	i.DisplayHints = displayHints
	i.AppMetadata = appMetadata
	return i, nil
}

func (r *PostgresReader) GetInstance(ctx context.Context, id uuid.UUID) (structure.Instance, bool, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+instanceSelectFields+` FROM vitals_instances WHERE instance_id = $1`, uuidToText(id))
	i, err := scanInstance(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Instance{}, false, nil
		}
		return structure.Instance{}, false, err
	}
	return i, true, nil
}

func (r *PostgresReader) GetInstanceByScopeKey(ctx context.Context, scopeType, scopeID, instanceKey string) (structure.Instance, bool, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+instanceSelectFields+` FROM vitals_instances WHERE scope_type = $1 AND scope_id = $2 AND instance_key = $3`,
		scopeType, scopeID, instanceKey,
	)
	i, err := scanInstance(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Instance{}, false, nil
		}
		return structure.Instance{}, false, err
	}
	return i, true, nil
}

const readingSelectFields = `instance_id, state, impact, impact_score, value, summary, reason_code, details_json,
	observed_at, updated_at, expires_at, source_instance_id, actor_principal_id, trace_id, span_id, parent_span_id,
	quality_warnings, revision`

func scanReading(row rowScanner) (structure.Reading, error) {
	var rd structure.Reading
	var instanceIDText string
	var state, impact string
	var value, detailsJSON, qualityWarnings []byte
	var sourceInstanceIDText, actorPrincipalIDText, traceIDText, spanIDText, parentSpanIDText *string

	if err := row.Scan(
		&instanceIDText, &state, &impact, &rd.ImpactScore, &value, &rd.Summary, &rd.ReasonCode, &detailsJSON,
		&rd.ObservedAt, &rd.UpdatedAt, &rd.ExpiresAt, &sourceInstanceIDText, &actorPrincipalIDText,
		&traceIDText, &spanIDText, &parentSpanIDText, &qualityWarnings, &rd.Revision,
	); err != nil {
		return rd, fmt.Errorf("dbstore: scan reading: %w", err)
	}

	var err error
	if rd.InstanceID, err = parseUUID(instanceIDText); err != nil {
		return rd, fmt.Errorf("dbstore: parse instance_id: %w", err)
	}
	rd.State = structure.State(state)
	rd.Impact = structure.Impact(impact)
	rd.Value = value
	rd.DetailsJSON = detailsJSON
	if rd.SourceInstanceID, err = parseNullableUUID(sourceInstanceIDText); err != nil {
		return rd, fmt.Errorf("dbstore: parse source_instance_id: %w", err)
	}
	if rd.ActorPrincipalID, err = parseNullableUUID(actorPrincipalIDText); err != nil {
		return rd, fmt.Errorf("dbstore: parse actor_principal_id: %w", err)
	}
	if sc, err := spanContextFromText(traceIDText, spanIDText, parentSpanIDText); err != nil {
		return rd, fmt.Errorf("dbstore: parse trace context: %w", err)
	} else {
		rd.TraceContext = sc
	}
	if len(qualityWarnings) > 0 {
		if err := json.Unmarshal(qualityWarnings, &rd.QualityWarnings); err != nil {
			return rd, fmt.Errorf("dbstore: unmarshal quality_warnings: %w", err)
		}
	}
	return rd, nil
}

// spanContextFromText rebuilds a *logging.SpanContext from its three
// hex-text columns — nil if traceID/spanID are absent, since a
// reading with no trace context at all is the common case.
func spanContextFromText(traceID, spanID, parentSpanID *string) (*logging.SpanContext, error) {
	if traceID == nil || spanID == nil {
		return nil, nil
	}
	tid, err := logging.ParseTraceID(*traceID)
	if err != nil {
		return nil, fmt.Errorf("trace_id: %w", err)
	}
	sid, err := logging.ParseSpanID(*spanID)
	if err != nil {
		return nil, fmt.Errorf("span_id: %w", err)
	}
	sc := &logging.SpanContext{TraceID: tid, SpanID: sid}
	if parentSpanID != nil {
		psid, err := logging.ParseSpanID(*parentSpanID)
		if err != nil {
			return nil, fmt.Errorf("parent_span_id: %w", err)
		}
		sc.ParentSpanID = psid
	}
	return sc, nil
}

func (r *PostgresReader) GetReading(ctx context.Context, instanceID uuid.UUID) (structure.Reading, bool, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+readingSelectFields+` FROM vitals_current_readings WHERE instance_id = $1`, uuidToText(instanceID))
	rd, err := scanReading(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Reading{}, false, nil
		}
		return structure.Reading{}, false, err
	}
	return rd, true, nil
}

func (r *PostgresReader) ListHistory(ctx context.Context, instanceID uuid.UUID, limit int) ([]structure.HistoryEntry, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT history_id, `+readingSelectFields+` FROM vitals_reading_history
		 WHERE instance_id = $1 ORDER BY recorded_at DESC LIMIT $2`,
		uuidToText(instanceID), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("dbstore: list history: %w", err)
	}
	defer rows.Close()

	var entries []structure.HistoryEntry
	for rows.Next() {
		var historyIDText string
		// history_id is scanned first, separately, then the shared
		// reading fields via a small adapter that skips it.
		var h structure.HistoryEntry
		scanner := &historyRowScanner{rows: rows, historyIDText: &historyIDText}
		rd, err := scanReading(scanner)
		if err != nil {
			return nil, err
		}
		if h.HistoryID, err = parseUUID(historyIDText); err != nil {
			return nil, fmt.Errorf("dbstore: parse history_id: %w", err)
		}
		h.Reading = rd
		entries = append(entries, h)
	}
	return entries, rows.Err()
}

// historyRowScanner adapts a pgx.Rows whose first selected column is
// history_id (not part of Reading's own field set) so scanReading can
// still be reused unchanged for the remaining, identical columns.
type historyRowScanner struct {
	rows          pgx.Rows
	historyIDText *string
}

func (s *historyRowScanner) Scan(dest ...any) error {
	full := append([]any{s.historyIDText}, dest...)
	return s.rows.Scan(full...)
}

const groupSelectFields = `group_id, scope_type, scope_id, group_key, title, description, sort_order,
	display_hints, app_metadata, created_at, updated_at, archived_at`

func scanGroup(row rowScanner) (structure.Group, error) {
	var g structure.Group
	var idText string
	var displayHints, appMetadata []byte

	if err := row.Scan(
		&idText, &g.ScopeType, &g.ScopeID, &g.GroupKey, &g.Title, &g.Description, &g.SortOrder,
		&displayHints, &appMetadata, &g.CreatedAt, &g.UpdatedAt, &g.ArchivedAt,
	); err != nil {
		return g, fmt.Errorf("dbstore: scan group: %w", err)
	}
	var err error
	if g.GroupID, err = parseUUID(idText); err != nil {
		return g, fmt.Errorf("dbstore: parse group_id: %w", err)
	}
	g.DisplayHints = displayHints
	g.AppMetadata = appMetadata
	return g, nil
}

func (r *PostgresReader) GetGroup(ctx context.Context, id uuid.UUID) (structure.Group, bool, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+groupSelectFields+` FROM vitals_groups WHERE group_id = $1`, uuidToText(id))
	g, err := scanGroup(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Group{}, false, nil
		}
		return structure.Group{}, false, err
	}
	return g, true, nil
}

func (r *PostgresReader) GetGroupByScopeKey(ctx context.Context, scopeType, scopeID, groupKey string) (structure.Group, bool, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+groupSelectFields+` FROM vitals_groups WHERE scope_type = $1 AND scope_id = $2 AND group_key = $3`,
		scopeType, scopeID, groupKey,
	)
	g, err := scanGroup(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return structure.Group{}, false, nil
		}
		return structure.Group{}, false, err
	}
	return g, true, nil
}

func scanGroupMember(row rowScanner) (structure.GroupMember, error) {
	var m structure.GroupMember
	var groupIDText, memberKind string
	var vitalInstanceIDText, childGroupIDText *string
	var displayHints, appMetadata []byte

	if err := row.Scan(
		&groupIDText, &m.MemberKey, &memberKind, &vitalInstanceIDText, &childGroupIDText,
		&m.Label, &m.SortOrder, &m.Required, &displayHints, &appMetadata,
	); err != nil {
		return m, fmt.Errorf("dbstore: scan group member: %w", err)
	}
	var err error
	if m.GroupID, err = parseUUID(groupIDText); err != nil {
		return m, fmt.Errorf("dbstore: parse group_id: %w", err)
	}
	m.MemberKind = structure.MemberKind(memberKind)
	if m.VitalInstanceID, err = parseNullableUUID(vitalInstanceIDText); err != nil {
		return m, fmt.Errorf("dbstore: parse vital_instance_id: %w", err)
	}
	if m.ChildGroupID, err = parseNullableUUID(childGroupIDText); err != nil {
		return m, fmt.Errorf("dbstore: parse child_group_id: %w", err)
	}
	m.DisplayHints = displayHints
	m.AppMetadata = appMetadata
	return m, nil
}

func (r *PostgresReader) ListGroupMembers(ctx context.Context, groupID uuid.UUID) ([]structure.GroupMember, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT group_id, member_key, member_kind, vital_instance_id, child_group_id, label, sort_order, required,
		        display_hints, app_metadata
		 FROM vitals_group_members WHERE group_id = $1 ORDER BY sort_order, member_key`,
		uuidToText(groupID),
	)
	if err != nil {
		return nil, fmt.Errorf("dbstore: list group members: %w", err)
	}
	defer rows.Close()

	var members []structure.GroupMember
	for rows.Next() {
		m, err := scanGroupMember(rows)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}
