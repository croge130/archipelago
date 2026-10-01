package websocket

import (
	"context"
	"net/http"
	"sync"

	coderws "github.com/coder/websocket"
	"github.com/croge130/archipelago/transit"
)

// Backend is the server-side transit.Backend: an http.Handler that
// upgrades incoming connections and queues them for Accept, the same
// shape Lighthouse's own composition root uses (accept channel
// queued, Accept blocks until one is ready).
type Backend struct {
	acceptCh chan *Conn

	closed    chan struct{}
	closeOnce sync.Once
}

var _ transit.Backend = (*Backend)(nil)
var _ http.Handler = (*Backend)(nil)

func NewBackend() *Backend {
	return &Backend{
		acceptCh: make(chan *Conn, 16),
		closed:   make(chan struct{}),
	}
}

func (b *Backend) Accept(ctx context.Context) (transit.Session, error) {
	select {
	case conn := <-b.acceptCh:
		return conn, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.closed:
		return nil, transit.ErrSessionClosed
	}
}

func (b *Backend) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

// ServeHTTP upgrades the request to a WebSocket connection and queues
// the resulting Session for Accept. mTLS peer identity, if any, is
// captured here from r.TLS — at accept, where the Session is created,
// exactly as 11-transit-model.md's ported design calls for.
func (b *Backend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := coderws.Accept(w, r, nil)
	if err != nil {
		return
	}
	identity := peerIdentityFromTLS(r.TLS)
	conn := newConn(ws, r.RemoteAddr, identity)
	select {
	case b.acceptCh <- conn:
	case <-b.closed:
		conn.shutdown()
	}
}
