package websocket

import (
	"context"
	"encoding/json"
	"sync"

	coderws "github.com/coder/websocket"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

// Conn is a transit.Session backed by a real WebSocket connection.
type Conn struct {
	ws         *coderws.Conn
	remoteAddr string
	identity   transit.PeerIdentity

	sendCh chan []byte

	closed    chan struct{}
	closeOnce sync.Once

	incoming chan wire.Message
	acceptCh chan *Channel

	mu       sync.Mutex
	channels map[string]*Channel
}

var _ transit.Session = (*Conn)(nil)

func newConn(ws *coderws.Conn, remoteAddr string, identity transit.PeerIdentity) *Conn {
	c := &Conn{
		ws:         ws,
		remoteAddr: remoteAddr,
		identity:   identity,
		sendCh:     make(chan []byte, 64),
		closed:     make(chan struct{}),
		incoming:   make(chan wire.Message, 64),
		acceptCh:   make(chan *Channel, 8),
		channels:   make(map[string]*Channel),
	}
	go c.writeLoop()
	go c.readLoop()
	return c
}

func (c *Conn) RemoteAddr() string { return c.remoteAddr }

func (c *Conn) PeerIdentity() transit.PeerIdentity { return c.identity }

// Capabilities is the zero value: WebSocket has exactly one reliable
// ordered stream, no independent network-level streams, no real
// datagrams.
func (c *Conn) Capabilities() transit.Capabilities { return transit.Capabilities{} }

func (c *Conn) Done() <-chan struct{} { return c.closed }

// Close ends the connection. Safe to call more than once.
func (c *Conn) Close() error {
	c.shutdown()
	return nil
}

func (c *Conn) Reply(ctx context.Context, msg wire.Message) error { return c.send(ctx, msg) }
func (c *Conn) Push(ctx context.Context, msg wire.Message) error  { return c.send(ctx, msg) }

func (c *Conn) send(ctx context.Context, msg wire.Message) error {
	data, err := wire.Encode(msg)
	if err != nil {
		return err
	}
	return c.enqueue(ctx, data)
}

// enqueue puts data on sendCh for the writer goroutine, checking
// closed first and non-blocking: sendCh is buffered, so a send can
// still succeed even after closed fires (select has no priority among
// simultaneously-ready cases), making a that-should-reliably-fail
// send flaky without this pre-check. A send genuinely concurrent with
// Close stays best-effort either way, the same as a real network
// write racing a connection drop.
func (c *Conn) enqueue(ctx context.Context, data []byte) error {
	select {
	case <-c.closed:
		return transit.ErrSessionClosed
	default:
	}
	select {
	case c.sendCh <- data:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return transit.ErrSessionClosed
	}
}

// Next returns the next message that wasn't routed to a Channel —
// see the package doc for why this exists in place of a Router.
func (c *Conn) Next(ctx context.Context) (wire.Message, error) {
	select {
	case msg := <-c.incoming:
		return msg, nil
	case <-ctx.Done():
		return wire.Message{}, ctx.Err()
	case <-c.closed:
		return wire.Message{}, transit.ErrSessionClosed
	}
}

func (c *Conn) OpenChannel(ctx context.Context, opts transit.ChannelOpts) (transit.Channel, error) {
	id := wire.NewChannelID(opts.Initiator)
	ch := c.registerChannel(id, opts.Type, opts.Params)
	data, err := wire.Encode(wire.Message{Kind: wire.KindStreamOpen, Channel: id, Type: opts.Type, Payload: opts.Params})
	if err != nil {
		c.removeChannel(id)
		return nil, err
	}
	if err := c.enqueue(ctx, data); err != nil {
		c.removeChannel(id)
		return nil, err
	}
	return ch, nil
}

// AcceptChannel returns the next channel the peer opened — see the
// package doc for why this exists in place of a Router.
func (c *Conn) AcceptChannel(ctx context.Context) (transit.Channel, error) {
	select {
	case ch := <-c.acceptCh:
		return ch, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.closed:
		return nil, transit.ErrSessionClosed
	}
}

func (c *Conn) registerChannel(id, typ string, params json.RawMessage) *Channel {
	ch := newChannel(id, typ, params, c)
	c.mu.Lock()
	c.channels[id] = ch
	c.mu.Unlock()
	return ch
}

func (c *Conn) removeChannel(id string) {
	c.mu.Lock()
	delete(c.channels, id)
	c.mu.Unlock()
}

func (c *Conn) shutdown() {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.ws.Close(coderws.StatusNormalClosure, "")
	})
}

func (c *Conn) writeLoop() {
	defer c.shutdown()
	for {
		select {
		case data := <-c.sendCh:
			// context.Background() deliberately, not a caller's
			// per-call ctx: the library closes the whole connection
			// on any Write error, context expiration included, so a
			// short per-call timeout must bound only enqueueing into
			// sendCh (above), never the actual wire write.
			if err := c.ws.Write(context.Background(), coderws.MessageText, data); err != nil {
				return
			}
		case <-c.closed:
			return
		}
	}
}

func (c *Conn) readLoop() {
	defer c.shutdown()
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			return
		}
		msg, err := wire.Decode(data)
		if err != nil {
			continue // malformed frame; drop rather than killing the connection
		}
		c.dispatch(msg)
	}
}

func (c *Conn) dispatch(msg wire.Message) {
	switch msg.Kind {
	case wire.KindStreamOpen:
		ch := c.registerChannel(msg.Channel, msg.Type, msg.Payload)
		select {
		case c.acceptCh <- ch:
		case <-c.closed:
		}
	case wire.KindStreamData:
		c.mu.Lock()
		ch, ok := c.channels[msg.Channel]
		c.mu.Unlock()
		// ch.inboxClosed is readLoop-owned (this goroutine), so this
		// check and any concurrent peerDone/Close for the same
		// channel can never race — see Channel.peerDone's own doc.
		if !ok || ch.inboxClosed {
			return
		}
		select {
		case ch.inbox <- msg:
		case <-ch.localClosed:
		case <-c.closed:
		}
	case wire.KindStreamEnd:
		c.mu.Lock()
		ch, ok := c.channels[msg.Channel]
		c.mu.Unlock()
		if ok {
			ch.peerDone()
		}
	default:
		select {
		case c.incoming <- msg:
		case <-c.closed:
		}
	}
}

var _ transit.Conn = (*Conn)(nil)
