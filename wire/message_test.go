package wire

import "testing"

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
