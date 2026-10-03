package dbstore

import (
	"encoding/json"

	"github.com/google/uuid"
)

// Helpers for the UUID<->text and nullable-pointer plumbing every scan
// and exec call needs — same discipline as every other module's own
// scan.go (explicit round-tripping through strings, not a specific pgx
// UUID codec).

func uuidToText(id uuid.UUID) string {
	return id.String()
}

func nullableUUIDToText(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func parseUUID(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}

func parseNullableUUID(s *string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func nullableJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("{}")
	}
	return raw
}
