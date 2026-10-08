package wire

import (
	"encoding/json"
	"fmt"
)

// The protocol's own vocabulary: the version it speaks, the handshake
// that agrees one, and the coded errors a peer can answer with. wire owns
// these because they are the contract every client needs; how a server
// resolves a version to handlers is its own business and lives in the
// router. See docs/architecture/21-router-and-handshake-model.md.

// Protocol versions. MinProtocol is the oldest this code still speaks and
// MaxProtocol the newest; they are equal while there is one version.
//
// Changelog:
//
//	1  the first version: the envelope in message.go, the delivery
//	   classes, typed channels, the transit.hello handshake, and the coded
//	   errors below.
const (
	MinProtocol = 1
	MaxProtocol = 1
)

// TypeHello is the message type of the handshake. The transit.* types
// are reserved to the router; nothing else registers under them.
const TypeHello = "transit.hello"

// Hello is the dialer's opening request. It carries a version range and
// nothing else of substance: it reveals no more than a multicast
// announcement does, and says nothing of domain, principal or what the
// node offers — those are learned after, through routes the peer is
// authorized to call.
type Hello struct {
	MinVersion int `json:"min_version"`
	MaxVersion int `json:"max_version"`

	// InstanceID is a logging hint only. Like every peer-asserted claim
	// it is unverified and must not be trusted or authorized on.
	InstanceID string `json:"instance_id,omitempty"`
}

// HelloReply is the acceptor's answer: the version chosen for the
// session and the newest the acceptor speaks.
type HelloReply struct {
	Version    int `json:"version"`
	MaxVersion int `json:"max_version"`
}

// Validate checks a received Hello.
func (h Hello) Validate() error {
	if h.MinVersion < 1 || h.MaxVersion < h.MinVersion {
		return fmt.Errorf("wire: hello: invalid version range [%d, %d]", h.MinVersion, h.MaxVersion)
	}
	return nil
}

// NegotiateVersion picks the highest version both sides speak. ok is
// false when the ranges do not overlap.
func NegotiateVersion(localMin, localMax, peerMin, peerMax int) (version int, ok bool) {
	hi := localMax
	if peerMax < hi {
		hi = peerMax
	}
	lo := localMin
	if peerMin > lo {
		lo = peerMin
	}
	if hi < lo {
		return 0, false
	}
	return hi, true
}

// ErrorCode is a member of a small closed set. The code is what a
// caller branches on; the message is for logs and people and never
// carries internals.
type ErrorCode string

const (
	// ErrUnknownRoute: no route is registered for that message type.
	ErrUnknownRoute ErrorCode = "unknown_route"
	// ErrHelloRequired: the session has not completed the handshake.
	ErrHelloRequired ErrorCode = "hello_required"
	// ErrVersionUnsupported: no protocol version in common.
	ErrVersionUnsupported ErrorCode = "version_unsupported"
	// ErrUnauthorized: the route's authorizer refused. It deliberately
	// says nothing of why, so a peer cannot probe what exists.
	ErrUnauthorized ErrorCode = "unauthorized"
	// ErrInvalid: the payload failed the handler's validation.
	ErrInvalid ErrorCode = "invalid"
	// ErrBusy: the receiver's in-flight limit is reached; retry later.
	ErrBusy ErrorCode = "busy"
	// ErrCancelled: the request was cancelled.
	ErrCancelled ErrorCode = "cancelled"
	// ErrInternal: the handler failed; details are logged on the
	// handling side only.
	ErrInternal ErrorCode = "internal"
)

func (c ErrorCode) Valid() bool {
	switch c {
	case ErrUnknownRoute, ErrHelloRequired, ErrVersionUnsupported, ErrUnauthorized,
		ErrInvalid, ErrBusy, ErrCancelled, ErrInternal:
		return true
	default:
		return false
	}
}

// ErrorPayload is the body of an error frame.
type ErrorPayload struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message,omitempty"`
}

// ErrorReply builds the error frame answering req: same Type and ID, so
// the caller can correlate it, Kind error, and a coded payload.
func ErrorReply(req Message, code ErrorCode, message string) Message {
	payload, _ := json.Marshal(ErrorPayload{Code: code, Message: message}) // two plain fields; cannot fail
	return Message{Type: req.Type, ID: req.ID, Kind: KindError, Payload: payload}
}

// DecodeError reads the payload of an error frame. A frame whose payload
// is missing or carries an unknown code is reported as ErrInternal with
// the raw text, so a newer peer's new code never makes an error vanish.
func DecodeError(m Message) ErrorPayload {
	var p ErrorPayload
	if err := json.Unmarshal(m.Payload, &p); err != nil || !p.Code.Valid() {
		return ErrorPayload{Code: ErrInternal, Message: string(m.Payload)}
	}
	return p
}
