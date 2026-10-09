package sso

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	certstoreEvaluation "github.com/croge130/archipelago/certstore/evaluation"
	"github.com/croge130/archipelago/peerauth"
	"github.com/croge130/archipelago/router"
	"github.com/croge130/archipelago/sso/structure"
	"github.com/croge130/archipelago/wire"
)

// RouteIssue is the route (and endpoint key) a peer calls to be issued a
// ticket.
const RouteIssue = "sso.ticket.issue"

// IssueRequest is RouteIssue's payload. There is deliberately no subject
// field: the ticket vouches for whoever the connection's verified identity
// resolves to. Issuing a ticket for a different principal is delegation,
// which needs its own permission and is not offered here (12, "Delivery").
type IssueRequest struct {
	Audience   string `json:"audience"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

// IssuerStore is what the route needs to read: peer resolution plus the
// principal lookup Issue itself performs.
type IssuerStore interface {
	peerauth.Store
	PrincipalStore
}

// IssueConfig configures IssueHandler.
type IssueConfig struct {
	Store  IssuerStore
	Signer certstoreEvaluation.Signer

	// DefaultTTL applies when a request names none (default one minute).
	DefaultTTL time.Duration
	// MaxTTL is the longest ticket the route will issue (default five
	// minutes). A request beyond it is refused rather than silently
	// shortened, so a relying party never receives a ticket expiring
	// sooner than its holder was told.
	MaxTTL time.Duration
}

// IssueHandler is Issue as a router handler: the caller asks for a ticket
// for itself, bound to an audience, and the reply is the signed Ticket.
// Register it with routerauth.Handle to decide which principals may ask.
func IssueHandler(cfg IssueConfig) (router.Handler, error) {
	if cfg.Store == nil || cfg.Signer == nil {
		return nil, errors.New("sso: IssueHandler needs a Store and a Signer")
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = time.Minute
	}
	if cfg.MaxTTL <= 0 {
		cfg.MaxTTL = 5 * time.Minute
	}
	if cfg.DefaultTTL > cfg.MaxTTL {
		return nil, fmt.Errorf("sso: DefaultTTL %s exceeds MaxTTL %s", cfg.DefaultTTL, cfg.MaxTTL)
	}
	return func(ctx context.Context, req router.Request) (json.RawMessage, error) {
		var body IssueRequest
		if err := json.Unmarshal(req.Message.Payload, &body); err != nil {
			return nil, router.Errorf(wire.ErrInvalid, "issue: malformed payload")
		}
		if body.Audience == "" {
			return nil, router.Errorf(wire.ErrInvalid, "issue: audience is required")
		}
		ttl := cfg.DefaultTTL
		if body.TTLSeconds < 0 {
			return nil, router.Errorf(wire.ErrInvalid, "issue: ttl_seconds must not be negative")
		}
		if body.TTLSeconds > 0 {
			ttl = time.Duration(body.TTLSeconds) * time.Second
		}
		if ttl > cfg.MaxTTL {
			return nil, router.Errorf(wire.ErrInvalid, "issue: ttl exceeds the maximum of %d seconds", int(cfg.MaxTTL/time.Second))
		}

		subject, err := peerauth.ResolvePrincipal(ctx, cfg.Store, req.Session)
		if errors.Is(err, peerauth.ErrNoPeerIdentity) || errors.Is(err, peerauth.ErrUnknownPeer) {
			return nil, router.Errorf(wire.ErrUnauthorized, "not authorized")
		}
		if err != nil {
			return nil, err
		}
		ticket, err := Issue(ctx, cfg.Store, cfg.Signer, subject, body.Audience, ttl)
		if err != nil {
			return nil, err
		}
		return json.Marshal(ticket)
	}, nil
}

// RequestTicket asks a peer to issue this node a ticket for audience.
// ttl of zero takes the issuer's default.
func RequestTicket(ctx context.Context, peer *router.Peer, audience string, ttl time.Duration) (structure.Ticket, error) {
	payload, err := json.Marshal(IssueRequest{Audience: audience, TTLSeconds: int(ttl / time.Second)})
	if err != nil {
		return structure.Ticket{}, err
	}
	raw, err := peer.Call(ctx, RouteIssue, payload)
	if err != nil {
		return structure.Ticket{}, err
	}
	var ticket structure.Ticket
	if err := json.Unmarshal(raw, &ticket); err != nil {
		return structure.Ticket{}, &router.RemoteError{Code: wire.ErrInternal, Message: "malformed sso.ticket.issue reply"}
	}
	return ticket, nil
}
