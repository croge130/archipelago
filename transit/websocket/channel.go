package websocket

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

// Channel is a transit.Channel multiplexed over its Conn's single
// WebSocket stream — stream_open/stream_data/stream_end frames
// carrying its id are how the two peers' Conns demux it back out.
type Channel struct {
	id     string
	typ    string
	params json.RawMessage
	conn   *Conn

	inbox       chan wire.Message
	inboxClosed bool // readLoop-owned only; see peerDone and Conn.dispatch's stream_data case
	localClosed chan struct{}
	closeOnce   sync.Once
}

var _ transit.Channel = (*Channel)(nil)

func newChannel(id, typ string, params json.RawMessage, conn *Conn) *Channel {
	return &Channel{
		id:          id,
		typ:         typ,
		params:      params,
		conn:        conn,
		inbox:       make(chan wire.Message, 32),
		localClosed: make(chan struct{}),
	}
}

func (c *Channel) ID() string { return c.id }

func (c *Channel) Type() string { return c.typ }

func (c *Channel) Params() json.RawMessage { return c.params }

func (c *Channel) Send(ctx context.Context, msg wire.Message) error {
	select {
	case <-c.localClosed:
		return transit.ErrChannelClosed
	default:
	}
	msg.Channel = c.id
	if msg.Kind == "" {
		msg.Kind = wire.KindStreamData
	}
	data, err := wire.Encode(msg)
	if err != nil {
		return err
	}
	return c.conn.enqueue(ctx, data)
}

// Recv drains any messages already in flight before reporting
// closed — the inbox is only ever closed by peerDone, and only once,
// so a buffered message is never dropped by a race with that close.
func (c *Channel) Recv(ctx context.Context) (wire.Message, error) {
	select {
	case msg, ok := <-c.inbox:
		if !ok {
			return wire.Message{}, transit.ErrChannelClosed
		}
		return msg, nil
	case <-ctx.Done():
		return wire.Message{}, ctx.Err()
	case <-c.conn.closed:
		return wire.Message{}, transit.ErrSessionClosed
	}
}

// Close ends this end of the channel and best-effort notifies the
// peer via a stream_end frame — best-effort because Close must never
// block on a full send queue.
func (c *Channel) Close(reason string) error {
	c.closeOnce.Do(func() {
		close(c.localClosed)
		c.conn.removeChannel(c.id)
		if data, err := wire.Encode(wire.Message{Kind: wire.KindStreamEnd, Channel: c.id}); err == nil {
			select {
			case c.conn.sendCh <- data:
			default:
			}
		}
	})
	return nil
}

func (c *Channel) Done() <-chan struct{} { return c.localClosed }

// peerDone handles an incoming stream_end: the peer is done sending,
// so Recv should drain whatever's buffered and then report closed.
// It deliberately does not close localClosed — this side may still
// Send until it calls Close itself, the same half-duplex shape
// transit/inmem's Channel already gives.
//
// Only ever called from Conn's single readLoop goroutine, same as
// the inboxClosed read in Conn.dispatch's stream_data case — plain
// field access is safe without a mutex because there is never a
// second goroutine touching it, not because the access happens to be
// small.
func (c *Channel) peerDone() {
	if c.inboxClosed {
		return // a duplicate or malicious second stream_end; don't double-close
	}
	c.inboxClosed = true
	close(c.inbox)
}
