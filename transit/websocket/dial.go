package websocket

import (
	"context"
	"fmt"
	"net/http"

	coderws "github.com/coder/websocket"
	"github.com/croge130/archipelago/transit"
)

// Dial connects to a WebSocket server as a client, returning its
// Session. remoteAddr on the resulting Session is url itself — a
// Dial response carries no net.Addr the way an http.Request does.
//
// client is used for the connection; pass nil for the default
// (plain HTTP). A caller that needs mTLS supplies an *http.Client
// whose Transport sets TLSClientConfig — see the mtls integration
// package, which builds exactly that from certstore-issued
// certificates.
func Dial(ctx context.Context, url string, client *http.Client) (transit.Session, error) {
	ws, resp, err := coderws.Dial(ctx, url, &coderws.DialOptions{HTTPClient: client})
	if err != nil {
		return nil, fmt.Errorf("transit/websocket: dial: %w", err)
	}
	var identity transit.PeerIdentity
	if resp != nil && resp.TLS != nil {
		identity = peerIdentityFromTLS(resp.TLS)
	}
	return newConn(ws, url, identity), nil
}
