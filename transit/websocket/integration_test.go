package websocket

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

func withTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// dialPair starts a real HTTP test server fronting a Backend and
// dials it as a client, returning the server-side Session (as seen
// by Backend.Accept) and the client-side Session (as seen by Dial) —
// a genuine two-process-equivalent round trip, not a mock.
func dialPair(t *testing.T) (server, client *Conn, cleanup func()) {
	t.Helper()
	backend := NewBackend()
	httpServer := httptest.NewServer(backend)

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	ctx, cancel := withTimeout(t)
	defer cancel()

	clientSession, err := Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	serverSession, err := backend.Accept(ctx)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}

	return serverSession.(*Conn), clientSession.(*Conn), func() {
		clientSession.(*Conn).Close()
		serverSession.(*Conn).Close()
		backend.Close()
		httpServer.Close()
	}
}

func TestPushDeliversAcrossRealConnection(t *testing.T) {
	server, client, cleanup := dialPair(t)
	defer cleanup()
	ctx, cancel := withTimeout(t)
	defer cancel()

	if err := server.Push(ctx, wire.Message{Type: "demo.event", Payload: []byte(`{"n":1}`)}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	got, err := client.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.Type != "demo.event" || string(got.Payload) != `{"n":1}` {
		t.Fatalf("got = %+v, want demo.event with payload {\"n\":1}", got)
	}
}

func TestReplyDeliversAcrossRealConnection(t *testing.T) {
	server, client, cleanup := dialPair(t)
	defer cleanup()
	ctx, cancel := withTimeout(t)
	defer cancel()

	if err := client.Reply(ctx, wire.Message{Type: "demo.reply", ID: "req-7"}); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	got, err := server.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if got.ID != "req-7" {
		t.Fatalf("got.ID = %q, want req-7", got.ID)
	}
}

func TestMessagesArriveInOrderAcrossRealConnection(t *testing.T) {
	server, client, cleanup := dialPair(t)
	defer cleanup()
	ctx, cancel := withTimeout(t)
	defer cancel()

	for i := 0; i < 20; i++ {
		if err := server.Push(ctx, wire.Message{Type: "demo.event", ID: string(rune('a' + i))}); err != nil {
			t.Fatalf("Push %d: %v", i, err)
		}
	}
	for i := 0; i < 20; i++ {
		got, err := client.Next(ctx)
		if err != nil {
			t.Fatalf("Next %d: %v", i, err)
		}
		if got.ID != string(rune('a'+i)) {
			t.Fatalf("message %d arrived out of order: got ID %q", i, got.ID)
		}
	}
}

func TestChannelRoundTripAcrossRealConnection(t *testing.T) {
	server, client, cleanup := dialPair(t)
	defer cleanup()
	ctx, cancel := withTimeout(t)
	defer cancel()

	opened := make(chan transit.Channel, 1)
	go func() {
		ch, err := client.AcceptChannel(ctx)
		if err != nil {
			t.Errorf("AcceptChannel: %v", err)
			return
		}
		opened <- ch
	}()

	serverCh, err := server.OpenChannel(ctx, transit.ChannelOpts{Initiator: wire.InitiatorServer, Bidirectional: true})
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}

	var clientCh transit.Channel
	select {
	case clientCh = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptChannel did not receive the opened channel within 5s")
	}
	if serverCh.ID() != clientCh.ID() {
		t.Fatalf("serverCh.ID() = %q, clientCh.ID() = %q, want equal", serverCh.ID(), clientCh.ID())
	}

	if err := serverCh.Send(ctx, wire.Message{Type: "demo.data", ID: "chunk-1"}); err != nil {
		t.Fatalf("serverCh.Send: %v", err)
	}
	got, err := clientCh.Recv(ctx)
	if err != nil || got.ID != "chunk-1" {
		t.Fatalf("clientCh.Recv = (%+v, %v), want chunk-1", got, err)
	}

	if err := clientCh.Send(ctx, wire.Message{Type: "demo.data", ID: "chunk-2"}); err != nil {
		t.Fatalf("clientCh.Send: %v", err)
	}
	got, err = serverCh.Recv(ctx)
	if err != nil || got.ID != "chunk-2" {
		t.Fatalf("serverCh.Recv = (%+v, %v), want chunk-2", got, err)
	}
}

func TestChannelCloseDeliversStreamEndAcrossRealConnection(t *testing.T) {
	server, client, cleanup := dialPair(t)
	defer cleanup()
	ctx, cancel := withTimeout(t)
	defer cancel()

	opened := make(chan transit.Channel, 1)
	go func() {
		ch, err := client.AcceptChannel(ctx)
		if err != nil {
			t.Errorf("AcceptChannel: %v", err)
			return
		}
		opened <- ch
	}()

	serverCh, err := server.OpenChannel(ctx, transit.ChannelOpts{Initiator: wire.InitiatorServer})
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	var clientCh transit.Channel
	select {
	case clientCh = <-opened:
	case <-time.After(5 * time.Second):
		t.Fatal("AcceptChannel timed out")
	}

	if err := serverCh.Send(ctx, wire.Message{Type: "demo.data", ID: "last"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := serverCh.Close("done"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// The already-sent message must still arrive before Recv reports
	// closed — closing never drops data already in flight.
	got, err := clientCh.Recv(ctx)
	if err != nil || got.ID != "last" {
		t.Fatalf("clientCh.Recv (buffered) = (%+v, %v), want last", got, err)
	}
	if _, err := clientCh.Recv(ctx); err != transit.ErrChannelClosed {
		t.Fatalf("clientCh.Recv (after peer close) = %v, want ErrChannelClosed", err)
	}
}

func TestPeerIdentityAbsentWithoutClientCert(t *testing.T) {
	server, client, cleanup := dialPair(t)
	defer cleanup()

	if server.PeerIdentity().Present {
		t.Fatal("expected no client certificate over plain HTTP, so PeerIdentity().Present should be false")
	}
	if client.PeerIdentity().Present {
		t.Fatal("expected the client's own view of the server's identity to also be absent over plain HTTP")
	}
}

func TestConcurrentPushIsRaceFree(t *testing.T) {
	server, client, cleanup := dialPair(t)
	defer cleanup()
	ctx, cancel := withTimeout(t)
	defer cancel()

	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := server.Push(ctx, wire.Message{Type: "demo.event", ID: "x"}); err != nil {
				t.Errorf("Push: %v", err)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if _, err := client.Next(ctx); err != nil {
			t.Fatalf("Next %d: %v", i, err)
		}
	}
}

func TestChannelTypeAndParamsCrossARealConnection(t *testing.T) {
	server, client, cleanup := dialPair(t)
	defer cleanup()
	ctx, cancel := withTimeout(t)
	defer cancel()

	accepted := make(chan transit.Channel, 1)
	go func() {
		ch, err := client.AcceptChannel(ctx)
		if err != nil {
			t.Errorf("AcceptChannel: %v", err)
			return
		}
		accepted <- ch
	}()

	params := json.RawMessage(`{"task":"export"}`)
	opened, err := server.OpenChannel(ctx, transit.ChannelOpts{Initiator: wire.InitiatorServer, Bidirectional: true, Type: "console.relay", Params: params})
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	ch := <-accepted
	if opened.Type() != "console.relay" || ch.Type() != "console.relay" {
		t.Errorf("type: opener %q, acceptor %q, want console.relay on both", opened.Type(), ch.Type())
	}
	if string(ch.Params()) != string(params) {
		t.Errorf("the acceptor's params = %s, want %s", ch.Params(), params)
	}
}
