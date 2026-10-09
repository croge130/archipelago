package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/croge130/archipelago/transit"
	"github.com/croge130/archipelago/wire"
)

// Handler handles one request or event. For a request, the returned
// payload becomes the response and a returned error becomes a coded
// error frame; for an event the result is discarded and an error is
// only logged.
type Handler func(ctx context.Context, req Request) (json.RawMessage, error)

// Authorizer is run after the handshake and before the handler. A
// non-nil error refuses the message: the caller sees only
// ErrUnauthorized, and the reason is logged here.
type Authorizer func(ctx context.Context, session transit.Session, msg wire.Message) error

// ChannelHandler owns an accepted channel until it returns; the router
// closes the channel afterwards.
type ChannelHandler func(ctx context.Context, req ChannelRequest) error

// Request is what a Handler is given.
type Request struct {
	Message wire.Message
	Session transit.Session
	// Peer lets the handler call or push back to the sender on the same
	// session.
	Peer *Peer
}

// ChannelRequest is what a ChannelHandler is given.
type ChannelRequest struct {
	Channel transit.Channel
	Session transit.Session
	Peer    *Peer
}

// RouteKey identifies a route. Version is reserved: it is always 0 for
// now, so that adding versioned handlers later populates a field that
// already exists instead of rekeying the table (21, "Versions").
type RouteKey struct {
	Type    string
	Version int
}

// Route registers a handler for a message type.
type Route struct {
	Type    string
	Version int // must be 0; see RouteKey
	Handler Handler

	// Concurrency is how many messages of this route may run at once per
	// session. The default, 1, handles them one at a time in arrival
	// order — the route is the ordering domain.
	Concurrency int

	Authorize Authorizer
}

// ChannelRoute registers a handler for a typed channel.
type ChannelRoute struct {
	Type      string
	Handler   ChannelHandler
	Authorize Authorizer
}

// Options configure a Router. Zero values take the defaults.
type Options struct {
	// Logger receives structured logs; default slog.Default().
	Logger *slog.Logger

	// MaxInFlight bounds how many handlers run at once per session;
	// beyond it a request is answered busy (default 64). It is the
	// inbound analogue of the consent rule in 19: a peer cannot make a
	// node do unbounded work by sending faster than it can answer.
	MaxInFlight int

	// HelloTimeout is how long an accepted session may stay silent
	// before its handshake; it is closed after (default 10s).
	HelloTimeout time.Duration

	// MinProtocol and MaxProtocol are the versions this router speaks;
	// default wire.MinProtocol and wire.MaxProtocol.
	MinProtocol int
	MaxProtocol int

	// SendTimeout bounds each reply or error frame written back, which
	// is sent after the handler's own context may be done (default 10s).
	SendTimeout time.Duration
}

func (o Options) withDefaults() Options {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.MaxInFlight <= 0 {
		o.MaxInFlight = 64
	}
	if o.HelloTimeout <= 0 {
		o.HelloTimeout = 10 * time.Second
	}
	if o.MinProtocol == 0 {
		o.MinProtocol = wire.MinProtocol
	}
	if o.MaxProtocol == 0 {
		o.MaxProtocol = wire.MaxProtocol
	}
	if o.SendTimeout <= 0 {
		o.SendTimeout = 10 * time.Second
	}
	return o
}

// Router is a registry of routes, shared by every session it serves.
// Routes may be added at any time, but are normally registered once at
// startup.
type Router struct {
	opts Options

	mu       sync.RWMutex
	routes   map[RouteKey]Route
	channels map[string]ChannelRoute
}

// New creates a Router.
func New(opts Options) *Router {
	return &Router{
		opts:     opts.withDefaults(),
		routes:   map[RouteKey]Route{},
		channels: map[string]ChannelRoute{},
	}
}

// reservedPrefix is the namespace the router keeps for itself.
const reservedPrefix = "transit."

func validateRouteType(typ string) error {
	if typ == "" {
		return fmt.Errorf("router: a route needs a Type")
	}
	if strings.HasPrefix(typ, reservedPrefix) {
		return fmt.Errorf("router: %q is in the %q namespace, which the router keeps for itself", typ, reservedPrefix)
	}
	return nil
}

// Validate reports whether Handle would accept the route's own fields.
// It cannot see whether the type is already taken. It exists so a caller
// that must do other work before registering (routerauth writes the
// endpoint definition first) can fail before doing any of it.
func (route Route) Validate() error {
	if err := validateRouteType(route.Type); err != nil {
		return err
	}
	if route.Handler == nil {
		return fmt.Errorf("router: route %q has no Handler", route.Type)
	}
	if route.Version != 0 {
		return fmt.Errorf("router: route %q: versioned routes are not supported yet; Version must be 0", route.Type)
	}
	if route.Concurrency < 0 {
		return fmt.Errorf("router: route %q: Concurrency must not be negative", route.Type)
	}
	return nil
}

// Validate is Route.Validate for a channel route.
func (route ChannelRoute) Validate() error {
	if err := validateRouteType(route.Type); err != nil {
		return err
	}
	if route.Handler == nil {
		return fmt.Errorf("router: channel route %q has no Handler", route.Type)
	}
	return nil
}

// Handle registers a route. A duplicate type is an error rather than a
// silent replacement, so two registrations can never fight over a name.
func (r *Router) Handle(route Route) error {
	if err := route.Validate(); err != nil {
		return err
	}
	if route.Concurrency == 0 {
		route.Concurrency = 1
	}
	key := RouteKey{Type: route.Type}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.routes[key]; exists {
		return fmt.Errorf("router: a route for %q is already registered", route.Type)
	}
	r.routes[key] = route
	return nil
}

// HandleChannel registers a handler for a typed channel.
func (r *Router) HandleChannel(route ChannelRoute) error {
	if err := route.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.channels[route.Type]; exists {
		return fmt.Errorf("router: a channel route for %q is already registered", route.Type)
	}
	r.channels[route.Type] = route
	return nil
}

func (r *Router) route(typ string) (Route, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	route, ok := r.routes[RouteKey{Type: typ}]
	return route, ok
}

func (r *Router) channelRoute(typ string) (ChannelRoute, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	route, ok := r.channels[typ]
	return route, ok
}
