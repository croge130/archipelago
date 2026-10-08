package router

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/transit/inmem"
	"github.com/croge130/archipelago/wire"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func ctxT(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

// pair connects two routers over an in-memory pipe: the first dials, the
// second accepts.
func pair(t *testing.T, dialer, acceptor *Router) (d, a *Peer) {
	t.Helper()
	dc, ac := inmem.NewPipe()
	a = acceptor.Accept(ac)
	var err error
	d, err = dialer.Connect(ctxT(t), dc, "dialer-1")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { d.Close(); a.Close() })
	return d, a
}

func newRouter(opts Options) *Router {
	opts.Logger = quiet()
	return New(opts)
}

func echo(_ context.Context, req Request) (json.RawMessage, error) { return req.Message.Payload, nil }

func mustHandle(t *testing.T, r *Router, route Route) {
	t.Helper()
	if err := r.Handle(route); err != nil {
		t.Fatalf("Handle(%s): %v", route.Type, err)
	}
}

func TestHandshakeNegotiatesAndCallsRoundTrip(t *testing.T) {
	server := newRouter(Options{})
	mustHandle(t, server, Route{Type: "demo.echo", Handler: echo})
	d, a := pair(t, newRouter(Options{}), server)

	if d.Version() != 1 || a.Version() != 1 {
		t.Fatalf("negotiated versions: dialer %d, acceptor %d, want 1", d.Version(), a.Version())
	}
	got, err := d.Call(ctxT(t), "demo.echo", json.RawMessage(`{"n":7}`))
	if err != nil || string(got) != `{"n":7}` {
		t.Fatalf("Call: %s err=%v", got, err)
	}
}

