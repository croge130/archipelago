package transit

import (
	"context"
	"encoding/json"
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

	// Type says what the channel is for (a console relay, a subscription,
	// a streaming task), and Params optionally carries its setup. Both
	// travel in the stream_open frame and are visible to the accepting
	// side, which is what lets a router dispatch an accepted channel to
	// the right handler. An empty Type is allowed at this layer; whether a
	// router accepts it is the router's decision.
	Type   string
	Params json.RawMessage
}

// Channel is one long-lived logical stream, scoped to a Session.
type Channel interface {
	ID() string

	// Type and Params are what the opener said the channel is for, on
	// both ends: the opener sees what it passed, the acceptor what the
	// stream_open frame carried.
	Type() string
	Params() json.RawMessage

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

// Receiver is the inbound side of a session: the messages and channels
// the peer sends. Session is deliberately outbound-only (Reply, Push,
// OpenChannel); anything that runs a receive loop — a router — takes a
// value that is both. Both backends satisfy it.
//
// Next returns the next message that was not part of a channel, in
// arrival order. AcceptChannel returns the next channel the peer opened.
// Both return ErrSessionClosed once the session ends.
type Receiver interface {
	Next(ctx context.Context) (wire.Message, error)
	AcceptChannel(ctx context.Context) (Channel, error)
}

// Conn is what a router needs from a connection: send and receive.
type Conn interface {
	Session
	Receiver
}
