package traceagg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/croge130/archipelago/logging"
	"github.com/google/uuid"
)

// MessageTypeEntries is the wire.Message.Type PushEntries sends and
// Collector.Ingest expects.
const MessageTypeEntries = "traceagg.entries"

// EntryBatch is MessageTypeEntries' payload shape: one reporting
// instance's entries, tagged with which instance they came from so a
// Collector can attribute them after Ingest.
type EntryBatch struct {
	SourceInstanceID uuid.UUID
	Entries          []logging.Entry
}

// Pusher is the one thing PushEntries needs from the peer it reports
// to: send an event. *router.Peer satisfies it, which is how entries
// reach a coordinator whose router is running the handshake and
// authorization in front of Collector.Handler.
type Pusher interface {
	Push(ctx context.Context, typ string, payload json.RawMessage) error
}

// PushEntries ships entries to a peer as an event (reliable, no reply
// expected — exactly the right guarantee for "here's what happened,"
// per 11-transit-model.md's own delivery-class taxonomy). Finding and
// connecting to the peer is the caller's job; this function only owns
// the send.
//
// An event the receiving router cannot take in right now (over its
// in-flight limit) is dropped and counted there, not retried: see
// 21-router-and-handshake-model.md. Trace entries are diagnostic, so
// that loss is acceptable here in a way it would not be for a job result.
func PushEntries(ctx context.Context, peer Pusher, sourceInstanceID uuid.UUID, entries []logging.Entry) error {
	payload, err := json.Marshal(EntryBatch{SourceInstanceID: sourceInstanceID, Entries: entries})
	if err != nil {
		return fmt.Errorf("traceagg: push entries: marshal: %w", err)
	}
	if err := peer.Push(ctx, MessageTypeEntries, payload); err != nil {
		return fmt.Errorf("traceagg: push entries: %w", err)
	}
	return nil
}
