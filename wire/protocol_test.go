package wire

import (
	"encoding/json"
	"testing"
)

func TestNegotiateVersionPicksTheHighestCommon(t *testing.T) {
	cases := []struct {
		lmin, lmax, pmin, pmax int
		want                   int
		ok                     bool
	}{
		{1, 1, 1, 1, 1, true},
		{1, 3, 2, 5, 3, true},
		{2, 4, 1, 2, 2, true},
		{1, 2, 3, 4, 0, false},
		{3, 4, 1, 2, 0, false},
	}
	for _, c := range cases {
		got, ok := NegotiateVersion(c.lmin, c.lmax, c.pmin, c.pmax)
		if got != c.want || ok != c.ok {
			t.Errorf("Negotiate(%d-%d, %d-%d) = %d,%v; want %d,%v", c.lmin, c.lmax, c.pmin, c.pmax, got, ok, c.want, c.ok)
		}
	}
}

func TestHelloValidate(t *testing.T) {
	if err := (Hello{MinVersion: 1, MaxVersion: 2}).Validate(); err != nil {
		t.Errorf("a valid range: %v", err)
	}
	for _, h := range []Hello{{}, {MinVersion: 0, MaxVersion: 1}, {MinVersion: 3, MaxVersion: 2}} {
		if err := h.Validate(); err == nil {
			t.Errorf("%+v should be rejected", h)
		}
	}
}

func TestProtocolConstantsAreConsistent(t *testing.T) {
	if MinProtocol < 1 || MaxProtocol < MinProtocol {
		t.Fatalf("invalid protocol range [%d, %d]", MinProtocol, MaxProtocol)
	}
	if _, ok := NegotiateVersion(MinProtocol, MaxProtocol, MinProtocol, MaxProtocol); !ok {
		t.Fatal("this code must be able to speak to itself")
	}
}

func TestErrorReplyCorrelatesAndRoundTrips(t *testing.T) {
	req := Message{Type: "endpoints.list", ID: "req-1", Kind: KindRequest}
	reply := ErrorReply(req, ErrUnauthorized, "no")
	if reply.Kind != KindError || reply.ID != "req-1" || reply.Type != "endpoints.list" {
		t.Fatalf("the error frame must echo the request's type and id: %+v", reply)
	}
	if err := reply.Validate(); err != nil {
		t.Errorf("an error reply should be a valid message: %v", err)
	}
	got := DecodeError(reply)
	if got.Code != ErrUnauthorized || got.Message != "no" {
		t.Errorf("round trip: %+v", got)
	}
}

func TestDecodeErrorNeverLosesAnUnknownCode(t *testing.T) {
	newer := Message{Kind: KindError, Payload: json.RawMessage(`{"code":"quota_exceeded","message":"x"}`)}
	got := DecodeError(newer)
	if got.Code != ErrInternal || got.Message == "" {
		t.Errorf("an unknown code from a newer peer must surface as internal with its text, got %+v", got)
	}
	if got := DecodeError(Message{Kind: KindError}); got.Code != ErrInternal {
		t.Errorf("a missing payload should be internal, got %+v", got)
	}
}

func TestErrorCodes(t *testing.T) {
	for _, c := range []ErrorCode{ErrUnknownRoute, ErrHelloRequired, ErrVersionUnsupported, ErrUnauthorized, ErrInvalid, ErrBusy, ErrCancelled, ErrInternal} {
		if !c.Valid() {
			t.Errorf("%s should be valid", c)
		}
	}
	if ErrorCode("nope").Valid() {
		t.Error("an unknown code should be invalid")
	}
}

func TestValidateChannelFramesNeedAChannelNotAType(t *testing.T) {
	ch := NewChannelID(InitiatorClient)
	if err := (Message{Kind: KindStreamData, Channel: ch}).Validate(); err != nil {
		t.Errorf("stream_data identified by its channel should validate: %v", err)
	}
	if err := (Message{Kind: KindStreamEnd, Channel: ch}).Validate(); err != nil {
		t.Errorf("stream_end identified by its channel should validate: %v", err)
	}
	if err := (Message{Kind: KindStreamData}).Validate(); err == nil {
		t.Error("stream_data with no channel should be rejected")
	}
	if err := (Message{Kind: KindStreamOpen}).Validate(); err == nil {
		t.Error("stream_open with no channel should be rejected")
	}
	// stream_open may carry a Type (what the channel is for) or not.
	if err := (Message{Kind: KindStreamOpen, Channel: ch, Type: "console.relay"}).Validate(); err != nil {
		t.Errorf("a typed stream_open: %v", err)
	}
	// Every other message still needs a Type.
	if err := (Message{Kind: KindRequest}).Validate(); err == nil {
		t.Error("a request with no Type should be rejected")
	}
}
