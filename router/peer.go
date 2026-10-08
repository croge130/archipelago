package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
	"github.com/google/uuid"
)

// ErrHandshakeIncomplete is returned by an operation that needs a
// completed handshake on a session that has not finished one.
var ErrHandshakeIncomplete = errors.New("router: the handshake has not completed on this session")

// Peer is the router's runtime for one session. It runs the receive
// loop, correlates responses to the calls waiting on them, dispatches
// what arrives to the Router's routes, and lets this end call the other.
// Both ends of a session run a Peer, each with its own routes.
type Peer struct {
	r    *Router
	conn transit.Conn
	log  *slog.Logger

	ctx    context.Context // cancelled when the session ends; handlers run under it
	cancel context.CancelFunc
	done   chan struct{}

	closeOnce sync.Once

	mu         sync.Mutex
	pending    map[string]chan wire.Message  // outstanding calls, by request ID
	inflight   map[string]context.CancelFunc // running request handlers, by request ID
	queues     map[string]*routeQueue        // per route type
	helloDone  bool
	version    int
	helloTimer *time.Timer

	inFlight      atomic.Int64 // handlers queued or running
	droppedEvents atomic.Uint64
}

type routeQueue struct {
	ch chan wire.Message
}

func (r *Router) newPeer(conn transit.Conn) *Peer {
	ctx, cancel := context.WithCancel(context.Background())
	p := &Peer{
		r:        r,
		conn:     conn,
		log:      r.opts.Logger.With("peer", conn.RemoteAddr(), "peer_fingerprint", conn.PeerIdentity().Fingerprint),
		ctx:      ctx,
		cancel:   cancel,
		done:     make(chan struct{}),
		pending:  map[string]chan wire.Message{},
		inflight: map[string]context.CancelFunc{},
		queues:   map[string]*routeQueue{},
	}
	go p.readLoop()
	go p.channelLoop()
	go func() {
		select {
		case <-conn.Done():
			p.shutdown("the session ended")
		case <-p.done:
		}
	}()
	return p
}

// Accept serves a session this node accepted (from a Backend). The peer
// must complete the handshake within Options.HelloTimeout or the session
// is closed; until then only the handshake is answered.
func (r *Router) Accept(conn transit.Conn) *Peer {
	p := r.newPeer(conn)
	p.mu.Lock()
	p.helloTimer = time.AfterFunc(r.opts.HelloTimeout, func() {
		if !p.handshakeDone() {
			p.log.Warn("no handshake before the timeout; closing the session", "timeout", r.opts.HelloTimeout)
			p.Close()
		}
	})
	p.mu.Unlock()
	return p
}

// Connect serves a session this node dialed and performs the handshake
// as the dialer. instanceID is an optional logging hint for the other
// side. On failure the session is closed and the error is returned; a
// version mismatch arrives as a *RemoteError with ErrVersionUnsupported.
func (r *Router) Connect(ctx context.Context, conn transit.Conn, instanceID string) (*Peer, error) {
	p := r.newPeer(conn)
	hello := wire.Hello{MinVersion: r.opts.MinProtocol, MaxVersion: r.opts.MaxProtocol, InstanceID: instanceID}
	payload, err := json.Marshal(hello)
	if err != nil {
		p.Close()
		return nil, fmt.Errorf("router: connect: %w", err)
	}
	raw, err := p.call(ctx, wire.TypeHello, payload)
	if err != nil {
		p.Close()
		return nil, fmt.Errorf("router: connect: %w", err)
	}
	var reply wire.HelloReply
	if err := json.Unmarshal(raw, &reply); err != nil || reply.Version < 1 {
		p.Close()
		return nil, fmt.Errorf("router: connect: the peer's handshake reply was malformed")
	}
	p.setHandshake(reply.Version)
	p.log.Info("handshake complete", "role", "dialer", "version", reply.Version)
	return p, nil
}

// Session is the underlying session.
func (p *Peer) Session() transit.Session { return p.conn }

// Done is closed when the session has ended.
func (p *Peer) Done() <-chan struct{} { return p.done }

// Version is the protocol version negotiated for this session, or 0
// before the handshake.
func (p *Peer) Version() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.version
}

// DroppedEvents counts events discarded because the in-flight limit was
// reached. A request over the limit is answered busy so its sender
// knows; an event has no reply path, so the only honest signal is this
// counter and a log line. (21 lists this as an open question.)
func (p *Peer) DroppedEvents() uint64 { return p.droppedEvents.Load() }

// InFlight is how many handlers are queued or running right now.
func (p *Peer) InFlight() int { return int(p.inFlight.Load()) }

