package registry

import (
	"context"
	"encoding/json"
	"errors"

	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/peerauth"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/wire"
)

// RouteRegister is the route (and endpoint key) a peer calls to register
// itself as an instance.
const RouteRegister = "registry.register"

// RegisterRequest is RouteRegister's payload. There is deliberately no
// principal field: the instance belongs to whoever the connection's
// verified identity resolves to, never to a name the caller asserts.
type RegisterRequest struct {
	Group    string          `json:"group"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// RegisterHandler is RegisterFromSession as a router handler: the reply
// is the Instance that was registered. Register it with routerauth.Handle
// to choose which principals may register at all; without a permission
// any peer whose certificate resolves to a principal may, which is the
// behaviour RegisterFromSession has always had.
func RegisterHandler(reader gatehouseFacade.Reader, writer gatehouseFacade.Writer) router.Handler {
	return func(ctx context.Context, req router.Request) (json.RawMessage, error) {
		var body RegisterRequest
		if err := json.Unmarshal(req.Message.Payload, &body); err != nil {
			return nil, router.Errorf(wire.ErrInvalid, "register: malformed payload")
		}
		if body.Group == "" {
			return nil, router.Errorf(wire.ErrInvalid, "register: group is required")
		}
		inst, err := RegisterFromSession(ctx, reader, writer, req.Session, body.Group, body.Metadata)
		switch {
		case errors.Is(err, peerauth.ErrNoPeerIdentity), errors.Is(err, peerauth.ErrUnknownPeer):
			return nil, router.Errorf(wire.ErrUnauthorized, "not authorized")
		case err != nil:
			return nil, err
		}
		return json.Marshal(inst)
	}
}

// Register asks the peer to register this connection as an instance in
// group, and returns the Instance the peer recorded.
func Register(ctx context.Context, peer *router.Peer, group string, metadata json.RawMessage) (structure.Instance, error) {
	payload, err := json.Marshal(RegisterRequest{Group: group, Metadata: metadata})
	if err != nil {
		return structure.Instance{}, err
	}
	raw, err := peer.Call(ctx, RouteRegister, payload)
	if err != nil {
		return structure.Instance{}, err
	}
	var inst structure.Instance
	if err := json.Unmarshal(raw, &inst); err != nil {
		return structure.Instance{}, &router.RemoteError{Code: wire.ErrInternal, Message: "malformed registry.register reply"}
	}
	return inst, nil
}