func TestNothingIsReachableBeforeTheHandshake(t *testing.T) {
	server := newRouter(Options{})
	mustHandle(t, server, Route{Type: "demo.echo", Handler: echo})
	dc, ac := inmem.NewPipe()
	p := server.Accept(ac)
	defer p.Close()

	// A raw dialer that skips the handshake.
	if err := dc.Push(ctxT(t), wire.Message{Type: "demo.echo", ID: "r1", Kind: wire.KindRequest}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	resp, err := dc.Next(ctxT(t))
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if resp.Kind != wire.KindError || wire.DecodeError(resp).Code != wire.ErrHelloRequired {
		t.Fatalf("a request before the handshake: %+v, want hello_required", resp)
	}
	// And a peer's own Call is gated until its handshake is done.
	if _, err := p.Call(ctxT(t), "demo.echo", nil); !errors.Is(err, ErrHandshakeIncomplete) {
		t.Errorf("Call before the handshake: err = %v, want ErrHandshakeIncomplete", err)
	}
}

func TestVersionMismatchIsReportedAtTheStart(t *testing.T) {
	server := newRouter(Options{MinProtocol: 5, MaxProtocol: 6})
	dc, ac := inmem.NewPipe()
	a := server.Accept(ac)
	defer a.Close()
	_, err := newRouter(Options{}).Connect(ctxT(t), dc, "")
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != wire.ErrVersionUnsupported {
		t.Fatalf("Connect to an incompatible peer: err = %v, want a version_unsupported RemoteError", err)
	}
}

func TestAcceptedSessionThatNeverSaysHelloIsClosed(t *testing.T) {
	server := newRouter(Options{HelloTimeout: 50 * time.Millisecond})
	_, ac := inmem.NewPipe()
	p := server.Accept(ac)
	select {
	case <-p.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a session that never completed the handshake was not closed")
	}
}

func TestUnknownRouteAndUnknownEvent(t *testing.T) {
	d, a := pair(t, newRouter(Options{}), newRouter(Options{}))
	_, err := d.Call(ctxT(t), "no.such.route", nil)
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != wire.ErrUnknownRoute {
		t.Fatalf("unknown route: err = %v, want unknown_route", err)
	}
	// An unknown event is ignored; the session stays healthy.
	if err := d.Push(ctxT(t), "no.such.event", nil); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if _, err := d.Call(ctxT(t), "no.such.route", nil); err == nil {
		t.Fatal("expected the still-healthy session to answer with an error")
	}
	_ = a
}

func TestHandlerErrorsAreCodedAndInternalsNeverLeak(t *testing.T) {
	server := newRouter(Options{})
	mustHandle(t, server, Route{Type: "demo.invalid", Handler: func(context.Context, Request) (json.RawMessage, error) {
		return nil, Errorf(wire.ErrInvalid, "report_id is required")
	}})
	mustHandle(t, server, Route{Type: "demo.boom", Handler: func(context.Context, Request) (json.RawMessage, error) {
		return nil, errors.New("pq: password authentication failed for user archipelago")
	}})
	mustHandle(t, server, Route{Type: "demo.panic", Handler: func(context.Context, Request) (json.RawMessage, error) {
		panic("nil map")
	}})
	mustHandle(t, server, Route{Type: "demo.echo", Handler: echo})
	d, _ := pair(t, newRouter(Options{}), server)

	var re *RemoteError
	if _, err := d.Call(ctxT(t), "demo.invalid", nil); !errors.As(err, &re) || re.Code != wire.ErrInvalid || re.Message != "report_id is required" {
		t.Errorf("a coded handler error: %v", err)
	}
	if _, err := d.Call(ctxT(t), "demo.boom", nil); !errors.As(err, &re) || re.Code != wire.ErrInternal || re.Message != "internal error" {
		t.Errorf("an uncoded error must be a generic internal error, got %v", err)
	}
	if _, err := d.Call(ctxT(t), "demo.panic", nil); !errors.As(err, &re) || re.Code != wire.ErrInternal || re.Message != "internal error" {
		t.Errorf("a panic must be a generic internal error, got %v", err)
	}
	// The session survives a panicking handler.
	if got, err := d.Call(ctxT(t), "demo.echo", json.RawMessage(`1`)); err != nil || string(got) != "1" {
		t.Errorf("after a panic: %s err=%v", got, err)
	}
}

func TestAuthorizerRefusalNeverRunsTheHandler(t *testing.T) {
	var ran atomic.Bool
	server := newRouter(Options{})
	mustHandle(t, server, Route{
		Type: "demo.secret",
		Handler: func(context.Context, Request) (json.RawMessage, error) {
			ran.Store(true)
			return nil, nil
		},
		Authorize: func(context.Context, transit.Session, wire.Message) error { return errors.New("peer lacks the grant") },
	})
	mustHandle(t, server, Route{Type: "demo.open", Handler: echo, Authorize: func(context.Context, transit.Session, wire.Message) error { return nil }})
	d, _ := pair(t, newRouter(Options{}), server)

	_, err := d.Call(ctxT(t), "demo.secret", nil)
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != wire.ErrUnauthorized || re.Message != "not authorized" {
		t.Fatalf("err = %v, want unauthorized with a generic message (the reason must not reach the peer)", err)
	}
	if ran.Load() {
		t.Error("the handler ran despite the refusal")
	}
	if _, err := d.Call(ctxT(t), "demo.open", json.RawMessage(`1`)); err != nil {
		t.Errorf("an authorized route: %v", err)
	}
}

func TestRoutesAreSequentialByDefaultAndOptInConcurrent(t *testing.T) {
	run := func(concurrency int) (maxSeen int32) {
		var cur, max atomic.Int32
		release := make(chan struct{})
		server := newRouter(Options{})
		mustHandle(t, server, Route{Type: "demo.slow", Concurrency: concurrency, Handler: func(ctx context.Context, _ Request) (json.RawMessage, error) {
			n := cur.Add(1)
			for {
				m := max.Load()
				if n <= m || max.CompareAndSwap(m, n) {
					break
				}
			}
			select {
			case <-release:
			case <-ctx.Done():
			}
			cur.Add(-1)
			return nil, nil
		}})
		d, _ := pair(t, newRouter(Options{}), server)
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _, _ = d.Call(ctxT(t), "demo.slow", nil) }()
		}
		time.Sleep(150 * time.Millisecond)
		close(release)
		wg.Wait()
		return max.Load()
	}
	if got := run(0); got != 1 {
		t.Errorf("the default must be sequential: %d handlers overlapped", got)
	}
	if got := run(3); got < 2 {
		t.Errorf("Concurrency 3 should let handlers overlap, saw at most %d", got)
	}
}

func TestASlowRouteDoesNotBlockOtherRoutes(t *testing.T) {
	release := make(chan struct{})
	server := newRouter(Options{})
	mustHandle(t, server, Route{Type: "demo.slow", Handler: func(ctx context.Context, _ Request) (json.RawMessage, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	}})
	mustHandle(t, server, Route{Type: "demo.fast", Handler: echo})
	d, _ := pair(t, newRouter(Options{}), server)

	go func() { _, _ = d.Call(ctxT(t), "demo.slow", nil) }()
	time.Sleep(50 * time.Millisecond)
	if got, err := d.Call(ctxT(t), "demo.fast", json.RawMessage(`1`)); err != nil || string(got) != "1" {
		t.Fatalf("a fast route behind a stuck one: %s err=%v", got, err)
	}
	close(release)
}

