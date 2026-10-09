package routerauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/peerauth"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/wire"
	"github.com/google/uuid"
)

// EndpointsListRoute is the route (and endpoint key) peers call to learn
// what this node exposes.
const EndpointsListRoute = "endpoints.list"

// EndpointInfo is one advertised endpoint on the wire.
type EndpointInfo struct {
	Key                string          `json:"key"`
	Description        string          `json:"description,omitempty"`
	RequiredPermission string          `json:"required_permission,omitempty"`
	Metadata           json.RawMessage `json:"metadata,omitempty"`
}

// EndpointsListReply is the endpoints.list response.
type EndpointsListReply struct {
	Endpoints []EndpointInfo `json:"endpoints"`
}

// HandleEndpointsList registers endpoints.list, a one-line handler over
// facade.AdvertiseEndpoints. filterByGrant is 15's visibility policy: true
// shows the caller only what its principal may call (a peer whose
// certificate resolves to no principal then sees public endpoints only);
// false shows every registered endpoint and denies at call time. The route
// is itself public, so a peer can always ask what it is allowed to do.
func (g *Registrar) HandleEndpointsList(ctx context.Context, filterByGrant bool) error {
	return g.Handle(ctx, Endpoint{
		Key:         EndpointsListRoute,
		Description: "List the endpoints this node exposes to the caller",
		Handler: func(ctx context.Context, req router.Request) (json.RawMessage, error) {
			principalID := uuid.Nil
			if filterByGrant {
				id, err := peerauth.ResolvePrincipal(ctx, g.reader, req.Session)
				switch {
				case err == nil:
					principalID = id
				case errors.Is(err, peerauth.ErrNoPeerIdentity), errors.Is(err, peerauth.ErrUnknownPeer):
					// No principal: the nil principal holds nothing, so
					// only endpoints with no requirement are shown.
				default:
					return nil, err
				}
			}
			defs, err := facade.AdvertiseEndpoints(ctx, g.reader, principalID, filterByGrant)
			if err != nil {
				return nil, err
			}
			reply := EndpointsListReply{Endpoints: make([]EndpointInfo, len(defs))}
			for i, d := range defs {
				reply.Endpoints[i] = EndpointInfo{
					Key:                d.EndpointKey,
					Description:        d.Description,
					RequiredPermission: d.RequiredPermissionKey,
					Metadata:           d.Metadata,
				}
			}
			out, err := json.Marshal(reply)
			if err != nil {
				return nil, fmt.Errorf("routerauth: encode endpoints.list reply: %w", err)
			}
			return out, nil
		},
		AllowReservedNamespace: true,
	})
}

// ListEndpoints asks a peer what it exposes to this node.
func ListEndpoints(ctx context.Context, peer *router.Peer) ([]EndpointInfo, error) {
	raw, err := peer.Call(ctx, EndpointsListRoute, nil)
	if err != nil {
		return nil, err
	}
	var reply EndpointsListReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, &router.RemoteError{Code: wire.ErrInternal, Message: "malformed endpoints.list reply"}
	}
	return reply.Endpoints, nil
}
