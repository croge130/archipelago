package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/croge130/archipelago/logging"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

// readLoop is the only reader of the session's non-channel messages. It
// decodes and hands off; it never runs a handler, so a stuck handler
// cannot stop it from reading a cancel frame.
func (p *Peer) readLoop() {
	for {
		msg, err := p.conn.Next(p.ctx)
		if err != nil {
			p.shutdown("receive loop ended: " + err.Error())
			return
		}
		p.handle(msg)
	}
}

func (p *Peer) handle(msg wire.Message) {
	switch msg.Kind {
	case wire.KindResponse, wire.KindError, wire.KindAck:
		p.deliver(msg)
		return
	case wire.KindCancel:
		p.cancelInflight(msg.ID)
		return
	case wire.KindRequest, wire.KindEvent:
	default:
		p.log.Debug("ignoring a frame of an unexpected kind", "kind", msg.Kind, "type", msg.Type)
		return
	}
	if err := msg.Validate(); err != nil {
		p.refuse(msg, wire.ErrInvalid, "malformed message")
		return
	}
	if msg.Type == wire.TypeHello {
		p.handleHello(msg)
		return
	}
	if !p.handshakeDone() {
		p.refuse(msg, wire.ErrHelloRequired, "complete the handshake first")
		return
	}
	route, ok := p.r.route(msg.Type)
	if !ok {
		if msg.Kind == wire.KindRequest {
			p.reply(wire.ErrorReply(msg, wire.ErrUnknownRoute, "no such route"))
		} else {
			// A newer peer may send events an older one does not know.
			p.log.Debug("ignoring an event with no route", "type", msg.Type)
		}
		return
	}
	p.enqueue(route, msg)
}

// refuse answers a request with a coded error; an event has no reply
// path, so it is only logged.
func (p *Peer) refuse(msg wire.Message, code wire.ErrorCode, text string) {
	if msg.Kind == wire.KindRequest {
		p.reply(wire.ErrorReply(msg, code, text))
		return
	}
	p.log.Debug("dropping an event", "type", msg.Type, "code", code)
}

func (p *Peer) deliver(msg wire.Message) {
	if msg.ID == "" {
		return
	}
	p.mu.Lock()
	wait, ok := p.pending[msg.ID]
	p.mu.Unlock()
	if !ok {
		p.log.Debug("response with no waiting call", "type", msg.Type, "id", msg.ID)
		return
	}
	select {
	case wait <- msg:
	default: // a duplicate; the first one already won
	}
}

func (p *Peer) cancelInflight(id string) {
	if id == "" {
		return
	}
	p.mu.Lock()
	cancel := p.inflight[id]
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *Peer) handleHello(msg wire.Message) {
	if msg.Kind != wire.KindRequest {
		return
	}
	var h wire.Hello
	if err := json.Unmarshal(msg.Payload, &h); err != nil || h.Validate() != nil {
		p.reply(wire.ErrorReply(msg, wire.ErrInvalid, "malformed handshake"))
		return
	}
	if p.handshakeDone() {
		p.reply(wire.ErrorReply(msg, wire.ErrInvalid, "the handshake already completed"))
		return
	}
	version, ok := wire.NegotiateVersion(p.r.opts.MinProtocol, p.r.opts.MaxProtocol, h.MinVersion, h.MaxVersion)
	if !ok {
		p.log.Warn("no protocol version in common", "peer_min", h.MinVersion, "peer_max", h.MaxVersion)
		p.reply(wire.ErrorReply(msg, wire.ErrVersionUnsupported,
			fmt.Sprintf("this side speaks protocol versions %d to %d", p.r.opts.MinProtocol, p.r.opts.MaxProtocol)))
		return
	}
	p.setHandshake(version)
	payload, _ := json.Marshal(wire.HelloReply{Version: version, MaxVersion: p.r.opts.MaxProtocol})
	p.reply(wire.Message{Type: msg.Type, ID: msg.ID, Kind: wire.KindResponse, Payload: payload})
	p.log.Info("handshake complete", "role", "acceptor", "version", version, "peer_instance_hint", h.InstanceID)
}

// acquire reserves one of the session's in-flight slots.
func (p *Peer) acquire() bool {
	if p.inFlight.Add(1) > int64(p.r.opts.MaxInFlight) {
		p.inFlight.Add(-1)
		return false
	}
	return true
}

func (p *Peer) release() { p.inFlight.Add(-1) }

// enqueue hands msg to its route's queue. A route handles messages in
// arrival order; Concurrency workers drain the queue, so Concurrency 1
// (the default) is strictly sequential.
func (p *Peer) enqueue(route Route, msg wire.Message) {
	if !p.acquire() {
		if msg.Kind == wire.KindRequest {
			p.reply(wire.ErrorReply(msg, wire.ErrBusy, "too many requests in flight; retry later"))
		} else {
			p.droppedEvents.Add(1)
			p.log.Warn("dropping an event: the in-flight limit is reached", "type", msg.Type)
		}
		return
	}
	q := p.queueFor(route)
	select {
	case q.ch <- msg:
	default:
		// Cannot happen while the queue is as large as the in-flight
		// limit, but fail safe rather than block the read loop.
		p.release()
		p.refuse(msg, wire.ErrBusy, "route queue full")
	}
}