func TestEventsOnARouteArriveInOrder(t *testing.T) {
	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	server := newRouter(Options{})
	mustHandle(t, server, Route{Type: "demo.event", Handler: func(_ context.Context, req Request) (json.RawMessage, error) {
		mu.Lock()
		got = append(got, string(req.Message.Payload))
		n := len(got)
		mu.Unlock()
		if n == 20 {
			close(done)
		}
		return nil, nil
	}})
	d, _ := pair(t, newRouter(Options{}), server)
	for i := 0; i < 20; i++ {
		if err := d.Push(ctxT(t), "demo.event", json.RawMessage(`"`+string(rune('a'+i))+`"`)); err != nil {
			t.Fatalf("Push: %v", err)
		}
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("events were not all handled")
	}
	mu.Lock()
	defer mu.Unlock()
	for i, v := range got {
		if v != `"`+string(rune('a'+i))+`"` {
			t.Fatalf("events handled out of order: %v", got)
		}
	}
}

func TestInFlightLimitAnswersBusyAndRecovers(t *testing.T) {
	release := make(chan struct{})
	server := newRouter(Options{MaxInFlight: 2})
	mustHandle(t, server, Route{Type: "demo.slow", Concurrency: 2, Handler: func(ctx context.Context, _ Request) (json.RawMessage, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	}})
	d, a := pair(t, newRouter(Options{}), server)

	for i := 0; i < 2; i++ {
		go func() { _, _ = d.Call(ctxT(t), "demo.slow", nil) }()
	}
	deadline := time.Now().Add(2 * time.Second)
	for a.InFlight() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	_, err := d.Call(ctxT(t), "demo.slow", nil)
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != wire.ErrBusy {
		t.Fatalf("over the limit: err = %v, want busy", err)
	}
	close(release)
	deadline = time.Now().Add(2 * time.Second)
	for a.InFlight() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := d.Call(ctxT(t), "demo.slow", nil); err != nil {
		t.Errorf("after the in-flight work finished: %v", err)
	}
}

func TestEventsOverTheLimitAreDroppedAndCounted(t *testing.T) {
	release := make(chan struct{})
	server := newRouter(Options{MaxInFlight: 1})
	mustHandle(t, server, Route{Type: "demo.event", Handler: func(ctx context.Context, _ Request) (json.RawMessage, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, nil
	}})
	d, a := pair(t, newRouter(Options{}), server)
	_ = d.Push(ctxT(t), "demo.event", nil)
	deadline := time.Now().Add(2 * time.Second)
	for a.InFlight() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	_ = d.Push(ctxT(t), "demo.event", nil)
	deadline = time.Now().Add(2 * time.Second)
	for a.DroppedEvents() < 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	if a.DroppedEvents() != 1 {
		t.Errorf("DroppedEvents = %d, want 1: an event over the limit has no reply path, so it must at least be counted", a.DroppedEvents())
	}
}

func TestCancellingACallCancelsTheRemoteHandler(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := newRouter(Options{})
	mustHandle(t, server, Route{Type: "demo.slow", Handler: func(ctx context.Context, _ Request) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}})
	d, _ := pair(t, newRouter(Options{}), server)

	callCtx, cancel := context.WithCancel(ctxT(t))
	errc := make(chan error, 1)
	go func() { _, err := d.Call(callCtx, "demo.slow", nil); errc <- err }()
	<-started
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the caller's error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call did not return after its context was cancelled")
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the remote handler's context was never cancelled")
	}
}

