package traceagg

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
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

// PushEntries ships entries to the peer session already has open, as
// an EventPush delivery (reliable, no reply expected — exactly the
// right guarantee for "here's what happened," per 11-transit-model.md's
// own delivery-class taxonomy). Finding and opening session is the
// caller's job; this function only owns the send.
func PushEntries(ctx context.Context, session transit.Session, sourceInstanceID uuid.UUID, entries []logging.Entry) error {
	payload, err := json.Marshal(EntryBatch{SourceInstanceID: sourceInstanceID, Entries: entries})
	if err != nil {
		return fmt.Errorf("traceagg: push entries: marshal: %w", err)
	}
	msg := wire.Message{Type: MessageTypeEntries, Kind: wire.KindEvent, Payload: payload}
	if err := session.Push(ctx, msg); err != nil {
		return fmt.Errorf("traceagg: push entries: %w", err)
	}
	return nil
}
