package inmem

import (
	"context"
	"sync"

	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

// Session is a loopback transit.Session — see the package doc.
type Session struct {
	remoteAddr string
	identity   transit.PeerIdentity
	caps       transit.Capabilities

	peer *Session

	incoming     chan wire.Message
	channelOpens chan *Channel

	closeOnce sync.Once
	closed    chan struct{}
}

var _ transit.Session = (*Session)(nil)

// NewPipe returns two connected Sessions, each the other's peer —
// the in-memory analogue of net.Pipe at the Session level.
func NewPipe() (a, b *Session) {
	a = newSession("pipe-a")
	b = newSession("pipe-b")
	a.peer = b
	b.peer = a
	return a, b
}

func newSession(addr string) *Session {
	return &Session{
		remoteAddr:   addr,
		incoming:     make(chan wire.Message, 16),
		channelOpens: make(chan *Channel, 4),
		closed:       make(chan struct{}),
	}
}

// WithPeerIdentity sets the identity this Session reports — a test
// helper for exercising mTLS-aware callers without a real certificate.
func (s *Session) WithPeerIdentity(identity transit.PeerIdentity) *Session {
	s.identity = identity
	return s
}

// WithCapabilities sets the capabilities this Session reports.
func (s *Session) WithCapabilities(caps transit.Capabilities) *Session {
	s.caps = caps
	return s
}

func (s *Session) RemoteAddr() string                 { return s.remoteAddr }
func (s *Session) PeerIdentity() transit.PeerIdentity { return s.identity }
func (s *Session) Capabilities() transit.Capabilities { return s.caps }
func (s *Session) Done() <-chan struct{}              { return s.closed }

// Close ends the session from this side. Pending and future
// Reply/Push/OpenChannel calls on either side fail with
// ErrSessionClosed once the closing side's Done channel fires.
func (s *Session) Close() {
	s.closeOnce.Do(func() { close(s.closed) })
}

func (s *Session) Reply(ctx context.Context, msg wire.Message) error {
	return s.send(ctx, msg)
}

func (s *Session) Push(ctx context.Context, msg wire.Message) error {
	return s.send(ctx, msg)
}

func (s *Session) send(ctx context.Context, msg wire.Message) error {
	select {
	case s.peer.incoming <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-s.closed:
		return transit.ErrSessionClosed
	case <-s.peer.closed:
		return transit.ErrSessionClosed
	}
}

// Next returns the next message the peer Reply'd or Push'd to this
// session — see the package doc for why this exists in place of a
// Router.
func (s *Session) Next(ctx context.Context) (wire.Message, error) {
	select {
	case msg := <-s.incoming:
		return msg, nil
	case <-ctx.Done():
		return wire.Message{}, ctx.Err()
	case <-s.closed:
		return wire.Message{}, transit.ErrSessionClosed
	}
}

func (s *Session) OpenChannel(ctx context.Context, opts transit.ChannelOpts) (transit.Channel, error) {
	id := wire.NewChannelID(opts.Initiator)
	local, remote := newChannelPair(id)
	select {
	case s.peer.channelOpens <- remote:
		return local, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, transit.ErrSessionClosed
	case <-s.peer.closed:
		return nil, transit.ErrSessionClosed
	}
}

// AcceptChannel returns the next channel the peer opened — see the
// package doc for why this exists in place of a Router.
func (s *Session) AcceptChannel(ctx context.Context) (transit.Channel, error) {
	select {
	case ch := <-s.channelOpens:
		return ch, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.closed:
		return nil, transit.ErrSessionClosed
	}
}
