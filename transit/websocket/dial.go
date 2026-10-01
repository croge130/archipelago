package websocket

import (
	"context"
	"fmt"

	coderws "github.com/coder/websocket"
	"github.com/croge130/archipelago/transit"
)

// Dial connects to a WebSocket server as a client, returning its
// Session. remoteAddr on the resulting Session is url itself — a
// Dial response carries no net.Addr the way an http.Request does.
func Dial(ctx context.Context, url string) (transit.Session, error) {
	ws, resp, err := coderws.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("transit/websocket: dial: %w", err)
	}
	var identity transit.PeerIdentity
	if resp != nil && resp.TLS != nil {
		identity = peerIdentityFromTLS(resp.TLS)
	}
	return newConn(ws, url, identity), nil
}