func TestEndingTheSessionCancelsHandlersAndFailsPendingCalls(t *testing.T) {
	started := make(chan struct{})
	handlerCancelled := make(chan struct{})
	server := newRouter(Options{})
	mustHandle(t, server, Route{Type: "demo.slow", Handler: func(ctx context.Context, _ Request) (json.RawMessage, error) {
		close(started)
		<-ctx.Done()
		close(handlerCancelled)
		return nil, ctx.Err()
	}})
	d, a := pair(t, newRouter(Options{}), server)

	errc := make(chan error, 1)
	go func() { _, err := d.Call(ctxT(t), "demo.slow", nil); errc <- err }()
	<-started
	a.Close()
	select {
	case <-handlerCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("ending the session did not cancel the handler")
	}
	d.Close()
	select {
	case err := <-errc:
		if err == nil {
			t.Error("a pending call on a closed session returned success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a pending call was not failed when the session ended")
	}
	if _, err := d.Call(ctxT(t), "demo.slow", nil); !errors.Is(err, transit.ErrSessionClosed) {
		t.Errorf("a call on a closed session: err = %v, want ErrSessionClosed", err)
	}
}

func TestTraceContextContinuesIntoTheHandler(t *testing.T) {
	var got logging.SpanContext
	var ok bool
	server := newRouter(Options{})
	mustHandle(t, server, Route{Type: "demo.trace", Handler: func(ctx context.Context, _ Request) (json.RawMessage, error) {
		got, ok = logging.SpanFromContext(ctx)
		return nil, nil
	}})
	d, _ := pair(t, newRouter(Options{}), server)

	root := logging.NewRootSpan()
	if _, err := d.Call(logging.ContextWithSpan(ctxT(t), root), "demo.trace", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !ok || got.TraceID != root.TraceID {
		t.Fatalf("the handler is not in the caller's trace: got %+v ok=%v, want trace %s", got, ok, root.TraceID)
	}
	if got.SpanID == root.SpanID {
		t.Error("the handler should run in its own span, not reuse the caller's")
	}
}

func TestEitherEndCanCallTheOther(t *testing.T) {
	client := newRouter(Options{})
	mustHandle(t, client, Route{Type: "demo.callback", Handler: func(_ context.Context, req Request) (json.RawMessage, error) {
		return json.RawMessage(`"client says hi"`), nil
	}})
	server := newRouter(Options{})
	mustHandle(t, server, Route{Type: "demo.ask", Handler: func(ctx context.Context, req Request) (json.RawMessage, error) {
		// The handler calls back to the sender on the same session.
		return req.Peer.Call(ctx, "demo.callback", nil)
	}})
	d, _ := pair(t, client, server)

	got, err := d.Call(ctxT(t), "demo.ask", nil)
	if err != nil || string(got) != `"client says hi"` {
		t.Fatalf("a callback through the server: %s err=%v", got, err)
	}
}

func TestRegistrationRules(t *testing.T) {
	r := newRouter(Options{})
	mustHandle(t, r, Route{Type: "demo.a", Handler: echo})
	for name, route := range map[string]Route{
		"duplicate":            {Type: "demo.a", Handler: echo},
		"empty type":           {Handler: echo},
		"reserved namespace":   {Type: "transit.hello", Handler: echo},
		"no handler":           {Type: "demo.b"},
		"versioned":            {Type: "demo.c", Version: 2, Handler: echo},
		"negative concurrency": {Type: "demo.d", Handler: echo, Concurrency: -1},
	} {
		if err := r.Handle(route); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
	if err := r.HandleChannel(ChannelRoute{Type: "transit.x", Handler: func(context.Context, ChannelRequest) error { return nil }}); err == nil {
		t.Error("a channel route in the reserved namespace was accepted")
	}
}

func TestTypedChannelsAreDispatchedByType(t *testing.T) {
	got := make(chan string, 1)
	server := newRouter(Options{})
	if err := server.HandleChannel(ChannelRoute{Type: "demo.stream", Handler: func(ctx context.Context, req ChannelRequest) error {
		msg, err := req.Channel.Recv(ctx)
		if err != nil {
			return err
		}
		got <- req.Channel.Type() + ":" + string(req.Channel.Params()) + ":" + string(msg.Payload)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	d, _ := pair(t, newRouter(Options{}), server)

	ch, err := d.OpenChannel(ctxT(t), transit.ChannelOpts{Initiator: wire.InitiatorClient, Bidirectional: true, Type: "demo.stream", Params: json.RawMessage(`{"k":1}`)})
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	if err := ch.Send(ctxT(t), wire.Message{Payload: json.RawMessage(`"hello"`)}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case v := <-got:
		if v != `demo.stream:{"k":1}:"hello"` {
			t.Errorf("handler saw %s", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the channel handler never ran")
	}
}

func TestChannelWithNoRouteOrBeforeHandshakeIsClosed(t *testing.T) {
	server := newRouter(Options{})
	d, _ := pair(t, newRouter(Options{}), server)
	ch, err := d.OpenChannel(ctxT(t), transit.ChannelOpts{Initiator: wire.InitiatorClient, Type: "no.such.stream"})
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	// Channel.Done fires on a local close; the opener learns the peer
	// closed through Recv reporting a closed channel.
	if _, err := ch.Recv(ctxT(t)); !errors.Is(err, transit.ErrChannelClosed) {
		t.Fatalf("a channel with no route: Recv err = %v, want ErrChannelClosed", err)
	}

	// A channel opened before the handshake is closed too.
	dc, ac := inmem.NewPipe()
	p := server.Accept(ac)
	defer p.Close()
	early, err := dc.OpenChannel(ctxT(t), transit.ChannelOpts{Initiator: wire.InitiatorClient, Type: "demo.stream"})
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	if _, err := early.Recv(ctxT(t)); !errors.Is(err, transit.ErrChannelClosed) {
		t.Fatalf("a channel opened before the handshake: Recv err = %v, want ErrChannelClosed", err)
	}
}
