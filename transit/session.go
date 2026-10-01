package transit

import (
	"context"
	"errors"

	"github.com/croge130/archipelago/wire"
)

// ErrSessionClosed is returned by a Session's send-side methods once
// the underlying connection has ended.
var ErrSessionClosed = errors.New("transit: session closed")

// ErrChannelClosed is returned by a Channel's methods once it has
// been closed, by either side.
var ErrChannelClosed = errors.New("transit: channel closed")

// Session is one connected peer, backend-agnostic. A caller that only
// ever touches Session and Channel never learns which protocol is
// live underneath.
type Session interface {
	RemoteAddr() string
	PeerIdentity() PeerIdentity
	Capabilities() Capabilities
	Done() <-chan struct{}

	// Reply is the RequestResponse delivery class: a correlated
	// answer to one exchange, matched by msg.ID.
	Reply(ctx context.Context, msg wire.Message) error

	// Push is the EventPush delivery class: a reliable,
	// non-blocking-to-the-caller peer-directed event with no reply
	// expected.
	Push(ctx context.Context, msg wire.Message) error

	// OpenChannel is the ChannelStream delivery class: a long-lived,
	// ordered, backpressured logical stream.
	OpenChannel(ctx context.Context, opts ChannelOpts) (Channel, error)
}

// ChannelOpts describes the channel a caller wants to open.
// Bidirectional distinguishes a console-relay-style channel (needs
// both directions) from a subscription-style one (server→client
// only), so a WebTransport backend can map it onto a bidi or uni
// stream exactly rather than guessing.
type ChannelOpts struct {
	Initiator     wire.Initiator
	Bidirectional bool
}

// Channel is one long-lived logical stream, scoped to a Session.
type Channel interface {
	ID() string
	Send(ctx context.Context, msg wire.Message) error
	Recv(ctx context.Context) (wire.Message, error)
	Close(reason string) error
	Done() <-chan struct{}
}

// Backend is what makes the protocol swappable — the composition
// root that wires Transit into an app selects one.
type Backend interface {
	Accept(ctx context.Context) (Session, error)
	Close() error
}
