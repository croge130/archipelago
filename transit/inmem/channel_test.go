package inmem

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

func newChannelPairForTest() (a, b *Channel) {
	return newChannelPair(wire.NewChannelID(wire.InitiatorClient), "", nil)
}

func TestChannelSendRecv(t *testing.T) {
	a, b := newChannelPairForTest()
	ctx, cancel := withTimeout(t)
	defer cancel()

	if err := a.Send(ctx, wire.Message{Type: "demo.data", ID: "1"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got, err := b.Recv(ctx)
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if got.ID != "1" {
		t.Fatalf("got.ID = %q, want 1", got.ID)
	}
}

func TestChannelBidirectional(t *testing.T) {
	a, b := newChannelPairForTest()
	ctx, cancel := withTimeout(t)
	defer cancel()

	if err := a.Send(ctx, wire.Message{Type: "demo.data", ID: "a-to-b"}); err != nil {
		t.Fatalf("a.Send: %v", err)
	}
	if err := b.Send(ctx, wire.Message{Type: "demo.data", ID: "b-to-a"}); err != nil {
		t.Fatalf("b.Send: %v", err)
	}
	got, err := b.Recv(ctx)
	if err != nil || got.ID != "a-to-b" {
		t.Fatalf("b.Recv = (%+v, %v), want a-to-b", got, err)
	}
	got, err = a.Recv(ctx)
	if err != nil || got.ID != "b-to-a" {
		t.Fatalf("a.Recv = (%+v, %v), want b-to-a", got, err)
	}
}

func TestChannelRecvDrainsBufferedMessagesBeforeReportingClosed(t *testing.T) {
	a, b := newChannelPairForTest()
	ctx, cancel := withTimeout(t)
	defer cancel()

	if err := a.Send(ctx, wire.Message{Type: "demo.data", ID: "1"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := a.Send(ctx, wire.Message{Type: "demo.data", ID: "2"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	a.Close("done sending")

	// Both buffered messages must still be delivered, in order,
	// before Recv reports the channel closed — closing never drops
	// data already in flight.
	got, err := b.Recv(ctx)
	if err != nil || got.ID != "1" {
		t.Fatalf("b.Recv (1st) = (%+v, %v), want message 1", got, err)
	}
	got, err = b.Recv(ctx)
	if err != nil || got.ID != "2" {
		t.Fatalf("b.Recv (2nd) = (%+v, %v), want message 2", got, err)
	}
	if _, err := b.Recv(ctx); err != transit.ErrChannelClosed {
		t.Fatalf("b.Recv (3rd) = %v, want ErrChannelClosed", err)
	}
}

func TestChannelSendAfterCloseFails(t *testing.T) {
	a, _ := newChannelPairForTest()
	a.Close("done")
	ctx, cancel := withTimeout(t)
	defer cancel()
	if err := a.Send(ctx, wire.Message{Type: "demo.data"}); err != transit.ErrChannelClosed {
		t.Fatalf("Send after Close = %v, want ErrChannelClosed", err)
	}
}

func TestChannelCloseIsIdempotentAndSafeFromBothEnds(t *testing.T) {
	a, b := newChannelPairForTest()
	if err := a.Close("first"); err != nil {
		t.Fatalf("a.Close (1st): %v", err)
	}
	if err := a.Close("second"); err != nil {
		t.Fatalf("a.Close (2nd) should be a safe no-op, got: %v", err)
	}
	if err := b.Close("from the other end"); err != nil {
		t.Fatalf("b.Close: %v", err)
	}
}

func TestChannelSendBlocksOnFullBufferUntilContextCancel(t *testing.T) {
	a, _ := newChannelPairForTest()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Fill the buffer with nobody draining it, then confirm the next
	// Send actually backpressures rather than silently dropping.
	for i := 0; i < 16; i++ {
		if err := a.Send(context.Background(), wire.Message{Type: "demo.data"}); err != nil {
			t.Fatalf("Send %d (filling buffer): %v", i, err)
		}
	}
	err := a.Send(ctx, wire.Message{Type: "demo.data"})
	if err == nil {
		t.Fatal("expected Send to block and then fail via context deadline once the buffer is full")
	}
}

func TestOpenedChannelCarriesItsTypeAndParamsToBothEnds(t *testing.T) {
	a, b := NewPipe()
	ctx, cancel := withTimeout(t)
	defer cancel()

	params := json.RawMessage(`{"task":"export"}`)
	opened, err := a.OpenChannel(ctx, transit.ChannelOpts{Initiator: wire.InitiatorClient, Bidirectional: true, Type: "jobs.progress", Params: params})
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	accepted, err := b.AcceptChannel(ctx)
	if err != nil {
		t.Fatalf("AcceptChannel: %v", err)
	}
	for name, ch := range map[string]transit.Channel{"opener": opened, "acceptor": accepted} {
		if ch.Type() != "jobs.progress" || string(ch.Params()) != string(params) {
			t.Errorf("%s sees type %q params %s, want jobs.progress %s", name, ch.Type(), ch.Params(), params)
		}
	}
	// An untyped channel is still allowed at this layer.
	plain, _ := a.OpenChannel(ctx, transit.ChannelOpts{Initiator: wire.InitiatorClient})
	if plain.Type() != "" || plain.Params() != nil {
		t.Errorf("an untyped channel should have no type or params, got %q %s", plain.Type(), plain.Params())
	}
}

func TestSessionSatisfiesConn(t *testing.T) {
	var _ transit.Conn = (*Session)(nil)
}
