package routerauth

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/croge130/archipelago/gatehouse-core/facade"
	"github.com/croge130/archipelago/gatehouse-core/structure"
	"github.com/croge130/archipelago/peerauth"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

// Registrar registers routes on one router against one domain's
// Gatehouse-core stores. One per App: it holds no domain concept of its
// own, the stores are the domain (18).
type Registrar struct {
	router *router.Router
	reader facade.Reader
	writer facade.Writer
}

// New returns a Registrar. All three arguments are required.
func New(r *router.Router, reader facade.Reader, writer facade.Writer) (*Registrar, error) {
	if r == nil || reader == nil || writer == nil {
		return nil, errors.New("routerauth: a router, a reader and a writer are all required")
	}
	return &Registrar{router: r, reader: reader, writer: writer}, nil
}

// Endpoint is a request/event route together with what it requires.
type Endpoint struct {
	// Key is both the route's message type and the advertised endpoint
	// key, so a caller that learned of the endpoint knows the type to
	// send. It follows facade.RegisterEndpoint's reserved-namespace rule.
	Key         string
	Description string

	// Permission is the key a caller's principal must hold. It must
	// already be registered (facade.ErrPermissionNotRegistered
	// otherwise). Empty means the endpoint is public: listed to every
	// peer and callable without a permission. A peer still needs a valid
	// mTLS session to reach it at all.
	Permission string

	// Metadata is opaque, app-defined and never authorized on.
	Metadata json.RawMessage

	Handler     router.Handler
	Concurrency int

	// AllowReservedNamespace is facade.RegisterEndpointOptions' override,
	// for the core's own routes.
	AllowReservedNamespace bool
}

// ChannelEndpoint is a typed-channel route together with what it requires.
// The channel type is also the endpoint key, in the same key space as
// Endpoint.Key: registering a route and a channel route under one key is
// permitted only if their definitions match, and is best avoided.
type ChannelEndpoint struct {
	Type                   string
	Description            string
	Permission             string
	Metadata               json.RawMessage
	Handler                router.ChannelHandler
	AllowReservedNamespace bool
}

// Handle registers the route, its authorizer and its endpoint definition
// together. The route's own fields are validated and the endpoint
// definition is written first, so a failure there leaves nothing
// half-registered; the only failure left for the route registration is a
// duplicate type, in which case the definition written is the one the
// other registration already holds (the registry is idempotent by key, and
// a different definition under the key is facade.ErrConflict).
func (g *Registrar) Handle(ctx context.Context, ep Endpoint) error {
	route := router.Route{
		Type:        ep.Key,
		Handler:     ep.Handler,
		Concurrency: ep.Concurrency,
		Authorize:   g.authorizer(ep.Permission),
	}
	if err := route.Validate(); err != nil {
		return err
	}
	if err := g.registerDefinition(ctx, ep.Key, ep.Description, ep.Permission, ep.Metadata, ep.AllowReservedNamespace); err != nil {
		return err
	}
	return g.router.Handle(route)
}

// HandleChannel is Handle for a typed channel.
func (g *Registrar) HandleChannel(ctx context.Context, ep ChannelEndpoint) error {
	route := router.ChannelRoute{
		Type:      ep.Type,
		Handler:   ep.Handler,
		Authorize: g.authorizer(ep.Permission),
	}
	if err := route.Validate(); err != nil {
		return err
	}
	if err := g.registerDefinition(ctx, ep.Type, ep.Description, ep.Permission, ep.Metadata, ep.AllowReservedNamespace); err != nil {
		return err
	}
	return g.router.HandleChannel(route)
}

func (g *Registrar) registerDefinition(ctx context.Context, key, description, permission string, metadata json.RawMessage, allowReserved bool) error {
	return facade.RegisterEndpoint(ctx, g.reader, g.writer, structure.EndpointDefinition{
		EndpointKey:           key,
		Description:           description,
		RequiredPermissionKey: permission,
		Metadata:              metadata,
	}, facade.RegisterEndpointOptions{AllowReservedNamespace: allowReserved})
}

// authorizer is the single place a route's permission becomes a check: it
// is peerauth.Require, so the answer is Transit's verified identity run
// through Gatehouse-core's own evaluator, nothing reimplemented. Empty
// permission means no check.
func (g *Registrar) authorizer(permission string) router.Authorizer {
	if permission == "" {
		return nil
	}
	return func(ctx context.Context, session transit.Session, _ wire.Message) error {
		return peerauth.Require(ctx, g.reader, session, permission)
	}
}
