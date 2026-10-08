package routere2e

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/transit/websocket"
	"github.com/croge130/archipelago/wire"
)

func ctxT(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func quiet() router.Options {
	return router.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// connect stands up a real websocket listener, dials it, and runs a
// router on each end.
func connect(t *testing.T, server, client *router.Router) (dialer, acceptor *router.Peer) {
	t.Helper()
	backend := websocket.NewBackend()
	httpServer := httptest.NewServer(backend)
	t.Cleanup(func() { httpServer.Close(); backend.Close() })

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")
	clientSession, err := websocket.Dial(ctxT(t), wsURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	serverSession, err := backend.Accept(ctxT(t))
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	acceptor = server.Accept(serverSession.(transit.Conn))
	dialer, err = client.Connect(ctxT(t), clientSession.(transit.Conn), "e2e-client")
	if err != nil {
		t.Fatalf("Connect over a real connection: %v", err)
	}
	t.Cleanup(func() { dialer.Close(); acceptor.Close() })
	return dialer, acceptor
}

func TestHandshakeCallAndTypedChannelOverARealWebsocket(t *testing.T) {
	server := router.New(quiet())
	if err := server.Handle(router.Route{Type: "demo.echo", Handler: func(_ context.Context, req router.Request) (json.RawMessage, error) {
		return req.Message.Payload, nil
	}}); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 1)
	if err := server.HandleChannel(router.ChannelRoute{Type: "demo.stream", Handler: func(ctx context.Context, req router.ChannelRequest) error {
		msg, err := req.Channel.Recv(ctx)
		if err != nil {
			return err
		}
		got <- req.Channel.Type() + ":" + string(req.Channel.Params()) + ":" + string(msg.Payload)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	d, a := connect(t, server, router.New(quiet()))

	if d.Version() != wire.MaxProtocol || a.Version() != wire.MaxProtocol {
		t.Fatalf("negotiated %d / %d over the wire, want %d", d.Version(), a.Version(), wire.MaxProtocol)
	}
	resp, err := d.Call(ctxT(t), "demo.echo", json.RawMessage(`{"over":"the network"}`))
	if err != nil || string(resp) != `{"over":"the network"}` {
		t.Fatalf("Call: %s err=%v", resp, err)
	}
	var re *router.RemoteError
	if _, err := d.Call(ctxT(t), "no.such.route", nil); !errors.As(err, &re) || re.Code != wire.ErrUnknownRoute {
		t.Errorf("an unknown route over the wire: err = %v", err)
	}

	ch, err := d.OpenChannel(ctxT(t), transit.ChannelOpts{Initiator: wire.InitiatorClient, Bidirectional: true, Type: "demo.stream", Params: json.RawMessage(`{"k":1}`)})
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	if err := ch.Send(ctxT(t), wire.Message{Payload: json.RawMessage(`"hi"`)}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case v := <-got:
		if v != `demo.stream:{"k":1}:"hi"` {
			t.Errorf("the channel handler saw %s", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the typed channel never reached its handler over the real connection")
	}
}

func TestCancelCrossesTheNetworkAndLosingTheConnectionFailsCalls(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := router.New(quiet())
	if err := server.Handle(router.Route{Type: "demo.slow", Handler: func(ctx context.Context, _ router.Request) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}}); err != nil {
		t.Fatal(err)
	}
	d, a := connect(t, server, router.New(quiet()))

	callCtx, cancel := context.WithCancel(ctxT(t))
	errc := make(chan error, 1)
	go func() { _, err := d.Call(callCtx, "demo.slow", nil); errc <- err }()
	<-started
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Errorf("the caller's error = %v, want context.Canceled", err)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the cancel frame never reached the remote handler across the network")
	}

	// Now drop the connection from the server side: the dialer's next
	// call must fail, not hang.
	a.Close()
	select {
	case <-d.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the dialer never noticed the connection was lost")
	}
	if _, err := d.Call(ctxT(t), "demo.slow", nil); err == nil {
		t.Error("a call on a lost connection succeeded")
	}
}
