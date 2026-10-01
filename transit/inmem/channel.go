package inmem

import (
	"context"
	"sync"

	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

// Channel is a loopback transit.Channel — one end of a pair wired
// together by newChannelPair.
type Channel struct {
	id          string
	outbox      chan wire.Message // this end writes here; closing it tells the peer "no more sends"
	inbox       chan wire.Message // this end reads here (the peer's outbox)
	closeOnce   sync.Once
	localClosed chan struct{}
}

var _ transit.Channel = (*Channel)(nil)

func newChannelPair(id string) (a, b *Channel) {
	const bufSize = 16
	ab := make(chan wire.Message, bufSize)
	ba := make(chan wire.Message, bufSize)
	a = &Channel{id: id, outbox: ab, inbox: ba, localClosed: make(chan struct{})}
	b = &Channel{id: id, outbox: ba, inbox: ab, localClosed: make(chan struct{})}
	return a, b
}

func (c *Channel) ID() string { return c.id }

func (c *Channel) Send(ctx context.Context, msg wire.Message) error {
	select {
	case <-c.localClosed:
		return transit.ErrChannelClosed
	default:
	}
	select {
	case c.outbox <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.localClosed:
		return transit.ErrChannelClosed
	}
}

// Recv drains any messages already in flight before reporting
// closed — closing outbox is a native Go channel close, so buffered
// sends are delivered in order before the zero-value/closed signal,
// the same "strengthen, never weaken" guarantee the delivery-class
// model uses elsewhere.
func (c *Channel) Recv(ctx context.Context) (wire.Message, error) {
	select {
	case msg, ok := <-c.inbox:
		if !ok {
			return wire.Message{}, transit.ErrChannelClosed
		}
		return msg, nil
	case <-ctx.Done():
		return wire.Message{}, ctx.Err()
	}
}

// Close ends this end of the channel. Safe to call more than once or
// from both ends independently.
func (c *Channel) Close(reason string) error {
	c.closeOnce.Do(func() {
		close(c.localClosed)
		close(c.outbox)
	})
	return nil
}

func (c *Channel) Done() <-chan struct{} { return c.localClosed }
