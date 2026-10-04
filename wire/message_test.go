package wire

import (
	"testing"

	"github.com/croge130/archipelago/logging"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	m := Message{Type: "demo.echo", Payload: []byte(`{"text":"hi"}`), ID: "req-1"}
	data, err := Encode(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Type != m.Type || got.ID != m.ID || string(got.Payload) != string(m.Payload) {
		t.Fatalf("got = %+v, want %+v", got, m)
	}
}

func TestEncodeDefaultsKindToRequest(t *testing.T) {
	data, err := Encode(Message{Type: "demo.echo"})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Kind != KindRequest {
		t.Fatalf("got.Kind = %q, want %q (the backward-compatible default)", got.Kind, KindRequest)
	}
}

func TestDecodeOldStyleMessageWithoutNewFields(t *testing.T) {
	// A message that predates id/kind/channel entirely — the
	// additive-envelope property: old data decodes exactly as it
	// would have before those fields existed, picking up only the
	// documented default.
	old := []byte(`{"type":"demo.echo","payload":{"text":"hi"}}`)
	got, err := Decode(old)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Type != "demo.echo" || got.ID != "" || got.Channel != "" || got.Kind != KindRequest {
		t.Fatalf("got = %+v, want an old-style message decoded with defaults only", got)
	}
}

func TestMessageValidateRequiresType(t *testing.T) {
	if err := (Message{}).Validate(); err == nil {
		t.Fatal("expected an error for a missing Type")
	}
}

func TestMessageValidateRejectsUnknownKind(t *testing.T) {
	m := Message{Type: "demo.echo", Kind: Kind("nonsense")}
	if err := m.Validate(); err == nil {
		t.Fatal("expected an error for an unknown Kind")
	}
}

func TestMessageValidateRejectsMalformedChannel(t *testing.T) {
	m := Message{Type: "demo.echo", Channel: "not-a-valid-channel-id"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected an error for a Channel with no valid initiator prefix")
	}
}

func TestMessageValidateAcceptsRealChannelID(t *testing.T) {
	m := Message{Type: "demo.echo", Channel: NewChannelID(InitiatorClient)}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected a real channel id to validate, got: %v", err)
	}
}

func TestWithTraceContextRoundTrips(t *testing.T) {
	sc := logging.NewRootSpan()
	m := Message{Type: "demo.echo"}.WithTraceContext(sc)

	got, found := m.TraceContext()
	if !found {
		t.Fatal("expected TraceContext to find the stamped context")
	}
	if got.TraceID != sc.TraceID || got.SpanID != sc.SpanID {
		t.Fatalf("got = %+v, want %+v", got, sc)
	}
	if !got.ParentSpanID.IsZero() {
		t.Fatalf("expected a root span's ParentSpanID to stay zero, got %s", got.ParentSpanID)
	}
}

func TestWithTraceContextCarriesParentSpan(t *testing.T) {
	root := logging.NewRootSpan()
	child := logging.NewChildSpan(root)
	m := Message{Type: "demo.echo"}.WithTraceContext(child)

	got, found := m.TraceContext()
	if !found {
		t.Fatal("expected TraceContext to find the stamped context")
	}
	if got.ParentSpanID != root.SpanID {
		t.Fatalf("ParentSpanID = %s, want the root span's id %s", got.ParentSpanID, root.SpanID)
	}
}

func TestTraceContextNotFoundOnOrdinaryMessage(t *testing.T) {
	// A message nobody stamped — the normal case, not an error.
	_, found := (Message{Type: "demo.echo"}).TraceContext()
	if found {
		t.Fatal("expected an unstamped message to report found=false")
	}
}

func TestMessageValidateRequiresTraceIDAndSpanIDTogether(t *testing.T) {
	m := Message{Type: "demo.echo", TraceID: logging.NewTraceID().String()}
	if err := m.Validate(); err == nil {
		t.Fatal("expected an error when TraceID is set but SpanID is not")
	}
}

func TestMessageValidateRejectsMalformedTraceID(t *testing.T) {
	m := Message{Type: "demo.echo", TraceID: "not-hex", SpanID: logging.NewSpanID().String()}
	if err := m.Validate(); err == nil {
		t.Fatal("expected an error for a malformed TraceID")
	}
}

func TestMessageValidateAcceptsWellFormedTraceContext(t *testing.T) {
	sc := logging.NewRootSpan()
	m := Message{Type: "demo.echo"}.WithTraceContext(sc)
	if err := m.Validate(); err != nil {
		t.Fatalf("expected a well-formed trace context to validate, got: %v", err)
	}
}

func TestDecodeOldStyleMessageHasNoTraceContext(t *testing.T) {
	// A message that predates TraceID/SpanID/ParentSpanID entirely —
	// the same additive-envelope property TestDecodeOldStyleMessageWithoutNewFields
	// already asserts for id/kind/channel.
	old := []byte(`{"type":"demo.echo","payload":{"text":"hi"}}`)
	got, err := Decode(old)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, found := got.TraceContext(); found {
		t.Fatal("expected an old-style message to carry no trace context")
	}
}