// Close ends the session: handlers are cancelled and waiting calls fail.
func (p *Peer) Close() {
	p.shutdown("closed locally")
	switch c := p.conn.(type) {
	case interface{ Close() error }:
		_ = c.Close()
	case interface{ Close() }:
		c.Close()
	}
}

func (p *Peer) shutdown(reason string) {
	p.closeOnce.Do(func() {
		p.cancel()
		close(p.done)
		p.mu.Lock()
		if p.helloTimer != nil {
			p.helloTimer.Stop()
		}
		p.mu.Unlock()
		p.log.Debug("session ended", "reason", reason)
	})
}

func (p *Peer) handshakeDone() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.helloDone
}

func (p *Peer) setHandshake(version int) {
	p.mu.Lock()
	p.helloDone = true
	p.version = version
	if p.helloTimer != nil {
		p.helloTimer.Stop()
	}
	p.mu.Unlock()
}

// Call sends a request and waits for its response. The error is a
// *RemoteError when the peer answered with a coded error, ctx's error
// if the context ends first (a cancel frame is then sent so the peer can
// stop the handler), or transit.ErrSessionClosed if the session ends.
//
// The context's deadline governs how long the caller waits. It is not
// shipped as an absolute time: the peer cancels the handler when it
// receives the cancel frame or loses the session, never by comparing
// clocks (19).
func (p *Peer) Call(ctx context.Context, typ string, payload json.RawMessage) (json.RawMessage, error) {
	if !p.handshakeDone() {
		return nil, ErrHandshakeIncomplete
	}
	return p.call(ctx, typ, payload)
}

// call is Call without the handshake gate, so the handshake itself can
// use it.
func (p *Peer) call(ctx context.Context, typ string, payload json.RawMessage) (json.RawMessage, error) {
	select {
	case <-p.done:
		return nil, transit.ErrSessionClosed
	default:
	}
	id := uuid.NewString()
	wait := make(chan wire.Message, 1)
	p.mu.Lock()
	p.pending[id] = wait
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
	}()

	msg := stampTrace(ctx, wire.Message{Type: typ, Payload: payload, ID: id, Kind: wire.KindRequest})
	// Session has no "send a request" method: Reply answers and Push
	// sends. Push is used for everything a node initiates.
	if err := p.conn.Push(ctx, msg); err != nil {
		return nil, err
	}
	select {
	case resp := <-wait:
		if resp.Kind == wire.KindError {
			e := wire.DecodeError(resp)
			return nil, &RemoteError{Code: e.Code, Message: e.Message}
		}
		return resp.Payload, nil
	case <-ctx.Done():
		p.sendBestEffort(wire.Message{Type: typ, ID: id, Kind: wire.KindCancel})
		return nil, ctx.Err()
	case <-p.done:
		return nil, transit.ErrSessionClosed
	}
}

// Push sends an event: no ID, no reply, reliable per the transport.
func (p *Peer) Push(ctx context.Context, typ string, payload json.RawMessage) error {
	if !p.handshakeDone() {
		return ErrHandshakeIncomplete
	}
	return p.conn.Push(ctx, stampTrace(ctx, wire.Message{Type: typ, Payload: payload, Kind: wire.KindEvent}))
}

// OpenChannel opens a typed channel on the session, after the handshake.
func (p *Peer) OpenChannel(ctx context.Context, opts transit.ChannelOpts) (transit.Channel, error) {
	if !p.handshakeDone() {
		return nil, ErrHandshakeIncomplete
	}
	return p.conn.OpenChannel(ctx, opts)
}

// stampTrace puts a child of ctx's span on msg, if ctx has one, so the
// receiving handler continues the same trace.
func stampTrace(ctx context.Context, msg wire.Message) wire.Message {
	if sc, ok := logging.SpanFromContext(ctx); ok {
		return msg.WithTraceContext(logging.NewChildSpan(sc))
	}
	return msg
}

// sendBestEffort writes a frame the caller will not wait on.
func (p *Peer) sendBestEffort(msg wire.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), p.r.opts.SendTimeout)
	defer cancel()
	if err := p.conn.Push(ctx, msg); err != nil {
		p.log.Debug("could not send a frame", "kind", msg.Kind, "type", msg.Type, "error", err)
	}
}

// reply writes a response or error frame back. It uses its own deadline
// rather than the handler's context, which may already be done.
func (p *Peer) reply(msg wire.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), p.r.opts.SendTimeout)
	defer cancel()
	if err := p.conn.Reply(ctx, msg); err != nil {
		p.log.Debug("could not send a reply", "kind", msg.Kind, "type", msg.Type, "error", err)
	}
}