func (p *Peer) queueFor(route Route) *routeQueue {
	p.mu.Lock()
	defer p.mu.Unlock()
	if q, ok := p.queues[route.Type]; ok {
		return q
	}
	q := &routeQueue{ch: make(chan wire.Message, p.r.opts.MaxInFlight)}
	p.queues[route.Type] = q
	for i := 0; i < route.Concurrency; i++ {
		go p.worker(route, q)
	}
	return q
}

func (p *Peer) worker(route Route, q *routeQueue) {
	for {
		select {
		case msg := <-q.ch:
			p.run(route, msg)
			p.release()
		case <-p.ctx.Done():
			return
		}
	}
}

// run handles one message: derive the handler's context (cancellable by
// a cancel frame, ended by the session, carrying a child span of the
// sender's trace), authorize, run the handler, and send the result.
func (p *Peer) run(route Route, msg wire.Message) {
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()

	isRequest := msg.Kind == wire.KindRequest
	if isRequest && msg.ID != "" {
		p.mu.Lock()
		p.inflight[msg.ID] = cancel
		p.mu.Unlock()
		defer func() {
			p.mu.Lock()
			delete(p.inflight, msg.ID)
			p.mu.Unlock()
		}()
	}

	span := logging.NewRootSpan()
	if parent, ok := msg.TraceContext(); ok {
		span = logging.NewChildSpan(parent)
	}
	ctx = logging.ContextWithSpan(ctx, span)
	attrs := span.Attrs()
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	log := p.log.With(args...)
	started := time.Now()

	outcome := "ok"
	defer func() {
		log.Debug("handled", "type", msg.Type, "kind", msg.Kind, "outcome", outcome, "duration", time.Since(started))
	}()

	if route.Authorize != nil {
		if err := route.Authorize(ctx, p.conn, msg); err != nil {
			outcome = string(wire.ErrUnauthorized)
			log.Warn("refused: not authorized", "type", msg.Type, "reason", err.Error())
			if isRequest {
				p.reply(wire.ErrorReply(msg, wire.ErrUnauthorized, "not authorized"))
			}
			return
		}
	}

	result, err := p.safely(ctx, route.Handler, Request{Message: msg, Session: p.conn, Peer: p})
	if p.ctx.Err() != nil {
		outcome = "session_ended"
		return // nobody is listening
	}
	if err != nil {
		code, text := classify(ctx, err)
		outcome = string(code)
		if code == wire.ErrInternal {
			log.Error("handler failed", "type", msg.Type, "error", err.Error())
		}
		if isRequest {
			p.reply(withTrace(wire.ErrorReply(msg, code, text), span))
		}
		return
	}
	if isRequest {
		p.reply(withTrace(wire.Message{Type: msg.Type, ID: msg.ID, Kind: wire.KindResponse, Payload: result}, span))
	}
}

// safely runs a handler, turning a panic into an internal error so one
// bad handler cannot take the session down.
func (p *Peer) safely(ctx context.Context, h Handler, req Request) (result json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panicked: %v", r)
		}
	}()
	return h(ctx, req)
}

// classify maps a handler's error to a wire code and the text safe to
// send. Only an *Error carries a caller-visible message; anything else
// is reported as a generic internal error.
func classify(ctx context.Context, err error) (wire.ErrorCode, string) {
	var re *Error
	if errors.As(err, &re) && re.Code.Valid() {
		return re.Code, re.Message
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return wire.ErrCancelled, "cancelled"
	}
	return wire.ErrInternal, "internal error"
}

func withTrace(msg wire.Message, span logging.SpanContext) wire.Message {
	return msg.WithTraceContext(span)
}

// channelLoop accepts channels the peer opens and dispatches them to
// channel routes by type.
func (p *Peer) channelLoop() {
	for {
		ch, err := p.conn.AcceptChannel(p.ctx)
		if err != nil {
			return // the read loop ends the session; this loop just stops
		}
		p.handleChannel(ch)
	}
}

func (p *Peer) handleChannel(ch transit.Channel) {
	if !p.handshakeDone() {
		_ = ch.Close(string(wire.ErrHelloRequired))
		return
	}
	route, ok := p.r.channelRoute(ch.Type())
	if !ok {
		p.log.Debug("closing a channel with no route", "channel_type", ch.Type())
		_ = ch.Close(string(wire.ErrUnknownRoute))
		return
	}
	if !p.acquire() {
		_ = ch.Close(string(wire.ErrBusy))
		return
	}
	go func() {
		defer p.release()
		defer func() { _ = ch.Close("done") }()
		ctx, cancel := context.WithCancel(p.ctx)
		defer cancel()
		span := logging.NewRootSpan()
		ctx = logging.ContextWithSpan(ctx, span)
		open := wire.Message{Type: ch.Type(), Kind: wire.KindStreamOpen, Channel: ch.ID(), Payload: ch.Params()}
		if route.Authorize != nil {
			if err := route.Authorize(ctx, p.conn, open); err != nil {
				p.log.Warn("refused a channel: not authorized", "channel_type", ch.Type(), "reason", err.Error())
				_ = ch.Close(string(wire.ErrUnauthorized))
				return
			}
		}
		if err := p.safelyChannel(ctx, route.Handler, ChannelRequest{Channel: ch, Session: p.conn, Peer: p}); err != nil && p.ctx.Err() == nil {
			p.log.Error("channel handler failed", "channel_type", ch.Type(), "error", err.Error())
		}
	}()
}

func (p *Peer) safelyChannel(ctx context.Context, h ChannelHandler, req ChannelRequest) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("channel handler panicked: %v", r)
		}
	}()
	return h(ctx, req)
}
