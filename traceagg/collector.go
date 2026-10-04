package traceagg

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/wire"
	"github.com/google/uuid"
)

// TaggedEntry is one collected Entry, attributed to the instance that
// reported it.
type TaggedEntry struct {
	logging.Entry
	SourceInstanceID uuid.UUID
}

// Collector is an in-memory, trace-ID-indexed store of ingested
// entries — bounded by nothing but process lifetime, per
// 16-trace-log-aggregation-model.md's "not a database" stance. A
// coordinator restart loses what it collected; a caller that wants
// durability already has the real sink each reporting instance's
// logger writes to, independent of this type.
type Collector struct {
	mu      sync.Mutex
	byTrace map[string][]TaggedEntry
}

func NewCollector() *Collector {
	return &Collector{byTrace: make(map[string][]TaggedEntry)}
}

// Ingest processes an already-received wire.Message of type
// MessageTypeEntries — see the package doc for why this takes a
// message rather than receiving one itself. Entries with no TraceID
// are dropped, not stored under an empty-string bucket: "which causal
// story does this belong to" is the one thing Trace exists to answer,
// and an untraced entry has no answer to give.
func (c *Collector) Ingest(msg wire.Message) error {
	if msg.Type != MessageTypeEntries {
		return fmt.Errorf("traceagg: ingest: unexpected message type %q, want %q", msg.Type, MessageTypeEntries)
	}
	var batch EntryBatch
	if err := json.Unmarshal(msg.Payload, &batch); err != nil {
		return fmt.Errorf("traceagg: ingest: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range batch.Entries {
		if e.TraceID == "" {
			continue
		}
		c.byTrace[e.TraceID] = append(c.byTrace[e.TraceID], TaggedEntry{Entry: e, SourceInstanceID: batch.SourceInstanceID})
	}
	return nil
}

// Trace returns every entry collected for traceID, across every
// instance that reported one, ordered by each entry's own recorded
// Time — the best available ordering signal without building real
// clock-skew compensation, which nothing here needs yet.
func (c *Collector) Trace(traceID string) []TaggedEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]TaggedEntry, len(c.byTrace[traceID]))
	copy(out, c.byTrace[traceID])
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}
