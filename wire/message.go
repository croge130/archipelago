package wire

import (
	"encoding/json"
	"fmt"

	"github.com/croge130/archipelago/logging"
)

// Message is the envelope every Transit caller speaks. id and channel
// are additive and orthogonal: id correlates one response to one
// request and lives for a single exchange; channel scopes ordering
// and flow for a long-lived logical stream and lives for many
// messages. A message that omits both behaves exactly as a
// single-exchange, unordered send would without them — the additive
// property that lets old callers ignore fields they don't use.
//
// TraceID/SpanID/ParentSpanID are the same kind of additive field,
// added later per docs/architecture/16-trace-log-aggregation-model.md:
// hex, W3C Trace Context-shaped, carrying a logging.SpanContext
// across a Transit hop so a receiving handler can continue the same
// trace in its own logging. Nothing sets these automatically — a
// caller that wants correlation calls WithTraceContext before
// sending; a message that omits them behaves exactly as one that
// predates the fields entirely.
type Message struct {
	Type         string          `json:"type"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	ID           string          `json:"id,omitempty"`
	Kind         Kind            `json:"kind,omitempty"`
	Channel      string          `json:"channel,omitempty"`
	TraceID      string          `json:"trace_id,omitempty"`
	SpanID       string          `json:"span_id,omitempty"`
	ParentSpanID string          `json:"parent_span_id,omitempty"`
}

// WithTraceContext stamps m with sc's trace context — the one
// deliberate propagation point; nothing calls this on a caller's
// behalf.
func (m Message) WithTraceContext(sc logging.SpanContext) Message {
	m.TraceID = sc.TraceID.String()
	m.SpanID = sc.SpanID.String()
	if !sc.ParentSpanID.IsZero() {
		m.ParentSpanID = sc.ParentSpanID.String()
	} else {
		m.ParentSpanID = ""
	}
	return m
}

// TraceContext parses m's trace fields back into a logging.SpanContext.
// found is false when TraceID or SpanID is absent — the normal case
// for a message nobody stamped, not an error.
func (m Message) TraceContext() (sc logging.SpanContext, found bool) {
	if m.TraceID == "" || m.SpanID == "" {
		return logging.SpanContext{}, false
	}
	traceID, err := logging.ParseTraceID(m.TraceID)
	if err != nil {
		return logging.SpanContext{}, false
	}
	spanID, err := logging.ParseSpanID(m.SpanID)
	if err != nil {
		return logging.SpanContext{}, false
	}
	sc = logging.SpanContext{TraceID: traceID, SpanID: spanID}
	if m.ParentSpanID != "" {
		parentSpanID, err := logging.ParseSpanID(m.ParentSpanID)
		if err != nil {
			return logging.SpanContext{}, false
		}
		sc.ParentSpanID = parentSpanID
	}
	return sc, true
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
	// A channel's data and end frames are identified by their Channel and
	// carry no Type of their own; every other message needs one.
	channelFrame := m.Kind == KindStreamData || m.Kind == KindStreamEnd
	switch {
	case channelFrame && m.Channel == "":
		return fmt.Errorf("wire: message: a %s frame requires a Channel", m.Kind)
	case m.Kind == KindStreamOpen && m.Channel == "":
		return fmt.Errorf("wire: message: a stream_open frame requires a Channel")
	case m.Type == "" && !channelFrame:
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
	if (m.TraceID == "") != (m.SpanID == "") {
		return fmt.Errorf("wire: message: TraceID and SpanID must both be set or both be empty")
	}
	if m.TraceID != "" {
		if _, err := logging.ParseTraceID(m.TraceID); err != nil {
			return fmt.Errorf("wire: message: invalid TraceID: %w", err)
		}
		if _, err := logging.ParseSpanID(m.SpanID); err != nil {
			return fmt.Errorf("wire: message: invalid SpanID: %w", err)
		}
	}
	if m.ParentSpanID != "" {
		if _, err := logging.ParseSpanID(m.ParentSpanID); err != nil {
			return fmt.Errorf("wire: message: invalid ParentSpanID: %w", err)
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
