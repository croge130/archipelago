package inmem

import (
	"context"
	"testing"
	"time"

	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

func withTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 2*time.Second)
}

func TestPushDeliversToPeer(t *testing.T) {
	a, b := NewPipe()
	ctx, cancel := withTimeout(t)
	defer cancel()

	if err := a.Push(ctx, wire.Message{Type: "demo.event"}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	got, err := b.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Type != "demo.event" {
		t.Fatalf("got.Type = %q, want demo.event", got.Type)
	}
}

func TestReplyDeliversToPeer(t *testing.T) {
	a, b := NewPipe()
	ctx, cancel := withTimeout(t)
	defer cancel()

	if err := a.Reply(ctx, wire.Message{Type: "demo.reply", ID: "req-1"}); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	got, err := b.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.ID != "req-1" {
		t.Fatalf("got.ID = %q, want req-1", got.ID)
	}
}

func TestMessagesArriveInOrder(t *testing.T) {
	a, b := NewPipe()
	ctx, cancel := withTimeout(t)
	defer cancel()

	for i := 0; i < 5; i++ {
		if err := a.Push(ctx, wire.Message{Type: "demo.event", ID: string(rune('0' + i))}); err != nil {
			t.Fatalf("Push %d: %v", i, err)
		}
	}
	for i := 0; i < 5; i++ {
		got, err := b.Next(ctx)
		if err != nil {
			t.Fatalf("Next %d: %v", i, err)
		}
		if got.ID != string(rune('0'+i)) {
			t.Fatalf("message %d arrived out of order: got ID %q", i, got.ID)
		}
	}
}

func TestSendAfterPeerCloseFails(t *testing.T) {
	a, b := NewPipe()
	ctx, cancel := withTimeout(t)
	defer cancel()

	b.Close()
	if err := a.Push(ctx, wire.Message{Type: "demo.event"}); err == nil {
		t.Fatal("expected Push to a closed peer to fail")
	}
}

func TestNextUnblocksOnLocalClose(t *testing.T) {
	a, _ := NewPipe()
	done := make(chan error, 1)
	go func() {
		_, err := a.Next(context.Background())
		done <- err
	}()

	a.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected Next to return an error once the session closes")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Next did not unblock within 2s of Close")
	}
}

func TestOpenChannelDeliversToPeerAcceptChannel(t *testing.T) {
	a, b := NewPipe()
	ctx, cancel := withTimeout(t)
	defer cancel()

	opened := make(chan transit.Channel, 1)
	go func() {
		ch, err := b.AcceptChannel(ctx)
		if err != nil {
			t.Errorf("AcceptChannel: %v", err)
			return
		}
		opened <- ch
	}()

	local, err := a.OpenChannel(ctx, transit.ChannelOpts{Initiator: wire.InitiatorClient, Bidirectional: true})
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}

	var remote transit.Channel
	select {
	case remote = <-opened:
	case <-time.After(2 * time.Second):
		t.Fatal("AcceptChannel did not receive the opened channel within 2s")
	}
	if local.ID() != remote.ID() {
		t.Fatalf("local.ID() = %q, remote.ID() = %q, want equal", local.ID(), remote.ID())
	}
}
