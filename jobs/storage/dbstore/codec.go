package dbstore

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/croge130/archipelago/jobs/structure"
	"github.com/croge130/archipelago/logging"
)

func durationMS(d time.Duration) int64  { return d.Milliseconds() }
func msDuration(ms int64) time.Duration { return time.Duration(ms) * time.Millisecond }

// spanContextToText and spanContextFromText mirror vitals' helpers: a
// job with no trace context is the common case, so all three columns
// are NULL together.
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

// jsonOrEmptyList and friends keep nil slices from being stored as the
// JSON literal null, so a definition reads back with [] and not nil.
func marshalList[T any](items []T) ([]byte, error) {
	if items == nil {
		items = []T{}
	}
	return json.Marshal(items)
}

func priorityRank(p structure.Priority) int16 { return int16(p.Rank()) }

// qualifiedJobFields prefixes every column in jobFields with alias, for
// a RETURNING clause in an UPDATE ... FROM where unqualified names would
// be ambiguous with the joined CTE.
func qualifiedJobFields(alias string) string {
	cols := strings.Split(jobFields, ",")
	for i, c := range cols {
		cols[i] = alias + "." + strings.TrimSpace(c)
	}
	return strings.Join(cols, ", ")
}

func sortByUrgency(jobs []structure.Job) {
	sort.SliceStable(jobs, func(a, b int) bool {
		ra, rb := jobs[a].Priority.Rank(), jobs[b].Priority.Rank()
		if ra != rb {
			return ra > rb
		}
		return jobs[a].RunAt.Before(jobs[b].RunAt)
	})
}
