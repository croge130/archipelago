package wire

import (
	"encoding/json"
	"fmt"
)

// Message is the envelope every Transit caller speaks. id and channel
// are additive and orthogonal: id correlates one response to one
// request and lives for a single exchange; channel scopes ordering
// and flow for a long-lived logical stream and lives for many
// messages. A message that omits both behaves exactly as a
// single-exchange, unordered send would without them — the additive
// property that lets old callers ignore fields they don't use.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	ID      string          `json:"id,omitempty"`
	Kind    Kind            `json:"kind,omitempty"`
	Channel string          `json:"channel,omitempty"`
}

// Normalize defaults an omitted Kind to request — the documented
// backward-compatible default for a message that predates the kind
// field entirely.
func (m Message) Normalize() Message {
	if m.Kind == "" {
		m.Kind = KindRequest
	}
	return m
}

func (m Message) Validate() error {
	if m.Type == "" {
		return fmt.Errorf("wire: message: Type is required")
	}
	if m.Kind != "" && !m.Kind.Valid() {
		return fmt.Errorf("wire: message: invalid Kind %q", m.Kind)
	}
	if m.Channel != "" {
		if err := validateChannelID(m.Channel); err != nil {
			return fmt.Errorf("wire: message: %w", err)
		}
	}
	return nil
}

// Encode marshals m after normalizing it, so an encoded message
// always carries an explicit Kind even if the caller left it unset.
func Encode(m Message) ([]byte, error) {
	data, err := json.Marshal(m.Normalize())
	if err != nil {
		return nil, fmt.Errorf("wire: encode: %w", err)
	}
	return data, nil
}

// Decode parses and normalizes a message. It does not call Validate
// — a caller that needs to reject a malformed Type/Kind combination
// does so explicitly, the same "parsing and validating are separate
// steps" discipline used throughout this design.
func Decode(data []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return Message{}, fmt.Errorf("wire: decode: %w", err)
	}
	return m.Normalize(), nil
}
